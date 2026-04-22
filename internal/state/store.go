package state

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

type ResourceKey struct {
	Cluster   string
	Namespace string
	Name      string
}

func (k ResourceKey) String() string {
	return k.Cluster + "/" + k.Namespace + "/" + k.Name
}

type PodKey = ResourceKey
type DeploymentKey = ResourceKey
type ServiceKey = ResourceKey
type NodeKey = ResourceKey

type PodRow struct {
	Key             PodKey
	Cluster         string
	Namespace       string
	Name            string
	Ready           string
	Status          string
	Restarts        int
	Age             string
	Node            string
	ResourceVersion string

	createdAt  time.Time
	searchText string
}

func (r PodRow) WithAge(now time.Time) PodRow {
	r.Age = formatAge(now.Sub(r.createdAt))
	return r
}

func (r PodRow) SearchText() string {
	return r.searchText
}

func (r PodRow) CreatedAt() time.Time {
	return r.createdAt
}

type PodDetails struct {
	Row PodRow
	Pod *corev1.Pod
}

type PodQuery struct {
	Namespace string
	Search    string
}

type PodSnapshot struct {
	Version    uint64
	Total      int
	Rows       []PodRow
	Namespaces []string
}

type DeploymentRow struct {
	Key             DeploymentKey
	Cluster         string
	Namespace       string
	Name            string
	Ready           string
	UpToDate        int32
	Available       int32
	Age             string
	ResourceVersion string

	createdAt  time.Time
	searchText string
}

func (r DeploymentRow) WithAge(now time.Time) DeploymentRow {
	r.Age = formatAge(now.Sub(r.createdAt))
	return r
}

func (r DeploymentRow) SearchText() string {
	return r.searchText
}

func (r DeploymentRow) CreatedAt() time.Time {
	return r.createdAt
}

type DeploymentDetails struct {
	Row        DeploymentRow
	Deployment *appsv1.Deployment
}

type DeploymentQuery struct {
	Namespace string
}

type DeploymentSnapshot struct {
	Version    uint64
	Total      int
	Rows       []DeploymentRow
	Namespaces []string
}

type ServiceRow struct {
	Key             ServiceKey
	Cluster         string
	Namespace       string
	Name            string
	Type            string
	ClusterIP       string
	Ports           string
	Age             string
	ResourceVersion string

	createdAt  time.Time
	searchText string
}

func (r ServiceRow) WithAge(now time.Time) ServiceRow {
	r.Age = formatAge(now.Sub(r.createdAt))
	return r
}

func (r ServiceRow) SearchText() string {
	return r.searchText
}

func (r ServiceRow) CreatedAt() time.Time {
	return r.createdAt
}

type ServiceDetails struct {
	Row     ServiceRow
	Service *corev1.Service
}

type ServiceQuery struct {
	Namespace string
}

type ServiceSnapshot struct {
	Version    uint64
	Total      int
	Rows       []ServiceRow
	Namespaces []string
}

type NodeRow struct {
	Key             NodeKey
	Cluster         string
	Name            string
	Status          string
	Roles           string
	Version         string
	Age             string
	ResourceVersion string

	createdAt  time.Time
	searchText string
}

func (r NodeRow) WithAge(now time.Time) NodeRow {
	r.Age = formatAge(now.Sub(r.createdAt))
	return r
}

func (r NodeRow) SearchText() string {
	return r.searchText
}

func (r NodeRow) CreatedAt() time.Time {
	return r.createdAt
}

type NodeDetails struct {
	Row  NodeRow
	Node *corev1.Node
}

type NodeSnapshot struct {
	Version uint64
	Total   int
	Rows    []NodeRow
}

// Store aggregates resources across all connected clusters.
// Indexed for fast filtering, sorting, and lookup.
type Store struct {
	mu sync.RWMutex

	pods          map[string]PodRow
	podObjects    map[string]*corev1.Pod
	podOrdered    []PodRow
	podNamespaces []string
	podsDirty     bool

	deployments          map[string]DeploymentRow
	deploymentObjects    map[string]*appsv1.Deployment
	deploymentOrdered    []DeploymentRow
	deploymentNamespaces []string
	deploymentsDirty     bool

	services          map[string]ServiceRow
	serviceObjects    map[string]*corev1.Service
	serviceOrdered    []ServiceRow
	serviceNamespaces []string
	servicesDirty     bool

	nodes       map[string]NodeRow
	nodeObjects map[string]*corev1.Node
	nodeOrdered []NodeRow
	nodesDirty  bool

	version           uint64
	podVersion        uint64
	deploymentVersion uint64
	serviceVersion    uint64
	nodeVersion       uint64
}

