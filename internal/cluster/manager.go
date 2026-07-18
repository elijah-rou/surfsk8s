package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/elijahrou/surfsk8s/internal/informer"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

type Config struct {
	KubeconfigPath   string
	Context          string
	DiscoveryTimeout time.Duration
}

type ContextInfo struct {
	Name    string
	Cluster string
	User    string
	Current bool
}

type ClusterConn struct {
	ID        string
	Name      string
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	REST      rest.Interface
	Discovery discovery.DiscoveryInterface
	Watcher   *informer.Watcher
	Cancel    context.CancelFunc
	Context   context.Context

	genericMu      sync.Mutex
	genericWatches map[string]*genericResourceWatch

	Healthy atomic.Bool
	Synced  atomic.Bool
	message atomic.Value
	warning atomic.Value

	discoveryContext context.Context
	discoveryCancel  context.CancelFunc
}

// Manager holds concurrent connections to multiple k8s clusters.
// Each cluster gets its own clientset and informer factory.
const (
	genericChangeHistoryLimit = 2048
	discoveryOverallTimeout   = 20 * time.Second
	maxDiscoveryAttempts      = 4
)

type GenericResourceChange struct {
	Version uint64
	Key     GenericResourceKey
	Deleted bool
	Row     GenericResourceRow
	Known   bool
}

type Manager struct {
	mu sync.RWMutex
	wg sync.WaitGroup

	store                   *state.Store
	discoveryTimeout        time.Duration
	discoveryOverallTimeout time.Duration
	discoveryRetryBackoffs  []time.Duration
	rawConfig               clientcmdapi.Config
	contexts                []ContextInfo
	conns                   map[string]*ClusterConn
	connOrder               []string // sorted context names; mirrors keys in conns
	resources               map[string][]discoveredResource
	closed                  bool
	version                 atomic.Uint64

	genericVersionMu sync.RWMutex
	genericVersions  map[string]uint64
	genericChanges   map[string][]GenericResourceChange
}

func NewManager(store *state.Store, cfg Config) (*Manager, error) {
	if store == nil {
		panic("cluster.NewManager: nil store")
	}
	discoveryTimeout := cfg.DiscoveryTimeout
	if discoveryTimeout == 0 {
		discoveryTimeout = 4 * time.Second
	}
	if discoveryTimeout < 0 {
		return nil, fmt.Errorf("discovery timeout must be >= 0")
	}

	rawConfig, contexts, err := loadContexts(cfg)
	if err != nil {
		return nil, err
	}

	return &Manager{
		store:                   store,
		discoveryTimeout:        discoveryTimeout,
		discoveryOverallTimeout: discoveryOverallTimeout,
		discoveryRetryBackoffs:  []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second},
		rawConfig:               rawConfig,
		contexts:                contexts,
		conns:                   make(map[string]*ClusterConn, max(1, len(contexts))),
		resources:               make(map[string][]discoveredResource, max(1, len(contexts))),
		genericVersions:         make(map[string]uint64, 8),
		genericChanges:          make(map[string][]GenericResourceChange, 8),
	}, nil
}

func (m *Manager) Version() uint64 {
	return m.version.Load()
}

func (m *Manager) GenericResourceVersion(resourceID string) uint64 {
	if resourceID == "" {
		return 0
	}
	m.genericVersionMu.RLock()
	defer m.genericVersionMu.RUnlock()
	return m.genericVersions[resourceID]
}

func (m *Manager) appendGenericChangeLocked(resourceID string, change GenericResourceChange) {
	changes := m.genericChanges[resourceID]
	if len(changes) < genericChangeHistoryLimit {
		m.genericChanges[resourceID] = append(changes, change)
		return
	}
	copy(changes, changes[1:])
	changes[len(changes)-1] = change
	m.genericChanges[resourceID] = changes
}

func (m *Manager) bumpGenericResourceVersion(resourceID string) {
	if resourceID == "" {
		panic("cluster.Manager.bumpGenericResourceVersion: empty resourceID")
	}
	m.genericVersionMu.Lock()
	m.genericVersions[resourceID]++
	m.genericVersionMu.Unlock()
}

