package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

const (
	resourceJumpTargetLimit = 64
	resourceJumpDepthLimit  = 8
)

type resourceJumpTarget struct {
	Label     string
	Resource  cluster.ResourceKind
	Cluster   string
	Namespace string
	Name      string
	Depth     int
}

type resourceJumpSource struct {
	Resource   cluster.ResourceKind
	Cluster    string
	Namespace  string
	Name       string
	APIGroup   string
	Kind       string
	Object     metav1.Object
	Pod        *corev1.Pod
	Deployment *appsv1.Deployment
	Service    *corev1.Service
	Node       *corev1.Node
	Generic    *unstructured.Unstructured
}

func builtinPodResourceKind() cluster.ResourceKind {
	return cluster.ResourceKind{ID: "/pods", Display: "Pods", Kind: "Pod", Resource: "pods", Namespaced: true}
}

func builtinDeploymentResourceKind() cluster.ResourceKind {
	return cluster.ResourceKind{ID: "apps/deployments", Display: "Deployments", Kind: "Deployment", APIGroup: "apps", Resource: "deployments", Namespaced: true}
}

func builtinServiceResourceKind() cluster.ResourceKind {
	return cluster.ResourceKind{ID: "/services", Display: "Services", Kind: "Service", Resource: "services", Namespaced: true}
}

func builtinNodeResourceKind() cluster.ResourceKind {
	return cluster.ResourceKind{ID: "/nodes", Display: "Nodes", Kind: "Node", Resource: "nodes", Namespaced: false}
}

func groupFromAPIVersion(apiVersion string) string {
	group, _, ok := strings.Cut(strings.TrimSpace(apiVersion), "/")
	if ok {
		return group
	}
	return ""
}

func jumpTargetKey(target resourceJumpTarget) string {
	return target.Resource.ID + "|" + target.Cluster + "|" + target.Namespace + "|" + target.Name
}

func jumpSourceVisitKey(source resourceJumpSource) string {
	return source.Resource.ID + "|" + source.Cluster + "|" + source.Namespace + "|" + source.Name
}

func jumpTargetLabel(resource cluster.ResourceKind, clusterName string, namespace string, name string) string {
	kind := strings.TrimSpace(resource.Kind)
	if kind == "" {
		kind = strings.TrimSpace(resource.Display)
	}
	path := name
	if namespace != "" {
		path = namespace + "/" + name
	}
	if clusterName == "" {
		return kind + " " + path
	}
	return kind + " " + path + " · " + clusterName
}

func addJumpTarget(dst []resourceJumpTarget, seen map[string]struct{}, target resourceJumpTarget) []resourceJumpTarget {
	key := jumpTargetKey(target)
	if _, ok := seen[key]; ok {
		return dst
	}
	seen[key] = struct{}{}
	return append(dst, target)
}