func NewStore() *Store {
	return &Store{
		pods:                 make(map[string]PodRow, 1024),
		podObjects:           make(map[string]*corev1.Pod, 1024),
		podNamespaces:        []string{""},
		podsDirty:            true,
		deployments:          make(map[string]DeploymentRow, 256),
		deploymentObjects:    make(map[string]*appsv1.Deployment, 256),
		deploymentNamespaces: []string{""},
		deploymentsDirty:     true,
		services:             make(map[string]ServiceRow, 256),
		serviceObjects:       make(map[string]*corev1.Service, 256),
		serviceNamespaces:    []string{""},
		servicesDirty:        true,
		nodes:                make(map[string]NodeRow, 128),
		nodeObjects:          make(map[string]*corev1.Node, 128),
		nodesDirty:           true,
	}
}

func (s *Store) Version() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) PodVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.podVersion
}

func (s *Store) DeploymentVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deploymentVersion
}

func (s *Store) ServiceVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.serviceVersion
}

func (s *Store) NodeVersion() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nodeVersion
}

func (s *Store) UpsertPod(cluster string, pod *corev1.Pod) {
	if pod == nil {
		panic("state.Store.UpsertPod: nil pod")
	}
	if cluster == "" {
		panic("state.Store.UpsertPod: empty cluster")
	}

	row := buildPodRow(cluster, pod)
	key := row.Key.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.pods[key]; ok {
		if pod.ResourceVersion != "" && existing.ResourceVersion == pod.ResourceVersion {
			return
		}
		if pod.ResourceVersion == "" && existing == row {
			return
		}
	}

	s.pods[key] = row
	s.podObjects[key] = pod
	s.podsDirty = true
	s.version++
	s.podVersion++
}

func (s *Store) DeletePod(cluster string, pod *corev1.Pod) {
	if pod == nil {
		panic("state.Store.DeletePod: nil pod")
	}
	if cluster == "" {
		panic("state.Store.DeletePod: empty cluster")
	}
	s.DeletePodByKey(cluster, pod.Namespace, pod.Name)
}

func (s *Store) DeletePodByKey(cluster string, namespace string, name string) {
	if cluster == "" {
		panic("state.Store.DeletePodByKey: empty cluster")
	}
	if namespace == "" {
		panic("state.Store.DeletePodByKey: empty namespace")
	}
	if name == "" {
		panic("state.Store.DeletePodByKey: empty name")
	}

	key := PodKey{Cluster: cluster, Namespace: namespace, Name: name}.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pods[key]; !ok {
		return
	}
	delete(s.pods, key)
	delete(s.podObjects, key)
	s.podsDirty = true
	s.version++
	s.podVersion++
}

func (s *Store) SnapshotPods(query PodQuery, now time.Time) PodSnapshot {
	rows := make([]PodRow, 0, 128)
	total := 0
	s.ForEachPod(func(row PodRow) bool {
		total++
		if query.Namespace != "" && row.Namespace != query.Namespace {
			return true
		}
		if query.Search != "" && !strings.Contains(row.SearchText(), strings.ToLower(strings.TrimSpace(query.Search))) {
			return true
		}
		rows = append(rows, row.WithAge(now))
		return true
	})
	return PodSnapshot{Version: s.Version(), Total: total, Rows: rows, Namespaces: s.PodNamespaces()}
}

func (s *Store) ForEachPod(fn func(PodRow) bool) {
	s.ensurePodsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, row := range s.podOrdered {
		if !fn(row) {
			return
		}
	}
}

// ForEachPodObject visits ordered pod rows with their live API objects for read-only use.
// Callers must not mutate the pod pointer.
func (s *Store) ForEachPodObject(fn func(PodRow, *corev1.Pod) bool) {
	s.ensurePodsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, row := range s.podOrdered {
		if !fn(row, s.podObjects[row.Key.String()]) {
			return
		}
	}
}

func (s *Store) PodNamespaces() []string {
	s.ensurePodsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.podNamespaces...)
}