func (m *Manager) recordGenericResourceUpsert(resourceID string, row GenericResourceRow) {
	if resourceID == "" {
		panic("cluster.Manager.recordGenericResourceUpsert: empty resourceID")
	}
	m.genericVersionMu.Lock()
	version := m.genericVersions[resourceID] + 1
	m.genericVersions[resourceID] = version
	m.appendGenericChangeLocked(resourceID, GenericResourceChange{Version: version, Key: row.Key, Row: row, Known: true})
	m.genericVersionMu.Unlock()
}

func (m *Manager) recordGenericResourceDelete(resourceID string, key GenericResourceKey) {
	if resourceID == "" {
		panic("cluster.Manager.recordGenericResourceDelete: empty resourceID")
	}
	m.genericVersionMu.Lock()
	version := m.genericVersions[resourceID] + 1
	m.genericVersions[resourceID] = version
	m.appendGenericChangeLocked(resourceID, GenericResourceChange{Version: version, Key: key, Deleted: true, Known: true})
	m.genericVersionMu.Unlock()
}

func (m *Manager) GenericResourceDelta(resourceID string, sinceVersion uint64) (uint64, []GenericResourceChange, bool) {
	if resourceID == "" {
		return 0, nil, false
	}
	m.genericVersionMu.RLock()
	defer m.genericVersionMu.RUnlock()
	currentVersion := m.genericVersions[resourceID]
	if sinceVersion == currentVersion {
		return currentVersion, nil, true
	}
	changes := m.genericChanges[resourceID]
	if len(changes) == 0 {
		return currentVersion, nil, false
	}
	oldestVersion := changes[0].Version
	if sinceVersion+1 < oldestVersion {
		return currentVersion, nil, false
	}
	start := 0
	for start < len(changes) && changes[start].Version <= sinceVersion {
		start++
	}
	for _, change := range changes[start:] {
		if !change.Known {
			return currentVersion, nil, false
		}
	}
	result := append([]GenericResourceChange(nil), changes[start:]...)
	return currentVersion, result, true
}

func (m *Manager) AvailableContexts() []ContextInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	contexts := make([]ContextInfo, len(m.contexts))
	copy(contexts, m.contexts)
	return contexts
}

