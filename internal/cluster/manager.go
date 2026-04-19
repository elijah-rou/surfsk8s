package cluster

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/elijahrou/surfsk8s/internal/informer"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

type Config struct {
	KubeconfigPath string
	Context        string
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
	Watcher   *informer.Watcher
	Cancel    context.CancelFunc

	Healthy atomic.Bool
	Synced  atomic.Bool
	message atomic.Value
	warning atomic.Value
}

// Manager holds concurrent connections to multiple k8s clusters.
// Each cluster gets its own clientset and informer factory.
type Manager struct {
	mu sync.RWMutex
	wg sync.WaitGroup

	store     *state.Store
	rawConfig clientcmdapi.Config
	contexts  []ContextInfo
	conns     map[string]*ClusterConn
	resources map[string][]discoveredResource
	closed    bool
	version   atomic.Uint64
}

func NewManager(store *state.Store, cfg Config) (*Manager, error) {
	if store == nil {
		panic("cluster.NewManager: nil store")
	}

	rawConfig, contexts, err := loadContexts(cfg)
	if err != nil {
		return nil, err
	}

	return &Manager{
		store:     store,
		rawConfig: rawConfig,
		contexts:  contexts,
		conns:     make(map[string]*ClusterConn, max(1, len(contexts))),
		resources: make(map[string][]discoveredResource, max(1, len(contexts))),
	}, nil
}

func (m *Manager) Version() uint64 {
	return m.version.Load()
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

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("create clientset for context %q: %w", contextName, err)
	}

	connCtx, cancel := context.WithCancel(ctx)
	watcher := informer.NewWatcher(clientset, contextName, m.store)
	conn := &ClusterConn{
		ID:        contextName,
		Name:      contextName,
		Clientset: clientset,
		Watcher:   watcher,
		Cancel:    cancel,
	}
	conn.message.Store("connecting")
	m.conns[contextName] = conn
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
	for _, resources := range m.resources {
		for _, resource := range resources {
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

func (m *Manager) Statuses() []components.ClusterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.conns))
	for id := range m.conns {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	statuses := make([]components.ClusterStatus, 0, len(ids))
	for _, id := range ids {
		conn := m.conns[id]
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
		statuses = append(statuses, status)
	}
	return statuses
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

func (m *Manager) discoverResources(ctx context.Context, conn *ClusterConn) {
	resourceLists, err := conn.Clientset.Discovery().ServerPreferredResources()
	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			conn.warning.Store("discovery-not-found")
		case isPartialDiscoveryError(err):
			conn.warning.Store("discovery-partial")
		default:
			conn.message.Store("discovery-error")
		}
		m.version.Add(1)
	}
	if ctx.Err() != nil {
		return
	}

	resources := make([]discoveredResource, 0, 128)
	seen := make(map[string]struct{}, 128)
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
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			resources = append(resources, discoveredResource{
				APIGroup:   gv.Group,
				Version:    gv.Version,
				Resource:   resource.Name,
				Kind:       resource.Kind,
				Namespaced: resource.Namespaced,
			})
		}
	}

	m.mu.Lock()
	if !m.closed {
		m.resources[conn.Name] = resources
		m.version.Add(1)
	}
	m.mu.Unlock()
}

func (m *Manager) hasContextLocked(name string) bool {
	for _, context := range m.contexts {
		if context.Name == name {
			return true
		}
	}
	return false
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

func isPartialDiscoveryError(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*apierrors.StatusError)
	if ok {
		return false
	}
	return strings.Contains(err.Error(), "unable to retrieve the complete list of server APIs")
}