func (s *Store) PodDetailsByKey(key PodKey, now time.Time) (PodDetails, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.pods[key.String()]
	if !ok {
		return PodDetails{}, false
	}
	pod := s.podObjects[key.String()]
	if pod == nil {
		return PodDetails{Row: row.WithAge(now)}, true
	}
	return PodDetails{Row: row.WithAge(now), Pod: pod.DeepCopy()}, true
}

// PodObjectByKey returns the live API pod object for read-only use (no DeepCopy).
// Callers must not mutate the returned pointer. Safe for sequential UI use.
func (s *Store) PodObjectByKey(key PodKey) (*corev1.Pod, bool) {
	s.ensurePodsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.pods[key.String()]; !ok {
		return nil, false
	}
	pod := s.podObjects[key.String()]
	if pod == nil {
		return nil, false
	}
	return pod, true
}

func (s *Store) PodResourceVersionByKey(key PodKey) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.pods[key.String()]
	if !ok {
		return "", false
	}
	return row.ResourceVersion, true
}

func (s *Store) UpsertDeployment(cluster string, deployment *appsv1.Deployment) {
	if deployment == nil {
		panic("state.Store.UpsertDeployment: nil deployment")
	}
	if cluster == "" {
		panic("state.Store.UpsertDeployment: empty cluster")
	}

	row := buildDeploymentRow(cluster, deployment)
	key := row.Key.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.deployments[key]; ok && existing.ResourceVersion == row.ResourceVersion {
		return
	}

	s.deployments[key] = row
	s.deploymentObjects[key] = deployment
	s.deploymentsDirty = true
	s.version++
	s.deploymentVersion++
}

func (s *Store) DeleteDeployment(cluster string, deployment *appsv1.Deployment) {
	if deployment == nil {
		panic("state.Store.DeleteDeployment: nil deployment")
	}
	if cluster == "" {
		panic("state.Store.DeleteDeployment: empty cluster")
	}
	key := DeploymentKey{Cluster: cluster, Namespace: deployment.Namespace, Name: deployment.Name}.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.deployments[key]; !ok {
		return
	}
	delete(s.deployments, key)
	delete(s.deploymentObjects, key)
	s.deploymentsDirty = true
	s.version++
	s.deploymentVersion++
}

func (s *Store) SnapshotDeployments(query DeploymentQuery, now time.Time) DeploymentSnapshot {
	rows := make([]DeploymentRow, 0, 64)
	total := 0
	s.ForEachDeployment(func(row DeploymentRow) bool {
		total++
		if query.Namespace != "" && row.Namespace != query.Namespace {
			return true
		}
		rows = append(rows, row.WithAge(now))
		return true
	})
	return DeploymentSnapshot{Version: s.Version(), Total: total, Rows: rows, Namespaces: s.DeploymentNamespaces()}
}

func (s *Store) ForEachDeployment(fn func(DeploymentRow) bool) {
	s.ensureDeploymentsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, row := range s.deploymentOrdered {
		if !fn(row) {
			return
		}
	}
}

func (s *Store) DeploymentNamespaces() []string {
	s.ensureDeploymentsReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.deploymentNamespaces...)
}

func (s *Store) DeploymentDetailsByKey(key DeploymentKey, now time.Time) (DeploymentDetails, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.deployments[key.String()]
	if !ok {
		return DeploymentDetails{}, false
	}
	object := s.deploymentObjects[key.String()]
	if object == nil {
		return DeploymentDetails{Row: row.WithAge(now)}, true
	}
	return DeploymentDetails{Row: row.WithAge(now), Deployment: object.DeepCopy()}, true
}

func (s *Store) UpsertService(cluster string, service *corev1.Service) {
	if service == nil {
		panic("state.Store.UpsertService: nil service")
	}
	if cluster == "" {
		panic("state.Store.UpsertService: empty cluster")
	}

	row := buildServiceRow(cluster, service)
	key := row.Key.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.services[key]; ok && existing.ResourceVersion == row.ResourceVersion {
		return
	}

	s.services[key] = row
	s.serviceObjects[key] = service
	s.servicesDirty = true
	s.version++
	s.serviceVersion++
}