func (m *Manager) ConnectedContextNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.conns))
	for name := range m.conns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (m *Manager) Connect(ctx context.Context, contextNames []string) error {
	if ctx == nil {
		panic("cluster.Manager.Connect: nil context")
	}
	if len(contextNames) == 0 {
		return fmt.Errorf("no contexts selected")
	}

	uniqueNames := make([]string, 0, len(contextNames))
	seen := make(map[string]struct{}, len(contextNames))
	for _, name := range contextNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		uniqueNames = append(uniqueNames, name)
	}
	if len(uniqueNames) == 0 {
		return fmt.Errorf("no contexts selected")
	}

	for _, name := range uniqueNames {
		if _, err := m.ConnectContext(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) ConnectContext(ctx context.Context, contextName string) (*ClusterConn, error) {
	if ctx == nil {
		panic("cluster.Manager.ConnectContext: nil context")
	}
	if contextName == "" {
		panic("cluster.Manager.ConnectContext: empty contextName")
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("manager closed")
	}
	if existing, ok := m.conns[contextName]; ok {
		m.mu.Unlock()
		return existing, nil
	}
	if !m.hasContextLocked(contextName) {
		m.mu.Unlock()
		return nil, fmt.Errorf("unknown kubeconfig context %q", contextName)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(m.rawConfig, &clientcmd.ConfigOverrides{CurrentContext: contextName})
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("load kubeconfig context %q: %w", contextName, err)
	}

	connCtx, cancel := context.WithCancel(ctx)
	discoveryCtx, discoveryCancel := context.WithTimeout(connCtx, m.discoveryOverallTimeout)
	cancelClients := func() {
		discoveryCancel()
		cancel()
	}
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		cancelClients()
		m.mu.Unlock()
		return nil, fmt.Errorf("create clientset for context %q: %w", contextName, err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		cancelClients()
		m.mu.Unlock()
		return nil, fmt.Errorf("create dynamic client for context %q: %w", contextName, err)
	}
	discoveryConfig := rest.CopyConfig(restConfig)
	discoveryConfig.Timeout = m.discoveryTimeout
	previousWrapTransport := discoveryConfig.WrapTransport
	discoveryConfig.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
		if previousWrapTransport != nil {
			transport = previousWrapTransport(transport)
		}
		return discoveryContextTransport{context: discoveryCtx, transport: transport}
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(discoveryConfig)
	if err != nil {
		cancelClients()
		m.mu.Unlock()
		return nil, fmt.Errorf("create discovery client for context %q: %w", contextName, err)
	}

	watcher := informer.NewWatcher(clientset, contextName, m.store)
	conn := &ClusterConn{
		ID:               contextName,
		Name:             contextName,
		Clientset:        clientset,
		Dynamic:          dynamicClient,
		REST:             clientset.CoreV1().RESTClient(),
		Discovery:        discoveryClient,
		Watcher:          watcher,
		Cancel:           cancel,
		Context:          connCtx,
		genericWatches:   make(map[string]*genericResourceWatch, 4),
		discoveryContext: discoveryCtx,
		discoveryCancel:  discoveryCancel,
	}
	conn.message.Store("connecting")
	m.conns[contextName] = conn
	m.insertConnOrderLocked(contextName)
	m.version.Add(1)
	m.wg.Add(2)
	m.mu.Unlock()

	watcher.Start(connCtx)
	go func() {
		defer m.wg.Done()
		m.awaitInitialSync(connCtx, conn)
	}()
	go func() {
		defer m.wg.Done()
		m.discoverResources(connCtx, conn)
	}()
	return conn, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	cancels := make([]context.CancelFunc, 0, len(m.conns))
	watchers := make([]*informer.Watcher, 0, len(m.conns))
	for _, conn := range m.conns {
		cancels = append(cancels, conn.Cancel)
		watchers = append(watchers, conn.Watcher)
	}
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	for _, watcher := range watchers {
		watcher.Wait()
	}
	m.wg.Wait()
}

func (m *Manager) Catalog() []ResourceGroup {
	m.mu.RLock()
	defer m.mu.RUnlock()

	merged := make([]discoveredResource, 0, 128)
	seen := make(map[string]struct{}, 128)
	for _, connectionName := range m.connOrder {
		for _, resource := range m.resources[connectionName] {
			key := resourceKey(resource.APIGroup, resource.Resource)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, resource)
		}
	}
	return buildCatalog(merged)
}

func (m *Manager) insertConnOrderLocked(name string) {
	i := sort.SearchStrings(m.connOrder, name)
	if i < len(m.connOrder) && m.connOrder[i] == name {
		return
	}
	m.connOrder = append(m.connOrder, "")
	copy(m.connOrder[i+1:], m.connOrder[i:])
	m.connOrder[i] = name
}

func clusterStatusFromConn(conn *ClusterConn) components.ClusterStatus {
	status := components.ClusterStatus{
		Name:    conn.Name,
		Healthy: conn.Healthy.Load(),
		Synced:  conn.Synced.Load(),
	}
	if message, ok := conn.message.Load().(string); ok {
		status.Message = message
	}
	if warning, ok := conn.warning.Load().(string); ok {
		status.Warning = warning
	}
	if decodeErrors := conn.Watcher.DecodeErrorCount(); decodeErrors > 0 {
		status.Warning = fmt.Sprintf("decode-errors:%d", decodeErrors)
	}
	return status
}