func sortJumpTargets(targets []resourceJumpTarget) {
	sort.SliceStable(targets, func(i int, j int) bool {
		left := targets[i]
		right := targets[j]
		if left.Depth != right.Depth {
			return left.Depth < right.Depth
		}
		if left.Resource.Kind != right.Resource.Kind {
			return left.Resource.Kind < right.Resource.Kind
		}
		if left.Cluster != right.Cluster {
			return left.Cluster < right.Cluster
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})
}

func (a *App) lookupResourceByGroupAndKind(apiGroup string, kind string) (cluster.ResourceKind, bool) {
	return lookupResourceByGroupAndKind(a.manager.Catalog(), apiGroup, kind)
}

func lookupResourceByGroupAndKind(catalog []cluster.ResourceGroup, apiGroup string, kind string) (cluster.ResourceKind, bool) {
	if kind == "" {
		return cluster.ResourceKind{}, false
	}
	seen := make(map[string]struct{}, 64)
	for _, group := range catalog {
		for _, resource := range group.Resources {
			if _, ok := seen[resource.ID]; ok {
				continue
			}
			seen[resource.ID] = struct{}{}
			if resource.APIGroup != apiGroup {
				continue
			}
			if !strings.EqualFold(resource.Kind, kind) {
				continue
			}
			return resource, true
		}
	}
	return cluster.ResourceKind{}, false
}

func (a *App) lookupResourceByGroupAndResource(apiGroup string, resourceName string) (cluster.ResourceKind, bool) {
	return lookupResourceByGroupAndResource(a.manager.Catalog(), apiGroup, resourceName)
}

func lookupResourceByGroupAndResource(catalog []cluster.ResourceGroup, apiGroup string, resourceName string) (cluster.ResourceKind, bool) {
	seen := make(map[string]struct{}, 64)
	for _, group := range catalog {
		for _, resource := range group.Resources {
			if _, ok := seen[resource.ID]; ok {
				continue
			}
			seen[resource.ID] = struct{}{}
			if resource.APIGroup == apiGroup && resource.Resource == resourceName {
				return resource, true
			}
		}
	}
	return cluster.ResourceKind{}, false
}

func (a *App) currentJumpSource(now time.Time) (resourceJumpSource, error) {
	switch a.screen {
	case screenPods:
		row, ok := a.podRowAt(a.podTable.SelectedIndex(), now)
		if !ok {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		details, ok := a.store.PodDetailsByKey(row.Key, now)
		if !ok || details.Pod == nil {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		return jumpSourceFromPodDetails(details), nil
	case screenPodDetails:
		if a.activePod.Pod == nil {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		return jumpSourceFromPodDetails(a.activePod), nil
	case screenResourceList:
		return a.currentResourceListJumpSource(now)
	case screenResourceDetails:
		return a.currentResourceDetailJumpSource(), nil
	default:
		return resourceJumpSource{}, fmt.Errorf("jump unsupported on this screen")
	}
}

func jumpSourceFromPodDetails(details state.PodDetails) resourceJumpSource {
	return resourceJumpSource{
		Resource:  builtinPodResourceKind(),
		Cluster:   details.Row.Cluster,
		Namespace: details.Row.Namespace,
		Name:      details.Row.Name,
		APIGroup:  "",
		Kind:      "Pod",
		Object:    details.Pod,
		Pod:       details.Pod,
	}
}

func jumpSourceFromDeploymentDetails(details state.DeploymentDetails) resourceJumpSource {
	return resourceJumpSource{
		Resource:   builtinDeploymentResourceKind(),
		Cluster:    details.Row.Cluster,
		Namespace:  details.Row.Namespace,
		Name:       details.Row.Name,
		APIGroup:   "apps",
		Kind:       "Deployment",
		Object:     details.Deployment,
		Deployment: details.Deployment,
	}
}

func jumpSourceFromServiceDetails(details state.ServiceDetails) resourceJumpSource {
	return resourceJumpSource{
		Resource:  builtinServiceResourceKind(),
		Cluster:   details.Row.Cluster,
		Namespace: details.Row.Namespace,
		Name:      details.Row.Name,
		APIGroup:  "",
		Kind:      "Service",
		Object:    details.Service,
		Service:   details.Service,
	}
}

func jumpSourceFromNodeDetails(details state.NodeDetails) resourceJumpSource {
	return resourceJumpSource{
		Resource: builtinNodeResourceKind(),
		Cluster:  details.Row.Cluster,
		Name:     details.Row.Name,
		APIGroup: "",
		Kind:     "Node",
		Object:   details.Node,
		Node:     details.Node,
	}
}

func jumpSourceFromGenericDetails(resource cluster.ResourceKind, details cluster.GenericResourceDetails) resourceJumpSource {
	resolved := resource
	if details.Object != nil {
		if resolved.Kind == "" {
			resolved.Kind = details.Object.GetKind()
		}
		if resolved.APIGroup == "" {
			resolved.APIGroup = groupFromAPIVersion(details.Object.GetAPIVersion())
		}
	}
	return resourceJumpSource{
		Resource:  resolved,
		Cluster:   details.Row.Cluster,
		Namespace: details.Row.Namespace,
		Name:      details.Row.Name,
		APIGroup:  resolved.APIGroup,
		Kind:      resolved.Kind,
		Object:    details.Object,
		Generic:   details.Object,
	}
}

func (a *App) currentResourceListJumpSource(now time.Time) (resourceJumpSource, error) {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		row, ok := a.deploymentRowAt(a.resourceTable.SelectedIndex(), now)
		if !ok {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		details, ok := a.store.DeploymentDetailsByKey(row.Key, now)
		if !ok || details.Deployment == nil {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		return jumpSourceFromDeploymentDetails(details), nil
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		row, ok := a.serviceRowAt(a.resourceTable.SelectedIndex(), now)
		if !ok {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		details, ok := a.store.ServiceDetailsByKey(row.Key, now)
		if !ok || details.Service == nil {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		return jumpSourceFromServiceDetails(details), nil
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		row, ok := a.nodeRowAt(a.resourceTable.SelectedIndex(), now)
		if !ok {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		details, ok := a.store.NodeDetailsByKey(row.Key, now)
		if !ok || details.Node == nil {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		return jumpSourceFromNodeDetails(details), nil
	default:
		row, ok := a.genericResourceRowAt(a.resourceTable.SelectedIndex(), now)
		if !ok {
			return resourceJumpSource{}, fmt.Errorf("resource vanished during refresh")
		}
		if row.Object != nil {
			return jumpSourceFromGenericDetails(a.activeResource, cluster.GenericResourceDetails{Row: row, Object: row.Object}), nil
		}
		// Defer network fetch to the jump discovery command; carry identity only.
		return resourceJumpSource{
			Resource:  a.activeResource,
			Cluster:   row.Cluster,
			Namespace: row.Namespace,
			Name:      row.Name,
			APIGroup:  a.activeResource.APIGroup,
			Kind:      a.activeResource.Kind,
		}, nil
	}
}

func (a *App) currentResourceDetailJumpSource() resourceJumpSource {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return jumpSourceFromDeploymentDetails(a.activeDeployment)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return jumpSourceFromServiceDetails(a.activeService)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return jumpSourceFromNodeDetails(a.activeNode)
	default:
		return jumpSourceFromGenericDetails(a.activeResource, a.activeGenericDetails)
	}
}

func (a *App) fetchJumpSourceFromTarget(ctx context.Context, backend genericResourceBackend, catalog []cluster.ResourceGroup, target resourceJumpTarget, now time.Time) (resourceJumpSource, bool) {
	switch {
	case target.Resource.Resource == "pods" && target.Resource.APIGroup == "":
		details, ok := a.store.PodDetailsByKey(state.PodKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok || details.Pod == nil {
			return resourceJumpSource{}, false
		}
		return jumpSourceFromPodDetails(details), true
	case target.Resource.Resource == "deployments" && target.Resource.APIGroup == "apps":
		details, ok := a.store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok || details.Deployment == nil {
			return resourceJumpSource{}, false
		}
		return jumpSourceFromDeploymentDetails(details), true
	case target.Resource.Resource == "services" && target.Resource.APIGroup == "":
		details, ok := a.store.ServiceDetailsByKey(state.ServiceKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok || details.Service == nil {
			return resourceJumpSource{}, false
		}
		return jumpSourceFromServiceDetails(details), true
	case target.Resource.Resource == "nodes" && target.Resource.APIGroup == "":
		details, ok := a.store.NodeDetailsByKey(state.NodeKey{Cluster: target.Cluster, Name: target.Name}, now)
		if !ok || details.Node == nil {
			return resourceJumpSource{}, false
		}
		return jumpSourceFromNodeDetails(details), true
	default:
		if backend == nil {
			panic("app.fetchJumpSourceFromTarget: nil backend")
		}
		if ctx == nil {
			panic("app.fetchJumpSourceFromTarget: nil context")
		}
		details, err := backend.GenericResourceDetails(ctx, target.Resource, cluster.GenericResourceKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if err != nil || details.Object == nil {
			return resourceJumpSource{}, false
		}
		return jumpSourceFromGenericDetails(target.Resource, details), true
	}
}

func ownerRefMatchesSource(ref metav1.OwnerReference, source resourceJumpSource, childNamespace string) bool {
	if ref.Name != source.Name {
		return false
	}
	if !strings.EqualFold(ref.Kind, source.Kind) {
		return false
	}
	if groupFromAPIVersion(ref.APIVersion) != source.APIGroup {
		return false
	}
	if source.Resource.Namespaced && childNamespace != source.Namespace {
		return false
	}
	return true
}

func (a *App) ownerJumpTargets(ctx context.Context, backend genericResourceBackend, catalog []cluster.ResourceGroup, source resourceJumpSource, now time.Time) []resourceJumpTarget {
	targets := make([]resourceJumpTarget, 0, 8)
	seen := make(map[string]struct{}, 8)
	visited := make(map[string]struct{}, 8)

	var walk func(resourceJumpSource, int)
	walk = func(current resourceJumpSource, depth int) {
		if current.Object == nil || depth >= resourceJumpDepthLimit || len(targets) >= resourceJumpTargetLimit {
			return
		}
		visitKey := jumpSourceVisitKey(current)
		if _, ok := visited[visitKey]; ok {
			return
		}
		visited[visitKey] = struct{}{}
		for _, ref := range current.Object.GetOwnerReferences() {
			resource, ok := lookupResourceByGroupAndKind(catalog, groupFromAPIVersion(ref.APIVersion), ref.Kind)
			if !ok {
				continue
			}
			namespace := ""
			if resource.Namespaced {
				namespace = current.Namespace
			}
			target := resourceJumpTarget{
				Label:     jumpTargetLabel(resource, current.Cluster, namespace, ref.Name),
				Resource:  resource,
				Cluster:   current.Cluster,
				Namespace: namespace,
				Name:      ref.Name,
				Depth:     depth,
			}
			before := len(targets)
			targets = addJumpTarget(targets, seen, target)
			if len(targets) >= resourceJumpTargetLimit {
				return
			}
			if len(targets) == before {
				continue
			}
			next, ok := a.fetchJumpSourceFromTarget(ctx, backend, catalog, target, now)
			if ok {
				walk(next, depth+1)
			}
		}
	}
	walk(source, 0)

	if source.Pod != nil && source.Pod.Spec.NodeName != "" {
		nodeTarget := resourceJumpTarget{
			Label:    jumpTargetLabel(builtinNodeResourceKind(), source.Cluster, "", source.Pod.Spec.NodeName),
			Resource: builtinNodeResourceKind(),
			Cluster:  source.Cluster,
			Name:     source.Pod.Spec.NodeName,
			Depth:    resourceJumpDepthLimit,
		}
		targets = addJumpTarget(targets, seen, nodeTarget)
	}

	sortJumpTargets(targets)
	return targets
}

func (a *App) descendantGenericCandidateResources(catalog []cluster.ResourceGroup) []cluster.ResourceKind {
	pairs := [][2]string{{"apps", "replicasets"}, {"apps", "daemonsets"}, {"apps", "statefulsets"}, {"batch", "jobs"}, {"batch", "cronjobs"}}
	resources := make([]cluster.ResourceKind, 0, len(pairs))
	for _, pair := range pairs {
		resource, ok := lookupResourceByGroupAndResource(catalog, pair[0], pair[1])
		if !ok {
			continue
		}
		resources = append(resources, resource)
	}
	return resources
}

func (a *App) appendDeploymentPods(targets []resourceJumpTarget, seen map[string]struct{}, source resourceJumpSource) []resourceJumpTarget {
	deployment := source.Deployment
	if deployment == nil || deployment.Spec.Selector == nil {
		return targets
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return targets
	}
	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if len(targets) >= resourceJumpTargetLimit {
			return false
		}
		if row.Cluster != source.Cluster || row.Namespace != source.Namespace || pod == nil {
			return true
		}
		if !selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
		targets = addJumpTarget(targets, seen, resourceJumpTarget{Label: jumpTargetLabel(builtinPodResourceKind(), row.Cluster, row.Namespace, row.Name), Resource: builtinPodResourceKind(), Cluster: row.Cluster, Namespace: row.Namespace, Name: row.Name, Depth: 0})
		return true
	})
	return targets
}

func (a *App) appendServicePods(targets []resourceJumpTarget, seen map[string]struct{}, source resourceJumpSource) []resourceJumpTarget {
	service := source.Service
	if service == nil || len(service.Spec.Selector) == 0 {
		return targets
	}
	selector := labels.SelectorFromSet(service.Spec.Selector)
	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if len(targets) >= resourceJumpTargetLimit {
			return false
		}
		if row.Cluster != source.Cluster || row.Namespace != source.Namespace || pod == nil {
			return true
		}
		if !selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
		targets = addJumpTarget(targets, seen, resourceJumpTarget{Label: jumpTargetLabel(builtinPodResourceKind(), row.Cluster, row.Namespace, row.Name), Resource: builtinPodResourceKind(), Cluster: row.Cluster, Namespace: row.Namespace, Name: row.Name, Depth: 0})
		return true
	})
	return targets
}

func (a *App) appendNodePods(targets []resourceJumpTarget, seen map[string]struct{}, source resourceJumpSource) []resourceJumpTarget {
	node := source.Node
	if node == nil || source.Name == "" {
		return targets
	}
	a.store.ForEachPod(func(row state.PodRow) bool {
		if len(targets) >= resourceJumpTargetLimit {
			return false
		}
		if row.Cluster != source.Cluster || row.Node != source.Name {
			return true
		}
		targets = addJumpTarget(targets, seen, resourceJumpTarget{Label: jumpTargetLabel(builtinPodResourceKind(), row.Cluster, row.Namespace, row.Name), Resource: builtinPodResourceKind(), Cluster: row.Cluster, Namespace: row.Namespace, Name: row.Name, Depth: 0})
		return true
	})
	return targets
}

func (a *App) directChildJumpTargets(ctx context.Context, backend genericResourceBackend, catalog []cluster.ResourceGroup, source resourceJumpSource, depth int) []resourceJumpTarget {
	targets := make([]resourceJumpTarget, 0, 8)
	seen := make(map[string]struct{}, 8)

	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if len(targets) >= resourceJumpTargetLimit {
			return false
		}
		if row.Cluster != source.Cluster || pod == nil {
			return true
		}
		if !ownerReferenceMatchesAny(pod.GetOwnerReferences(), source, row.Namespace) {
			return true
		}
		targets = addJumpTarget(targets, seen, resourceJumpTarget{Label: jumpTargetLabel(builtinPodResourceKind(), row.Cluster, row.Namespace, row.Name), Resource: builtinPodResourceKind(), Cluster: row.Cluster, Namespace: row.Namespace, Name: row.Name, Depth: depth})
		return true
	})

	if backend == nil {
		panic("app.directChildJumpTargets: nil backend")
	}
	if ctx == nil {
		panic("app.directChildJumpTargets: nil context")
	}
	for _, resource := range a.descendantGenericCandidateResources(catalog) {
		if len(targets) >= resourceJumpTargetLimit {
			break
		}
		_ = backend.ForEachGenericResourceRow(ctx, resource, func(row cluster.GenericResourceRow) bool {
			if len(targets) >= resourceJumpTargetLimit {
				return false
			}
			if row.Cluster != source.Cluster || row.Object == nil {
				return true
			}
			if !ownerReferenceMatchesAny(row.Object.GetOwnerReferences(), source, row.Namespace) {
				return true
			}
			targets = addJumpTarget(targets, seen, resourceJumpTarget{Label: jumpTargetLabel(resource, row.Cluster, row.Namespace, row.Name), Resource: resource, Cluster: row.Cluster, Namespace: row.Namespace, Name: row.Name, Depth: depth})
			return true
		})
	}

	sortJumpTargets(targets)
	return targets
}

func ownerReferenceMatchesAny(refs []metav1.OwnerReference, source resourceJumpSource, childNamespace string) bool {
	for _, ref := range refs {
		if ownerRefMatchesSource(ref, source, childNamespace) {
			return true
		}
	}
	return false
}

func (a *App) childJumpTargets(ctx context.Context, backend genericResourceBackend, catalog []cluster.ResourceGroup, source resourceJumpSource, now time.Time) []resourceJumpTarget {
	targets := make([]resourceJumpTarget, 0, 8)
	seen := make(map[string]struct{}, 8)
	visited := make(map[string]struct{}, 8)

	switch {
	case source.Deployment != nil:
		targets = a.appendDeploymentPods(targets, seen, source)
	case source.Service != nil:
		targets = a.appendServicePods(targets, seen, source)
	case source.Node != nil:
		targets = a.appendNodePods(targets, seen, source)
	}

	var walk func(resourceJumpSource, int)
	walk = func(current resourceJumpSource, depth int) {
		if depth >= resourceJumpDepthLimit || len(targets) >= resourceJumpTargetLimit {
			return
		}
		visitKey := jumpSourceVisitKey(current)
		if _, ok := visited[visitKey]; ok {
			return
		}
		visited[visitKey] = struct{}{}
		for _, target := range a.directChildJumpTargets(ctx, backend, catalog, current, depth) {
			before := len(targets)
			targets = addJumpTarget(targets, seen, target)
			if len(targets) >= resourceJumpTargetLimit {
				return
			}
			if len(targets) == before {
				continue
			}
			next, ok := a.fetchJumpSourceFromTarget(ctx, backend, catalog, target, now)
			if ok {
				walk(next, depth+1)
			}
		}
	}
	walk(source, 0)
	sortJumpTargets(targets)
	return targets
}

func (a *App) openJumpTargets(title string, targets []resourceJumpTarget, now time.Time) tea.Cmd {
	if len(targets) == 0 {
		return nil
	}
	if len(targets) == 1 {
		return a.openResourceJumpTarget(targets[0], now)
	}
	options := make([]actionOption, 0, len(targets))
	for _, target := range targets {
		options = append(options, actionOption{Label: target.Label, JumpTarget: target})
	}
	return a.openActionPicker(title, "enter select  esc cancel", pendingActionResourceJump, options)
}

func (a *App) tryOpenOwnerJump(now time.Time) (tea.Cmd, bool) {
	return a.beginJumpDiscovery(jumpDiscoverOwners, now)
}

func (a *App) tryOpenChildJump(now time.Time) (tea.Cmd, bool) {
	return a.beginJumpDiscovery(jumpDiscoverChildren, now)
}

func (a *App) beginJumpDiscovery(kind jumpDiscoveryKind, now time.Time) (tea.Cmd, bool) {
	source, err := a.currentJumpSource(now)
	if err != nil {
		a.statusMessage = err.Error()
		return nil, true
	}
	token := a.nextAsyncTokenValue()
	a.genericJumpToken = token
	a.genericJumpLoading = true
	a.activity = "resolving jump targets"

	backend := a.genericBackend
	rootCtx := a.context
	catalog := a.manager.Catalog()
	walker := a
	capturedSource := source
	capturedKind := kind
	capturedNow := now
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(rootCtx, genericRequestTimeout)
		defer cancel()
		resolved := capturedSource
		if resolved.Object == nil && resolved.Name != "" && !isBuiltinJumpResource(resolved.Resource) {
			details, fetchErr := backend.GenericResourceDetails(ctx, resolved.Resource, cluster.GenericResourceKey{
				Cluster:   resolved.Cluster,
				Namespace: resolved.Namespace,
				Name:      resolved.Name,
			}, capturedNow)
			if fetchErr != nil || details.Object == nil {
				return resourceJumpDiscoveryResultMsg{Token: token, Kind: capturedKind, Err: fmt.Errorf("resource vanished during refresh")}
			}
			resolved = jumpSourceFromGenericDetails(resolved.Resource, details)
		}
		var targets []resourceJumpTarget
		switch capturedKind {
		case jumpDiscoverOwners:
			targets = walker.ownerJumpTargets(ctx, backend, catalog, resolved, capturedNow)
		case jumpDiscoverChildren:
			targets = walker.childJumpTargets(ctx, backend, catalog, resolved, capturedNow)
		default:
			return resourceJumpDiscoveryResultMsg{Token: token, Kind: capturedKind, Err: fmt.Errorf("unsupported jump discovery")}
		}
		return resourceJumpDiscoveryResultMsg{Token: token, Kind: capturedKind, Targets: targets}
	}, true
}

func isBuiltinJumpResource(resource cluster.ResourceKind) bool {
	switch {
	case resource.Resource == "pods" && resource.APIGroup == "":
		return true
	case resource.Resource == "deployments" && resource.APIGroup == "apps":
		return true
	case resource.Resource == "services" && resource.APIGroup == "":
		return true
	case resource.Resource == "nodes" && resource.APIGroup == "":
		return true
	default:
		return false
	}
}

func (a *App) openResourceJumpTarget(target resourceJumpTarget, now time.Time) tea.Cmd {
	switch {
	case target.Resource.Resource == "pods" && target.Resource.APIGroup == "":
		details, ok := a.store.PodDetailsByKey(state.PodKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			return nil
		}
		a.activeResource = builtinPodResourceKind()
		a.activePod = details
		a.activePodUsage = a.podUsageByKey[details.Row.Key.String()]
		a.podUsageFetchedAt = time.Time{}
		a.podUsageLoading = false
		a.lastDataVersion = a.store.PodVersion()
		a.lastTick = now
		if a.actionReturnScreen == screenResourceDetails || a.screen == screenResourceDetails {
			a.podDetailReturnScreen = screenResourceDetails
		} else {
			a.podDetailReturnScreen = screenPods
		}
		a.screen = screenPodDetails
		a.resetTextViewport()
		return a.maybeRefreshResourceUsageCmd(now)
	case target.Resource.Resource == "deployments" && target.Resource.APIGroup == "apps":
		details, ok := a.store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			return nil
		}
		a.activeResource = builtinDeploymentResourceKind()
		a.activeDeployment = details
		a.activeDeploymentPods = a.deploymentAssociatedPods(now)
		a.screen = screenResourceDetails
		a.detailFocus = detailFocusContent
		a.lastDataVersion = a.store.DeploymentVersion()
		a.lastTick = now
		a.resetTextViewport()
		return a.maybeRefreshResourceUsageCmd(now)
	case target.Resource.Resource == "services" && target.Resource.APIGroup == "":
		details, ok := a.store.ServiceDetailsByKey(state.ServiceKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}, now)
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			return nil
		}
		a.activeResource = builtinServiceResourceKind()
		a.activeService = details
		a.activeServicePods = a.serviceAssociatedPods(now)
		a.screen = screenResourceDetails
		a.detailFocus = detailFocusContent
		a.lastDataVersion = a.store.ServiceVersion()
		a.lastTick = now
		a.resetTextViewport()
		return a.maybeRefreshResourceUsageCmd(now)
	case target.Resource.Resource == "nodes" && target.Resource.APIGroup == "":
		details, ok := a.store.NodeDetailsByKey(state.NodeKey{Cluster: target.Cluster, Name: target.Name}, now)
		if !ok {
			a.statusMessage = "resource vanished during refresh"
			return nil
		}
		a.activeResource = builtinNodeResourceKind()
		a.activeNode = details
		a.activeNodePods = a.nodeAssociatedPods(now)
		a.activeNodeUsage = a.nodeUsageByKey[details.Row.Key.String()]
		a.nodeUsageFetchedAt = time.Time{}
		a.nodeUsageLoading = false
		a.screen = screenResourceDetails
		a.detailFocus = detailFocusContent
		a.lastDataVersion = a.store.NodeVersion()
		a.lastTick = now
		a.resetTextViewport()
		return a.maybeRefreshResourceUsageCmd(now)
	default:
		token := a.nextAsyncTokenValue()
		a.genericJumpToken = token
		a.genericJumpLoading = true
		a.activity = "loading jump target"
		backend := a.genericBackend
		rootCtx := a.context
		resource := target.Resource
		key := cluster.GenericResourceKey{Cluster: target.Cluster, Namespace: target.Namespace, Name: target.Name}
		capturedTarget := target
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(rootCtx, genericRequestTimeout)
			defer cancel()
			details, err := backend.GenericResourceDetails(ctx, resource, key, time.Now())
			return resourceJumpResultMsg{Token: token, Target: capturedTarget, Details: details, Err: err, Resource: resource}
		}
	}
}