func (s *Store) DeleteService(cluster string, service *corev1.Service) {
	if service == nil {
		panic("state.Store.DeleteService: nil service")
	}
	if cluster == "" {
		panic("state.Store.DeleteService: empty cluster")
	}
	key := ServiceKey{Cluster: cluster, Namespace: service.Namespace, Name: service.Name}.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.services[key]; !ok {
		return
	}
	delete(s.services, key)
	delete(s.serviceObjects, key)
	s.servicesDirty = true
	s.version++
	s.serviceVersion++
}

func (s *Store) SnapshotServices(query ServiceQuery, now time.Time) ServiceSnapshot {
	rows := make([]ServiceRow, 0, 64)
	total := 0
	s.ForEachService(func(row ServiceRow) bool {
		total++
		if query.Namespace != "" && row.Namespace != query.Namespace {
			return true
		}
		rows = append(rows, row.WithAge(now))
		return true
	})
	return ServiceSnapshot{Version: s.Version(), Total: total, Rows: rows, Namespaces: s.ServiceNamespaces()}
}

func (s *Store) ForEachService(fn func(ServiceRow) bool) {
	s.ensureServicesReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, row := range s.serviceOrdered {
		if !fn(row) {
			return
		}
	}
}

func (s *Store) ServiceNamespaces() []string {
	s.ensureServicesReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.serviceNamespaces...)
}

func (s *Store) ServiceDetailsByKey(key ServiceKey, now time.Time) (ServiceDetails, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.services[key.String()]
	if !ok {
		return ServiceDetails{}, false
	}
	object := s.serviceObjects[key.String()]
	if object == nil {
		return ServiceDetails{Row: row.WithAge(now)}, true
	}
	return ServiceDetails{Row: row.WithAge(now), Service: object.DeepCopy()}, true
}

func (s *Store) UpsertNode(cluster string, node *corev1.Node) {
	if node == nil {
		panic("state.Store.UpsertNode: nil node")
	}
	if cluster == "" {
		panic("state.Store.UpsertNode: empty cluster")
	}

	row := buildNodeRow(cluster, node)
	key := row.Key.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.nodes[key]; ok && existing.ResourceVersion == row.ResourceVersion {
		return
	}

	s.nodes[key] = row
	s.nodeObjects[key] = node
	s.nodesDirty = true
	s.version++
	s.nodeVersion++
}

func (s *Store) DeleteNode(cluster string, node *corev1.Node) {
	if node == nil {
		panic("state.Store.DeleteNode: nil node")
	}
	if cluster == "" {
		panic("state.Store.DeleteNode: empty cluster")
	}
	key := NodeKey{Cluster: cluster, Name: node.Name}.String()

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nodes[key]; !ok {
		return
	}
	delete(s.nodes, key)
	delete(s.nodeObjects, key)
	s.nodesDirty = true
	s.version++
	s.nodeVersion++
}

func (s *Store) SnapshotNodes(now time.Time) NodeSnapshot {
	rows := make([]NodeRow, 0, 32)
	s.ForEachNode(func(row NodeRow) bool {
		rows = append(rows, row.WithAge(now))
		return true
	})
	return NodeSnapshot{Version: s.Version(), Total: len(rows), Rows: rows}
}

func (s *Store) ForEachNode(fn func(NodeRow) bool) {
	s.ensureNodesReady()
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, row := range s.nodeOrdered {
		if !fn(row) {
			return
		}
	}
}

func (s *Store) NodeDetailsByKey(key NodeKey, now time.Time) (NodeDetails, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.nodes[key.String()]
	if !ok {
		return NodeDetails{}, false
	}
	object := s.nodeObjects[key.String()]
	if object == nil {
		return NodeDetails{Row: row.WithAge(now)}, true
	}
	return NodeDetails{Row: row.WithAge(now), Node: object.DeepCopy()}, true
}