// StatusesInto fills dst with cluster connection health in stable sorted order.
// Reuses dst's backing array when cap(dst) is large enough; pass dst[:0] to append into an existing buffer.
func (m *Manager) StatusesInto(dst []components.ClusterStatus) []components.ClusterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var ids []string
	switch {
	case len(m.conns) == 0:
		return dst[:0]
	case len(m.connOrder) == len(m.conns):
		ids = m.connOrder
	default:
		// Rare: map/connOrder mismatch; fall back to sorting keys once.
		ids = make([]string, 0, len(m.conns))
		for id := range m.conns {
			ids = append(ids, id)
		}
		sort.Strings(ids)
	}

	n := len(ids)
	if cap(dst) < n {
		dst = make([]components.ClusterStatus, n)
	} else {
		dst = dst[:n]
	}
	for i, id := range ids {
		dst[i] = clusterStatusFromConn(m.conns[id])
	}
	return dst
}

func (m *Manager) Statuses() []components.ClusterStatus {
	return m.StatusesInto(nil)
}

func (m *Manager) ListGenericResource(ctx context.Context, resource ResourceKind) ([]GenericResourceRow, error) {
	if ctx == nil {
		panic("cluster.Manager.ListGenericResource: nil context")
	}
	if resource.Resource == "" {
		panic("cluster.Manager.ListGenericResource: empty resource")
	}
	if rows, ok := m.genericFixtureRows(resource.ID); ok {
		return rows, nil
	}

	m.mu.RLock()
	connections := make([]*ClusterConn, 0, len(m.conns))
	for _, conn := range m.conns {
		connections = append(connections, conn)
	}
	sort.Slice(connections, func(i int, j int) bool { return connections[i].Name < connections[j].Name })
	resolved := make(map[string]ResourceKind, len(connections))
	for _, conn := range connections {
		resolvedResource, ok := m.resolveResourceLocked(conn.Name, resource)
		if !ok {
			continue
		}
		resolved[conn.Name] = resolvedResource
	}
	m.mu.RUnlock()

	if len(connections) == 0 {
		return nil, fmt.Errorf("no connected contexts")
	}

	rows := make([]GenericResourceRow, 0, 128)
	errors := make([]string, 0, len(connections))
	for _, conn := range connections {
		resolvedResource, ok := resolved[conn.Name]
		if !ok {
			errors = append(errors, conn.Name+": resource not discovered")
			continue
		}
		watch := m.ensureGenericResourceWatch(conn, resolvedResource)
		if watch.synced.Load() {
			watch.touch(time.Now())
			rows = append(rows, watch.listRows()...)
			continue
		}
		list, err := listGenericResourceForConnection(ctx, conn, resolvedResource)
		if err != nil {
			errors = append(errors, conn.Name+": "+err.Error())
			continue
		}
		rows = append(rows, buildGenericResourceRows(conn.Name, list, resolvedResource.PrinterColumns)...)
	}
	if len(errors) != 0 {
		joined := strings.Join(errors, "; ")
		if len(rows) == 0 {
			return nil, fmt.Errorf("%s", joined)
		}
		return rows, fmt.Errorf("%s", joined)
	}
	return rows, nil
}