func (s *Store) ensurePodsReady() {
	s.mu.RLock()
	if !s.podsDirty {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if s.podsDirty {
		s.rebuildPodsDerivedLocked()
	}
	s.mu.Unlock()
}

func (s *Store) ensureDeploymentsReady() {
	s.mu.RLock()
	if !s.deploymentsDirty {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if s.deploymentsDirty {
		s.rebuildDeploymentsDerivedLocked()
	}
	s.mu.Unlock()
}

func (s *Store) ensureServicesReady() {
	s.mu.RLock()
	if !s.servicesDirty {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if s.servicesDirty {
		s.rebuildServicesDerivedLocked()
	}
	s.mu.Unlock()
}

func (s *Store) ensureNodesReady() {
	s.mu.RLock()
	if !s.nodesDirty {
		s.mu.RUnlock()
		return
	}
	s.mu.RUnlock()

	s.mu.Lock()
	if s.nodesDirty {
		s.rebuildNodesDerivedLocked()
	}
	s.mu.Unlock()
}

func (s *Store) rebuildPodsDerivedLocked() {
	s.podOrdered = s.podOrdered[:0]
	for _, row := range s.pods {
		s.podOrdered = append(s.podOrdered, row)
	}
	sort.Slice(s.podOrdered, func(i int, j int) bool {
		left := s.podOrdered[i]
		right := s.podOrdered[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Cluster < right.Cluster
	})
	s.podNamespaces = namespacesFromOrderedRows(s.podNamespaces[:0], s.podOrdered)
	s.podsDirty = false
}

func (s *Store) rebuildDeploymentsDerivedLocked() {
	s.deploymentOrdered = s.deploymentOrdered[:0]
	for _, row := range s.deployments {
		s.deploymentOrdered = append(s.deploymentOrdered, row)
	}
	sort.Slice(s.deploymentOrdered, func(i int, j int) bool {
		left := s.deploymentOrdered[i]
		right := s.deploymentOrdered[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Cluster < right.Cluster
	})
	s.deploymentNamespaces = namespacesFromOrderedDeploymentRows(s.deploymentNamespaces[:0], s.deploymentOrdered)
	s.deploymentsDirty = false
}

func (s *Store) rebuildServicesDerivedLocked() {
	s.serviceOrdered = s.serviceOrdered[:0]
	for _, row := range s.services {
		s.serviceOrdered = append(s.serviceOrdered, row)
	}
	sort.Slice(s.serviceOrdered, func(i int, j int) bool {
		left := s.serviceOrdered[i]
		right := s.serviceOrdered[j]
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Cluster < right.Cluster
	})
	s.serviceNamespaces = namespacesFromOrderedServiceRows(s.serviceNamespaces[:0], s.serviceOrdered)
	s.servicesDirty = false
}

func (s *Store) rebuildNodesDerivedLocked() {
	s.nodeOrdered = s.nodeOrdered[:0]
	for _, row := range s.nodes {
		s.nodeOrdered = append(s.nodeOrdered, row)
	}
	sort.Slice(s.nodeOrdered, func(i int, j int) bool {
		left := s.nodeOrdered[i]
		right := s.nodeOrdered[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.Cluster < right.Cluster
	})
	s.nodesDirty = false
}

func buildPodRow(cluster string, pod *corev1.Pod) PodRow {
	readyContainers := 0
	restarts := 0
	for _, status := range pod.Status.ContainerStatuses {
		if status.Ready {
			readyContainers++
		}
		restarts += int(status.RestartCount)
	}

	status := string(pod.Status.Phase)
	if pod.DeletionTimestamp != nil {
		status = "Terminating"
	} else if pod.Status.Reason != "" {
		status = pod.Status.Reason
	} else {
		for _, container := range pod.Status.ContainerStatuses {
			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				status = container.State.Waiting.Reason
				break
			}
			if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				status = container.State.Terminated.Reason
				break
			}
		}
	}

	row := PodRow{
		Key:             PodKey{Cluster: cluster, Namespace: pod.Namespace, Name: pod.Name},
		Cluster:         cluster,
		Namespace:       pod.Namespace,
		Name:            pod.Name,
		Ready:           fmt.Sprintf("%d/%d", readyContainers, len(pod.Spec.Containers)),
		Status:          status,
		Restarts:        restarts,
		Node:            pod.Spec.NodeName,
		ResourceVersion: pod.ResourceVersion,
		createdAt:       pod.CreationTimestamp.Time,
	}
	row.searchText = strings.ToLower(strings.Join([]string{row.Cluster, row.Namespace, row.Name, row.Ready, row.Status, row.Node}, " "))
	return row
}

func buildDeploymentRow(cluster string, deployment *appsv1.Deployment) DeploymentRow {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	row := DeploymentRow{
		Key:             DeploymentKey{Cluster: cluster, Namespace: deployment.Namespace, Name: deployment.Name},
		Cluster:         cluster,
		Namespace:       deployment.Namespace,
		Name:            deployment.Name,
		Ready:           fmt.Sprintf("%d/%d", deployment.Status.ReadyReplicas, desired),
		UpToDate:        deployment.Status.UpdatedReplicas,
		Available:       deployment.Status.AvailableReplicas,
		ResourceVersion: deployment.ResourceVersion,
		createdAt:       deployment.CreationTimestamp.Time,
	}
	row.searchText = strings.ToLower(strings.Join([]string{row.Cluster, row.Namespace, row.Name, row.Ready}, " "))
	return row
}

func buildServiceRow(cluster string, service *corev1.Service) ServiceRow {
	clusterIP := service.Spec.ClusterIP
	if clusterIP == "" {
		clusterIP = "-"
	}
	row := ServiceRow{
		Key:             ServiceKey{Cluster: cluster, Namespace: service.Namespace, Name: service.Name},
		Cluster:         cluster,
		Namespace:       service.Namespace,
		Name:            service.Name,
		Type:            string(service.Spec.Type),
		ClusterIP:       clusterIP,
		Ports:           servicePorts(service),
		ResourceVersion: service.ResourceVersion,
		createdAt:       service.CreationTimestamp.Time,
	}
	row.searchText = strings.ToLower(strings.Join([]string{row.Cluster, row.Namespace, row.Name, row.Type, row.ClusterIP, row.Ports}, " "))
	return row
}

func buildNodeRow(cluster string, node *corev1.Node) NodeRow {
	row := NodeRow{
		Key:             NodeKey{Cluster: cluster, Name: node.Name},
		Cluster:         cluster,
		Name:            node.Name,
		Status:          nodeStatus(node),
		Roles:           nodeRoles(node),
		Version:         node.Status.NodeInfo.KubeletVersion,
		ResourceVersion: node.ResourceVersion,
		createdAt:       node.CreationTimestamp.Time,
	}
	row.searchText = strings.ToLower(strings.Join([]string{row.Cluster, row.Name, row.Status, row.Roles, row.Version}, " "))
	return row
}

func namespacesFromOrderedRows(dst []string, rows []PodRow) []string {
	dst = append(dst[:0], "")
	lastNamespace := ""
	for _, row := range rows {
		if row.Namespace == lastNamespace {
			continue
		}
		lastNamespace = row.Namespace
		dst = append(dst, row.Namespace)
	}
	return dst
}

func namespacesFromOrderedDeploymentRows(dst []string, rows []DeploymentRow) []string {
	dst = append(dst[:0], "")
	lastNamespace := ""
	for _, row := range rows {
		if row.Namespace == lastNamespace {
			continue
		}
		lastNamespace = row.Namespace
		dst = append(dst, row.Namespace)
	}
	return dst
}

func namespacesFromOrderedServiceRows(dst []string, rows []ServiceRow) []string {
	dst = append(dst[:0], "")
	lastNamespace := ""
	for _, row := range rows {
		if row.Namespace == lastNamespace {
			continue
		}
		lastNamespace = row.Namespace
		dst = append(dst, row.Namespace)
	}
	return dst
}

func servicePorts(service *corev1.Service) string {
	if len(service.Spec.Ports) == 0 {
		return "-"
	}
	ports := make([]string, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		ports = append(ports, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
	}
	return strings.Join(ports, ",")
}

func nodeStatus(node *corev1.Node) string {
	for _, condition := range node.Status.Conditions {
		if condition.Type != corev1.NodeReady {
			continue
		}
		if condition.Status == corev1.ConditionTrue {
			return "Ready"
		}
		if condition.Status == corev1.ConditionFalse {
			return "NotReady"
		}
		return "Unknown"
	}
	return "Unknown"
}

func nodeRoles(node *corev1.Node) string {
	roles := make([]string, 0, 4)
	for key := range node.Labels {
		if !strings.HasPrefix(key, "node-role.kubernetes.io/") {
			continue
		}
		role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
		if role == "" {
			role = "control-plane"
		}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

func formatAge(age time.Duration) string {
	if age < 0 {
		return "0s"
	}
	if age < time.Minute {
		return fmt.Sprintf("%ds", int(age.Seconds()))
	}
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	}
	if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}