// ForEachGenericResourceRow visits every generic row across connected contexts in stable cluster order.
// When caches are synced, iteration avoids allocating a merged []GenericResourceRow slice per watch.
// If visit returns false, iteration stops early. Errors mirror ListGenericResource (partial data + trailing error).
func (m *Manager) ForEachGenericResourceRow(ctx context.Context, resource ResourceKind, visit func(GenericResourceRow) bool) error {
	if ctx == nil {
		panic("cluster.Manager.ForEachGenericResourceRow: nil context")
	}
	if resource.Resource == "" {
		panic("cluster.Manager.ForEachGenericResourceRow: empty resource")
	}
	if rows, ok := m.genericFixtureRows(resource.ID); ok {
		for _, row := range rows {
			if !visit(row) {
				return nil
			}
		}
		return nil
	}

	m.mu.RLock()
	connections := make([]*ClusterConn, 0, len(m.conns))
	for _, conn := range m.conns {
		connections = append(connections, conn)
	}
	sort.Slice(connections, func(i int, j int) bool { return connections[i].Name < connections[j].Name })
	resolved := make(map[string]ResourceKind, len(connections))
	for _, conn := range connections {
		resolvedResource, ok := m.resolveResourceLocked(conn.Name, resource)
		if !ok {
			continue
		}
		resolved[conn.Name] = resolvedResource
	}
	m.mu.RUnlock()

	if len(connections) == 0 {
		return fmt.Errorf("no connected contexts")
	}

	var rowsVisited int
	errors := make([]string, 0, len(connections))
	for _, conn := range connections {
		resolvedResource, ok := resolved[conn.Name]
		if !ok {
			errors = append(errors, conn.Name+": resource not discovered")
			continue
		}
		watch := m.ensureGenericResourceWatch(conn, resolvedResource)
		if watch.synced.Load() {
			watch.touch(time.Now())
			if !watch.forEachRow(func(row GenericResourceRow) bool {
				rowsVisited++
				return visit(row)
			}) {
				return nil
			}
			continue
		}
		list, err := listGenericResourceForConnection(ctx, conn, resolvedResource)
		if err != nil {
			errors = append(errors, conn.Name+": "+err.Error())
			continue
		}
		built := buildGenericResourceRows(conn.Name, list, resolvedResource.PrinterColumns)
		for _, row := range built {
			rowsVisited++
			if !visit(row) {
				return nil
			}
		}
	}
	if len(errors) != 0 {
		joined := strings.Join(errors, "; ")
		if rowsVisited == 0 {
			return fmt.Errorf("%s", joined)
		}
		return fmt.Errorf("%s", joined)
	}
	return nil
}

func (m *Manager) GenericResourceObjectVersion(resource ResourceKind, key GenericResourceKey) (string, bool) {
	if resource.Resource == "" {
		panic("cluster.Manager.GenericResourceObjectVersion: empty resource")
	}
	if key.Cluster == "" {
		panic("cluster.Manager.GenericResourceObjectVersion: empty cluster")
	}
	if key.Name == "" {
		panic("cluster.Manager.GenericResourceObjectVersion: empty name")
	}
	if version, ok := m.GenericResourceObjectVersionForTest(resource.ID, key); ok {
		return version, true
	}

	m.mu.RLock()
	conn, ok := m.conns[key.Cluster]
	if !ok {
		m.mu.RUnlock()
		return "", false
	}
	resolvedResource, ok := m.resolveResourceLocked(key.Cluster, resource)
	m.mu.RUnlock()
	if !ok {
		return "", false
	}

	watch := m.ensureGenericResourceWatch(conn, resolvedResource)
	watch.touch(time.Now())
	if !watch.synced.Load() {
		return "", false
	}
	return watch.objectResourceVersion(key)
}

func (m *Manager) GenericResourceDetails(ctx context.Context, resource ResourceKind, key GenericResourceKey, now time.Time) (GenericResourceDetails, error) {
	if ctx == nil {
		panic("cluster.Manager.GenericResourceDetails: nil context")
	}
	if resource.Resource == "" {
		panic("cluster.Manager.GenericResourceDetails: empty resource")
	}
	if key.Cluster == "" {
		panic("cluster.Manager.GenericResourceDetails: empty cluster")
	}
	if key.Name == "" {
		panic("cluster.Manager.GenericResourceDetails: empty name")
	}
	if details, ok, err := m.GenericResourceDetailsForTest(resource, key, now); ok || err != nil {
		return details, err
	}

	m.mu.RLock()
	conn, ok := m.conns[key.Cluster]
	if !ok {
		m.mu.RUnlock()
		return GenericResourceDetails{}, fmt.Errorf("cluster %q not connected", key.Cluster)
	}
	resolvedResource, ok := m.resolveResourceLocked(key.Cluster, resource)
	m.mu.RUnlock()
	if !ok {
		return GenericResourceDetails{}, fmt.Errorf("resource not discovered for cluster %q", key.Cluster)
	}

	watch := m.ensureGenericResourceWatch(conn, resolvedResource)
	watch.touch(now)
	if watch.synced.Load() {
		details, ok, err := watch.details(key, now)
		if err != nil {
			return GenericResourceDetails{}, err
		}
		if ok {
			return details, nil
		}
	}

	object, err := getGenericResourceForConnection(ctx, conn, resolvedResource, key)
	if err != nil {
		return GenericResourceDetails{}, err
	}
	return buildGenericResourceDetails(key.Cluster, object, now, resolvedResource.PrinterColumns)
}

func (m *Manager) awaitInitialSync(ctx context.Context, conn *ClusterConn) {
	syncCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if conn.Watcher.WaitForSync(syncCtx) {
		conn.Synced.Store(true)
		conn.Healthy.Store(true)
		conn.message.Store("")
		m.version.Add(1)
		return
	}

	if ctx.Err() == nil {
		conn.Healthy.Store(false)
		conn.message.Store("sync-timeout")
		m.version.Add(1)
	}
}

type discoveryContextTransport struct {
	context   context.Context
	transport http.RoundTripper
}

func (t discoveryContextTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		panic("cluster.discoveryContextTransport.RoundTrip: nil request")
	}
	if t.context == nil {
		panic("cluster.discoveryContextTransport.RoundTrip: nil context")
	}
	if t.transport == nil {
		panic("cluster.discoveryContextTransport.RoundTrip: nil transport")
	}
	return t.transport.RoundTrip(request.Clone(t.context))
}

func (m *Manager) discoverResources(ctx context.Context, conn *ClusterConn) {
	if ctx == nil {
		panic("cluster.Manager.discoverResources: nil context")
	}
	if conn == nil {
		panic("cluster.Manager.discoverResources: nil connection")
	}
	discoveryClient := conn.Discovery
	if discoveryClient == nil {
		discoveryClient = conn.Clientset.Discovery()
	}
	overallCtx := conn.discoveryContext
	cancelOverall := conn.discoveryCancel
	if overallCtx == nil {
		timeout := m.discoveryOverallTimeout
		if timeout <= 0 {
			timeout = discoveryOverallTimeout
		}
		overallCtx, cancelOverall = context.WithTimeout(ctx, timeout)
	}
	if cancelOverall == nil {
		panic("cluster.Manager.discoverResources: nil discovery cancel")
	}
	defer cancelOverall()

	backoffs := m.discoveryRetryBackoffs
	if len(backoffs) > maxDiscoveryAttempts-1 {
		backoffs = backoffs[:maxDiscoveryAttempts-1]
	}
	attemptLimit := len(backoffs) + 1
	accumulated := make([]discoveredResource, 0, 128)
	var printerColumnsByVersionKey map[string][]PrinterColumn
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for attempt := 0; attempt < attemptLimit; attempt++ {
		if ctx.Err() != nil {
			return
		}
		resourceLists, err := discoveryClient.ServerPreferredResources()
		if ctx.Err() != nil {
			return
		}
		if printerColumnsByVersionKey == nil && len(resourceLists) != 0 {
			printerColumnsByVersionKey = fetchCustomResourcePrinterColumns(overallCtx, conn.Dynamic)
			if ctx.Err() != nil {
				return
			}
		}
		resources := normalizeDiscoveredResources(resourceLists, printerColumnsByVersionKey)

		if err == nil {
			m.publishDiscoveryUpdate(conn, resources, "")
			return
		}

		status, retryable := classifyDiscoveryError(err)
		accumulated = unionDiscoveredResources(accumulated, resources)
		m.publishDiscoveryUpdate(conn, accumulated, status)
		if !retryable || attempt+1 >= attemptLimit {
			return
		}

		backoff := backoffs[attempt]
		if backoff < 0 {
			panic("cluster.Manager.discoverResources: negative discovery backoff")
		}
		timer.Reset(backoff)
		select {
		case <-ctx.Done():
			return
		case <-overallCtx.Done():
			return
		case <-timer.C:
		}
	}
}

func normalizeDiscoveredResources(resourceLists []*metav1.APIResourceList, printerColumnsByVersionKey map[string][]PrinterColumn) []discoveredResource {
	byKey := make(map[string]discoveredResource, len(resourceLists))
	for _, list := range resourceLists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range list.APIResources {
			if resource.Name == "" || strings.Contains(resource.Name, "/") {
				continue
			}
			key := resourceKey(gv.Group, resource.Name)
			if _, exists := byKey[key]; exists {
				continue
			}
			byKey[key] = discoveredResource{
				APIGroup:       gv.Group,
				Version:        gv.Version,
				Resource:       resource.Name,
				Kind:           resource.Kind,
				Namespaced:     resource.Namespaced,
				PrinterColumns: append([]PrinterColumn(nil), printerColumnsByVersionKey[printerColumnsKey(gv.Group, gv.Version, resource.Name)]...),
			}
		}
	}
	resources := make([]discoveredResource, 0, len(byKey))
	for _, resource := range byKey {
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(i int, j int) bool {
		return resourceKey(resources[i].APIGroup, resources[i].Resource) < resourceKey(resources[j].APIGroup, resources[j].Resource)
	})
	return resources
}

func unionDiscoveredResources(current []discoveredResource, incoming []discoveredResource) []discoveredResource {
	byKey := make(map[string]discoveredResource, len(current)+len(incoming))
	for _, resource := range current {
		byKey[resourceKey(resource.APIGroup, resource.Resource)] = resource
	}
	for _, resource := range incoming {
		key := resourceKey(resource.APIGroup, resource.Resource)
		if _, exists := byKey[key]; !exists {
			byKey[key] = resource
		}
	}
	result := make([]discoveredResource, 0, len(byKey))
	for _, resource := range byKey {
		result = append(result, resource)
	}
	sort.Slice(result, func(i int, j int) bool {
		return resourceKey(result[i].APIGroup, result[i].Resource) < resourceKey(result[j].APIGroup, result[j].Resource)
	})
	return result
}

func (m *Manager) publishDiscoveryUpdate(conn *ClusterConn, resources []discoveredResource, warning string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || conn.Context.Err() != nil {
		return
	}
	currentWarning, _ := conn.warning.Load().(string)
	if reflect.DeepEqual(m.resources[conn.Name], resources) && currentWarning == warning {
		return
	}
	m.resources[conn.Name] = append([]discoveredResource(nil), resources...)
	conn.warning.Store(warning)
	m.version.Add(1)
}

func classifyDiscoveryError(err error) (string, bool) {
	if err == nil {
		panic("cluster.classifyDiscoveryError: nil error")
	}
	if groups, partial := discovery.GroupDiscoveryFailedErrorGroups(err); partial {
		retryable := true
		for _, groupErr := range groups {
			if isPermanentDiscoveryError(groupErr) {
				retryable = false
				break
			}
		}
		return "discovery-partial", retryable
	}
	if apierrors.IsNotFound(err) {
		return "discovery-not-found", true
	}
	if isRetryableDiscoveryError(err) {
		return "discovery-error", true
	}
	return "discovery-error", false
}

func isRetryableDiscoveryError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return true
	}
	var apiStatus apierrors.APIStatus
	if errors.As(err, &apiStatus) {
		code := apiStatus.Status().Code
		return code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
	}
	return false
}

func isPermanentDiscoveryError(err error) bool {
	var syntaxError *json.SyntaxError
	return apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) || apierrors.IsBadRequest(err) || apierrors.IsInvalid(err) || errors.As(err, &syntaxError)
}

func (m *Manager) ensureGenericResourceWatch(conn *ClusterConn, resource ResourceKind) *genericResourceWatch {
	if conn == nil {
		panic("cluster.Manager.ensureGenericResourceWatch: nil conn")
	}
	if resource.ID == "" {
		panic("cluster.Manager.ensureGenericResourceWatch: empty resource ID")
	}

	conn.genericMu.Lock()
	if existing, ok := conn.genericWatches[resource.ID]; ok {
		existing.touch(time.Now())
		conn.genericMu.Unlock()
		return existing
	}

	var evicted *genericResourceWatch
	if len(conn.genericWatches) >= maxActiveGenericWatchesPerCluster {
		for _, candidate := range conn.genericWatches {
			if evicted == nil || candidate.lastAccessAt().Before(evicted.lastAccessAt()) {
				evicted = candidate
			}
		}
		if evicted != nil {
			delete(conn.genericWatches, evicted.resource.ID)
		}
	}
	watch := startGenericResourceWatch(conn.Context, m, conn, resource)
	conn.genericWatches[resource.ID] = watch
	conn.genericMu.Unlock()

	if evicted != nil {
		evicted.cancel()
	}
	return watch
}

func (m *Manager) resolveResourceLocked(clusterName string, resource ResourceKind) (ResourceKind, bool) {
	resolved := resource
	if resolved.Version != "" {
		return resolved, true
	}
	resources := m.resources[clusterName]
	for _, discovered := range resources {
		if discovered.APIGroup != resource.APIGroup || discovered.Resource != resource.Resource {
			continue
		}
		resolved.Version = discovered.Version
		if resolved.Kind == "" {
			resolved.Kind = discovered.Kind
		}
		if len(resolved.PrinterColumns) == 0 {
			resolved.PrinterColumns = append([]PrinterColumn(nil), discovered.PrinterColumns...)
		}
		return resolved, true
	}
	return ResourceKind{}, false
}

func (m *Manager) hasContextLocked(name string) bool {
	for _, context := range m.contexts {
		if context.Name == name {
			return true
		}
	}
	return false
}

func listGenericResourceForConnection(ctx context.Context, conn *ClusterConn, resource ResourceKind) (*unstructured.UnstructuredList, error) {
	if conn == nil {
		panic("cluster.listGenericResourceForConnection: nil conn")
	}
	if conn.Dynamic == nil {
		return nil, fmt.Errorf("dynamic client unavailable")
	}
	gvr := resourceGVR(resource)
	if resource.Namespaced {
		return conn.Dynamic.Resource(gvr).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	}
	return conn.Dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
}

func getGenericResourceForConnection(ctx context.Context, conn *ClusterConn, resource ResourceKind, key GenericResourceKey) (*unstructured.Unstructured, error) {
	if conn == nil {
		panic("cluster.getGenericResourceForConnection: nil conn")
	}
	if conn.Dynamic == nil {
		return nil, fmt.Errorf("dynamic client unavailable")
	}
	gvr := resourceGVR(resource)
	if resource.Namespaced {
		return conn.Dynamic.Resource(gvr).Namespace(key.Namespace).Get(ctx, key.Name, metav1.GetOptions{})
	}
	return conn.Dynamic.Resource(gvr).Get(ctx, key.Name, metav1.GetOptions{})
}

func loadContexts(cfg Config) (clientcmdapi.Config, []ContextInfo, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.KubeconfigPath != "" {
		loadingRules.ExplicitPath = cfg.KubeconfigPath
	}

	rawConfig, err := loadingRules.Load()
	if err != nil {
		return clientcmdapi.Config{}, nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	currentContext := rawConfig.CurrentContext
	if cfg.Context != "" {
		currentContext = cfg.Context
	}

	contexts := make([]ContextInfo, 0, len(rawConfig.Contexts))
	for name, context := range rawConfig.Contexts {
		contexts = append(contexts, ContextInfo{
			Name:    name,
			Cluster: context.Cluster,
			User:    context.AuthInfo,
			Current: name == currentContext,
		})
	}
	sort.Slice(contexts, func(i int, j int) bool {
		left := contexts[i]
		right := contexts[j]
		if left.Current != right.Current {
			return left.Current
		}
		return left.Name < right.Name
	})
	if len(contexts) == 0 {
		return clientcmdapi.Config{}, nil, fmt.Errorf("kubeconfig has no contexts")
	}
	return *rawConfig, contexts, nil
}
