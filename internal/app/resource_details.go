package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

const detailAssociatedPodLimit = 24

var (
	detailCardStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	detailSummaryCardStyle = detailCardStyle.Copy().BorderForeground(lipgloss.Color("14")).Bold(true)
	detailInsetCardStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 0)
)

type detailField struct {
	Label string
	Value string
}

func (a *App) renderDeploymentDetails() string {
	details := a.activeDeployment
	deployment := details.Deployment
	if deployment == nil {
		return "deployment disappeared"
	}
	contentWidth := a.resourceDetailContentWidth()
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("Deployment", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Ready", Value: details.Row.Ready}, {Label: "Selector", Value: metav1.FormatLabelSelector(deployment.Spec.Selector)}, {Label: "Strategy", Value: deploymentStrategyString(deployment)}}, contentWidth, true), Span: detailLayoutSpanFull},
		{Rendered: a.renderDeploymentTopRow(contentWidth, deployment), Span: detailLayoutSpanFull},
		{Rendered: renderDeploymentContainersSection(contentWidth, deployment.Spec.Template.Spec.Containers), Span: detailLayoutSpanFull},
		{Rendered: renderDeploymentMetadataAnnotationsRow(contentWidth, deployment.Labels, deployment.Annotations), Span: detailLayoutSpanFull},
		{Rendered: renderDeploymentPodSpecSection(deployment.Spec.Template), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(contentWidth, blocks)
}

func (a *App) renderServiceDetails() string {
	details := a.activeService
	service := details.Service
	if service == nil {
		return "service disappeared"
	}
	contentWidth := a.resourceDetailContentWidth()
	blocks := []detailLayoutBlock{
		{Rendered: renderServiceSummarySection(contentWidth, details, service), Span: detailLayoutSpanFull},
		{Rendered: renderServiceAuxiliarySection(contentWidth, service), Span: detailLayoutSpanFull},
		{Rendered: renderServicePortCardsSection(contentWidth, service.Spec.Ports), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(contentWidth, blocks)
}

func (a *App) renderNodeDetails() string {
	details := a.activeNode
	node := details.Node
	if node == nil {
		return "node disappeared"
	}
	contentWidth := a.resourceDetailContentWidth()
	info := node.Status.NodeInfo
	blocks := []detailLayoutBlock{{Rendered: renderDetailFieldSectionSized("Node", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Status", Value: details.Row.Status}, {Label: "Roles", Value: details.Row.Roles}, {Label: "Unschedulable", Value: boolString(node.Spec.Unschedulable)}, {Label: "OS image", Value: info.OSImage}, {Label: "Kernel", Value: info.KernelVersion}, {Label: "Kubelet", Value: info.KubeletVersion}, {Label: "Kube-proxy", Value: info.KubeProxyVersion}, {Label: "Container runtime", Value: info.ContainerRuntimeVersion}, {Label: "Architecture", Value: info.Architecture}, {Label: "Provider ID", Value: node.Spec.ProviderID}, {Label: "Pod CIDR", Value: strings.Join(node.Spec.PodCIDRs, ", ")}, {Label: "Hostname", Value: nodeAddress(node, corev1.NodeHostName)}, {Label: "Internal IP", Value: nodeAddress(node, corev1.NodeInternalIP)}, {Label: "External IP", Value: nodeAddress(node, corev1.NodeExternalIP)}}, contentWidth, true), Span: detailLayoutSpanFull}}
	if utilization := a.renderNodeUtilizationSection(contentWidth, node); utilization != "" {
		blocks = append(blocks, detailLayoutBlock{Rendered: utilization, Span: detailLayoutSpanFull})
	}
	blocks = append(blocks,
		detailLayoutBlock{Rendered: renderNodeConditionSection(node.Status.Conditions), Span: detailLayoutSpanCompact},
		detailLayoutBlock{Rendered: renderNodeTaintsSection(node.Spec.Taints), Span: detailLayoutSpanCompact},
		detailLayoutBlock{Rendered: renderMetadataSection(node.Labels, node.Annotations), Span: detailLayoutSpanCompact},
	)
	return renderDetailBlockLayout(contentWidth, blocks)
}

func renderPodDetails(width int, details state.PodDetails, usage cluster.PodResourceUsage, usageLabel string) string {
	pod := details.Pod
	if pod == nil {
		return "pod disappeared"
	}
	blocks := []detailLayoutBlock{{Rendered: renderDetailFieldSectionSized("Pod", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Phase", Value: string(pod.Status.Phase)}, {Label: "Ready", Value: details.Row.Ready}, {Label: "Restarts", Value: fmt.Sprintf("%d", details.Row.Restarts)}, {Label: "Started", Value: formatMetaTime(pod.Status.StartTime)}, {Label: "Pod IP", Value: pod.Status.PodIP}, {Label: "Host IP", Value: pod.Status.HostIP}, {Label: "Node", Value: pod.Spec.NodeName}, {Label: "Service account", Value: defaultString(pod.Spec.ServiceAccountName, "default")}, {Label: "QoS", Value: string(pod.Status.QOSClass)}, {Label: "Priority class", Value: pod.Spec.PriorityClassName}, {Label: "Runtime class", Value: derefString(pod.Spec.RuntimeClassName)}}, width, true), Span: detailLayoutSpanFull}}
	if usageSection := renderPodUsageSectionWithLabel(usage, usageLabel); usageSection != "" {
		blocks = append(blocks, detailLayoutBlock{Rendered: renderDetailCardContent(usageSection, false), Span: detailLayoutSpanCompact})
	}
	blocks = append(blocks,
		detailLayoutBlock{Rendered: renderPodConditionSection(pod.Status.Conditions), Span: detailLayoutSpanCompact},
		detailLayoutBlock{Rendered: renderPodSchedulingSection(pod.Spec), Span: detailLayoutSpanCompact},
		detailLayoutBlock{Rendered: renderMetadataSection(pod.Labels, pod.Annotations), Span: detailLayoutSpanCompact},
		detailLayoutBlock{Rendered: renderPodContainerSection("Containers", pod.Spec.Containers), Span: detailLayoutSpanFull},
		detailLayoutBlock{Rendered: renderPodContainerSection("Init containers", pod.Spec.InitContainers), Span: detailLayoutSpanFull},
	)
	return renderDetailBlockLayout(width, blocks)
}

func (a *App) refreshAssociatedPods(now time.Time) {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		a.activeDeploymentPods = a.deploymentAssociatedPods(now)
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		a.activeServicePods = a.serviceAssociatedPods(now)
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		a.activeNodePods = a.nodeAssociatedPods(now)
	}
}

func (a *App) deploymentAssociatedPods(now time.Time) []state.PodRow {
	deployment := a.activeDeployment.Deployment
	if deployment == nil || deployment.Spec.Selector == nil {
		return nil
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil || selector.Empty() {
		return nil
	}
	rows := make([]state.PodRow, 0, 16)
	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if row.Cluster != a.activeDeployment.Row.Cluster || row.Namespace != a.activeDeployment.Row.Namespace || pod == nil {
			return true
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			rows = append(rows, row.WithAge(now))
		}
		return true
	})
	return rows
}

func (a *App) serviceAssociatedPods(now time.Time) []state.PodRow {
	service := a.activeService.Service
	if service == nil || len(service.Spec.Selector) == 0 {
		return nil
	}
	selector := labels.SelectorFromSet(service.Spec.Selector)
	rows := make([]state.PodRow, 0, 16)
	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if row.Cluster != a.activeService.Row.Cluster || row.Namespace != a.activeService.Row.Namespace || pod == nil {
			return true
		}
		if selector.Matches(labels.Set(pod.Labels)) {
			rows = append(rows, row.WithAge(now))
		}
		return true
	})
	return rows
}

func (a *App) nodeAssociatedPods(now time.Time) []state.PodRow {
	nodeName := a.activeNode.Row.Name
	if nodeName == "" {
		return nil
	}
	rows := make([]state.PodRow, 0, 32)
	a.store.ForEachPod(func(row state.PodRow) bool {
		if row.Cluster != a.activeNode.Row.Cluster || row.Node != nodeName {
			return true
		}
		rows = append(rows, row.WithAge(now))
		return true
	})
	return rows
}

func (a *App) renderTypedGenericResourceDetails() (string, bool) {
	if a.activeGenericDetails.Object == nil {
		return "", false
	}
	width := a.resourceDetailContentWidth()
	switch {
	case a.activeResource.Resource == "daemonsets" && a.activeResource.APIGroup == "apps":
		return renderTypedDaemonSetDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "statefulsets" && a.activeResource.APIGroup == "apps":
		return renderTypedStatefulSetDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "jobs" && a.activeResource.APIGroup == "batch":
		return renderTypedJobDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "configmaps" && a.activeResource.APIGroup == "":
		return renderTypedConfigMapDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "horizontalpodautoscalers" && a.activeResource.APIGroup == "autoscaling":
		return renderTypedHorizontalPodAutoscalerDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "poddisruptionbudgets" && a.activeResource.APIGroup == "policy":
		return renderTypedPodDisruptionBudgetDetails(width, a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "persistentvolumeclaims" && a.activeResource.APIGroup == "":
		return renderTypedPersistentVolumeClaimDetails(width, a.activeResource, a.activeGenericDetails), true
	default:
		return "", false
	}
}

func renderTypedDaemonSetDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("DaemonSet", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")}, {Label: "Selector", Value: formatObjectMapInline(nestedStringMap(obj.Object, "spec", "selector", "matchLabels"))}, {Label: "Update strategy", Value: nestedString(obj.Object, "spec", "updateStrategy", "type")}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderDetailSection("Rollout", []string{fmt.Sprintf("- Desired:      %d", nestedInt64(obj.Object, "status", "desiredNumberScheduled")), fmt.Sprintf("- Current:      %d", nestedInt64(obj.Object, "status", "currentNumberScheduled")), fmt.Sprintf("- Updated:      %d", nestedInt64(obj.Object, "status", "updatedNumberScheduled")), fmt.Sprintf("- Ready:        %d", nestedInt64(obj.Object, "status", "numberReady")), fmt.Sprintf("- Available:    %d", nestedInt64(obj.Object, "status", "numberAvailable")), fmt.Sprintf("- Unavailable:  %d", nestedInt64(obj.Object, "status", "numberUnavailable"))}), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericConditionsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
		{Rendered: renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedStatefulSetDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("StatefulSet", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")}, {Label: "Service", Value: nestedString(obj.Object, "spec", "serviceName")}, {Label: "Pod policy", Value: nestedString(obj.Object, "spec", "podManagementPolicy")}, {Label: "Update strategy", Value: nestedString(obj.Object, "spec", "updateStrategy", "type")}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderDetailSection("Rollout", []string{fmt.Sprintf("- Desired:      %d", nestedInt64(obj.Object, "spec", "replicas")), fmt.Sprintf("- Current:      %d", nestedInt64(obj.Object, "status", "currentReplicas")), fmt.Sprintf("- Updated:      %d", nestedInt64(obj.Object, "status", "updatedReplicas")), fmt.Sprintf("- Ready:        %d", nestedInt64(obj.Object, "status", "readyReplicas")), fmt.Sprintf("- Available:    %d", nestedInt64(obj.Object, "status", "availableReplicas"))}), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericConditionsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderStatefulSetVolumeClaimsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
		{Rendered: renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedJobDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("Job", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")}, {Label: "Parallelism", Value: nestedIntString(obj.Object, "spec", "parallelism")}, {Label: "Completions", Value: nestedIntString(obj.Object, "spec", "completions")}, {Label: "Completion mode", Value: nestedString(obj.Object, "spec", "completionMode")}, {Label: "Backoff limit", Value: nestedIntString(obj.Object, "spec", "backoffLimit")}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderDetailSection("Status", []string{fmt.Sprintf("- Active:      %d", nestedInt64(obj.Object, "status", "active")), fmt.Sprintf("- Succeeded:   %d", nestedInt64(obj.Object, "status", "succeeded")), fmt.Sprintf("- Failed:      %d", nestedInt64(obj.Object, "status", "failed"))}), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericConditionsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
		{Rendered: renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedConfigMapDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	data := nestedStringMap(obj.Object, "data")
	binaryData := nestedStringMap(obj.Object, "binaryData")
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("ConfigMap", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Immutable", Value: boolString(nestedBool(obj.Object, "immutable"))}, {Label: "Data keys", Value: fmt.Sprintf("%d", len(data))}, {Label: "Binary data keys", Value: fmt.Sprintf("%d", len(binaryData))}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderConfigMapBinaryDataSection(binaryData), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
		{Rendered: renderConfigMapDataSection(data), Span: detailLayoutSpanFull},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedHorizontalPodAutoscalerDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("HorizontalPodAutoscaler", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Scale target", Value: formatHPAScaleTargetRef(obj.Object)}, {Label: "Min replicas", Value: defaultString(nestedIntString(obj.Object, "spec", "minReplicas"), "1")}, {Label: "Max replicas", Value: nestedIntString(obj.Object, "spec", "maxReplicas")}, {Label: "Current replicas", Value: nestedIntString(obj.Object, "status", "currentReplicas")}, {Label: "Desired replicas", Value: nestedIntString(obj.Object, "status", "desiredReplicas")}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderHPAMetricsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderHPABehaviorSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericConditionsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedPodDisruptionBudgetDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("PodDisruptionBudget", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Selector", Value: labelSelectorStringFromObject(obj.Object, "spec", "selector")}, {Label: "Min available", Value: nestedScalarString(obj.Object, "spec", "minAvailable")}, {Label: "Max unavailable", Value: nestedScalarString(obj.Object, "spec", "maxUnavailable")}, {Label: "Unhealthy pod eviction", Value: nestedString(obj.Object, "spec", "unhealthyPodEvictionPolicy")}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderDetailSection("Status", []string{fmt.Sprintf("- Current healthy:      %d", nestedInt64(obj.Object, "status", "currentHealthy")), fmt.Sprintf("- Desired healthy:      %d", nestedInt64(obj.Object, "status", "desiredHealthy")), fmt.Sprintf("- Expected pods:        %d", nestedInt64(obj.Object, "status", "expectedPods")), fmt.Sprintf("- Disruptions allowed:  %d", nestedInt64(obj.Object, "status", "disruptionsAllowed")), fmt.Sprintf("- Disrupted pods:       %d", len(nestedMap(obj.Object, "status", "disruptedPods")))}), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericConditionsSection(obj.Object), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderTypedPersistentVolumeClaimDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	claim, ok := persistentVolumeClaimFromObject(obj.Object)
	if !ok {
		return renderGenericResourceContextSection(resource, details)
	}
	status := details.Row.Status
	if claim.Status.Phase != "" {
		status = string(claim.Status.Phase)
	}
	blocks := []detailLayoutBlock{
		{Rendered: renderDetailFieldSectionSized("PersistentVolumeClaim", []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}, {Label: "Status", Value: defaultString(status, "unknown")}, {Label: "Volume", Value: claim.Spec.VolumeName}, {Label: "Storage class", Value: derefString(claim.Spec.StorageClassName)}, {Label: "Access modes", Value: formatPersistentVolumeAccessModes(claim.Spec.AccessModes)}, {Label: "Volume mode", Value: formatPersistentVolumeMode(claim.Spec.VolumeMode)}, {Label: "Requested storage", Value: resourceListQuantityString(claim.Spec.Resources.Requests, corev1.ResourceStorage)}, {Label: "Capacity", Value: resourceListQuantityString(claim.Status.Capacity, corev1.ResourceStorage)}}, width, true), Span: detailLayoutSpanFull},
		{Rendered: renderPVCBindingSourceSection(claim), Span: detailLayoutSpanCompact},
		{Rendered: renderPVCConditionSection(claim.Status.Conditions), Span: detailLayoutSpanCompact},
		{Rendered: renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()), Span: detailLayoutSpanCompact},
		{Rendered: renderGenericResourceContextSection(resource, details), Span: detailLayoutSpanCompact},
	}
	return renderDetailBlockLayout(width, blocks)
}

func renderCustomResourceDetails(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	resourceGroups := customResourceTopGroups(resource, details, obj)
	cards := []detailLayoutCard{{
		Title:    "Resource",
		Groups:   resourceGroups,
		Standout: true,
		Span:     detailLayoutSpanFull,
	}}
	cards = append(cards, renderCustomResourceMetadataCards(width, resource, details, obj)...)
	if specCard, ok := renderYAMLCard("Spec", nestedMap(obj.Object, "spec"), detailLayoutSpanFull); ok {
		cards = append(cards, specCard)
	}
	return renderDetailCardLayout(width, cards)
}

func customResourceTopGroups(resource cluster.ResourceKind, details cluster.GenericResourceDetails, object *unstructured.Unstructured) []detailLayoutGroup {
	status := nestedMap(object.Object, "status")
	scalar, nested := splitObjectFields(status, map[string]struct{}{"conditions": struct{}{}})
	statusFields := make([]detailField, 0, len(scalar))
	keys := make([]string, 0, len(scalar))
	for key := range scalar {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		statusFields = append(statusFields, detailField{Label: humanizeObjectKey(key), Value: scalarString(scalar[key])})
	}
	groups := []detailLayoutGroup{
		{Title: "Overview", Fields: []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: defaultString(details.Row.Namespace, "cluster")}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Kind", Value: defaultString(resource.Kind, object.GetKind())}}},
		{Title: "Runtime", Fields: []detailField{{Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")}, {Label: "Status", Value: defaultString(details.Row.Status, "unknown")}, {Label: "Age", Value: details.Row.Age}}},
		{Title: "Status", Fields: statusFields},
		{Title: "Conditions", Lines: genericConditionLines(object.Object)},
	}
	if lines := renderYAMLLines(nested); len(lines) != 0 {
		groups = append(groups, detailLayoutGroup{Title: "Status details", Lines: lines})
	}
	return groups
}

func renderCustomResourceMetadataCards(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails, object *unstructured.Unstructured) []detailLayoutCard {
	if object == nil {
		return nil
	}
	contentWidth := max(20, width-detailCardStyle.GetHorizontalFrameSize()-detailCardStyle.GetHorizontalPadding())
	body := renderCustomResourceMetadataBody(contentWidth, resource, details, object)
	card := detailLayoutCard{Title: "Metadata", Lines: []string{body}, Span: detailLayoutSpanFull}
	if isEmptyDetailLayoutCard(card) {
		return nil
	}
	return []detailLayoutCard{card}
}

func renderCustomResourceMetadataBody(width int, resource cluster.ResourceKind, details cluster.GenericResourceDetails, object *unstructured.Unstructured) string {
	const gap = 4
	smallWidth := max(24, (width-2*gap)/4)
	largeWidth := max(32, width-2*gap-2*smallWidth)
	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Overview", Fields: []detailField{{Label: "Name", Value: object.GetName()}, {Label: "Namespace", Value: object.GetNamespace()}, {Label: "Generation", Value: fmt.Sprintf("%d", object.GetGeneration())}, {Label: "Resource version", Value: object.GetResourceVersion()}, {Label: "Created", Value: formatObjectTime(object.GetCreationTimestamp())}}}, smallWidth),
		strings.Repeat(" ", gap),
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Context", Lines: []string{"- Kind: " + defaultString(resource.Kind, details.Object.GetKind()), "- API group: " + defaultString(resource.APIGroup, "core"), "- API version: " + defaultString(resource.Version, "server-default"), "- Resource: " + resource.Resource, "- Status: " + defaultString(details.Row.Status, "unknown")}}, smallWidth),
		strings.Repeat(" ", gap),
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Labels", Lines: stringMapLines(object.GetLabels())}, largeWidth),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Owner references", Lines: ownerReferenceLines(object.GetOwnerReferences())}, smallWidth),
		strings.Repeat(" ", gap),
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Finalizers", Lines: stringListLines(object.GetFinalizers())}, smallWidth),
		strings.Repeat(" ", gap),
		renderDetailLayoutGroupCell(detailLayoutGroup{Title: "Annotations", Lines: stringMapLines(object.GetAnnotations())}, largeWidth),
	)
	if strings.TrimSpace(row2) == "" {
		return row1
	}
	return row1 + "\n\n" + row2
}

func renderDetailLayoutGroupCell(group detailLayoutGroup, width int) string {
	if isEmptyDetailLayoutGroup(group) {
		return lipgloss.NewStyle().Width(width).Render("")
	}
	return lipgloss.NewStyle().Width(width).Render(renderDetailLayoutGroup(group, width))
}

func renderOwnerReferencesSection(refs []metav1.OwnerReference) string {
	card, ok := renderOwnerReferencesCard(refs, detailLayoutSpanCompact)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func renderOwnerReferencesCard(refs []metav1.OwnerReference, span detailLayoutSpan) (detailLayoutCard, bool) {
	lines := ownerReferenceLines(refs)
	if len(lines) == 0 {
		return detailLayoutCard{}, false
	}
	return detailLayoutCard{Title: "Owner references", Lines: lines, Span: span}, true
}

func renderStringListSection(title string, values []string) string {
	card, ok := renderStringListCard(title, values, detailLayoutSpanCompact)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func renderStringListCard(title string, values []string, span detailLayoutSpan) (detailLayoutCard, bool) {
	lines := stringListLines(values)
	if len(lines) == 0 {
		return detailLayoutCard{}, false
	}
	return detailLayoutCard{Title: title, Lines: lines, Span: span}, true
}

func renderObjectScalarFieldSection(title string, object map[string]interface{}, skip map[string]struct{}) string {
	scalar, _ := splitObjectFields(object, skip)
	card, ok := renderObjectScalarFieldCard(title, scalar, detailLayoutSpanCompact)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func renderObjectScalarFieldCard(title string, object map[string]interface{}, span detailLayoutSpan) (detailLayoutCard, bool) {
	if len(object) == 0 {
		return detailLayoutCard{}, false
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return detailLayoutCard{}, false
	}
	sort.Strings(keys)
	fields := make([]detailField, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, detailField{Label: humanizeObjectKey(key), Value: scalarString(object[key])})
	}
	return detailLayoutCard{Title: title, Fields: fields, Span: span}, true
}

func renderGenericConditionsCard(object map[string]interface{}, span detailLayoutSpan) (detailLayoutCard, bool) {
	lines := genericConditionLines(object)
	if len(lines) == 0 {
		return detailLayoutCard{}, false
	}
	return detailLayoutCard{Title: "Conditions", Lines: lines, Span: span}, true
}

func genericConditionLines(object map[string]interface{}) []string {
	items, found, _ := unstructured.NestedSlice(object, "status", "conditions")
	if !found || len(items) == 0 {
		return nil
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		condition, _ := item.(map[string]interface{})
		line := "- " + nestedString(condition, "type") + "=" + nestedString(condition, "status")
		if reason := nestedString(condition, "reason"); reason != "" {
			line += " reason=" + reason
		}
		if message := nestedString(condition, "message"); message != "" {
			line += " msg=" + message
		}
		lines = append(lines, line)
	}
	return lines
}

func renderYAMLSection(title string, value interface{}) string {
	card, ok := renderYAMLCard(title, value, detailLayoutSpanFull)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func renderYAMLCard(title string, value interface{}, span detailLayoutSpan) (detailLayoutCard, bool) {
	lines := renderYAMLLines(value)
	if len(lines) == 0 {
		return detailLayoutCard{}, false
	}
	return detailLayoutCard{Title: title, Lines: lines, Span: span}, true
}

func renderYAMLLines(value interface{}) []string {
	if isEmptyValue(value) {
		return nil
	}
	content, err := yaml.Marshal(value)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func splitObjectFields(object map[string]interface{}, skip map[string]struct{}) (map[string]interface{}, map[string]interface{}) {
	if len(object) == 0 {
		return nil, nil
	}
	scalar := make(map[string]interface{}, len(object))
	nested := make(map[string]interface{}, len(object))
	for key, value := range object {
		if _, ok := skip[key]; ok {
			continue
		}
		if isScalarValue(value) {
			scalar[key] = value
			continue
		}
		nested[key] = value
	}
	return scalar, nested
}

func isScalarValue(value interface{}) bool {
	switch value.(type) {
	case nil, string, bool, int, int32, int64, float64:
		return true
	default:
		return false
	}
}

func isEmptyValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case map[string]interface{}:
		return len(typed) == 0
	case []interface{}:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case string:
		return strings.TrimSpace(typed) == ""
	default:
		return false
	}
}

func humanizeObjectKey(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-'
	})
	if len(parts) == 0 {
		parts = []string{key}
	}
	for idx := range parts {
		if parts[idx] == "" {
			continue
		}
		parts[idx] = strings.ToUpper(parts[idx][:1]) + parts[idx][1:]
	}
	return strings.Join(parts, " ")
}

func formatObjectTime(value metav1.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}

func renderGenericResourceContextSection(resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	return renderDetailSection("Resource context", []string{
		"- Kind: " + defaultString(resource.Kind, details.Object.GetKind()),
		"- API group: " + defaultString(resource.APIGroup, "core"),
		"- API version: " + defaultString(resource.Version, "server-default"),
		"- Resource: " + resource.Resource,
		"- Status: " + defaultString(details.Row.Status, "unknown"),
	})
}

func renderStatefulSetVolumeClaimsSection(object map[string]interface{}) string {
	items, found, _ := unstructured.NestedSlice(object, "spec", "volumeClaimTemplates")
	if !found || len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		claim, _ := item.(map[string]interface{})
		name, _, _ := unstructured.NestedString(claim, "metadata", "name")
		storageClass, _, _ := unstructured.NestedString(claim, "spec", "storageClassName")
		volumeMode, _, _ := unstructured.NestedString(claim, "spec", "volumeMode")
		requests := nestedStringMap(claim, "spec", "resources", "requests")
		parts := []string{name}
		if storageClass != "" {
			parts = append(parts, "class="+storageClass)
		}
		if volumeMode != "" {
			parts = append(parts, "mode="+volumeMode)
		}
		if req := formatObjectMapInline(requests); req != "" {
			parts = append(parts, "requests="+req)
		}
		lines = append(lines, "- "+strings.Join(parts, "  "))
	}
	return renderDetailSection("Volume claims", lines)
}

func renderGenericConditionsSection(object map[string]interface{}) string {
	items, found, _ := unstructured.NestedSlice(object, "status", "conditions")
	if !found || len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		condition, _ := item.(map[string]interface{})
		line := "- " + nestedString(condition, "type") + "=" + nestedString(condition, "status")
		if reason := nestedString(condition, "reason"); reason != "" {
			line += " reason=" + reason
		}
		if message := nestedString(condition, "message"); message != "" {
			line += " msg=" + message
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Conditions", lines)
}

func renderConfigMapDataSection(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := sortedMapKeys(values)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- %s  %s", key, summarizeTextBlob(values[key])))
	}
	return renderDetailSection("Data", lines)
}

func renderConfigMapBinaryDataSection(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := sortedMapKeys(values)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, "- "+key)
	}
	return renderDetailSection("Binary data", lines)
}

func summarizeTextBlob(value string) string {
	size := len(value)
	trimmed := strings.TrimSuffix(value, "\n")
	lineCount := 0
	if trimmed != "" {
		lineCount = 1 + strings.Count(trimmed, "\n")
	}
	if lineCount == 0 {
		return fmt.Sprintf("%d B", size)
	}
	label := "lines"
	if lineCount == 1 {
		label = "line"
	}
	return fmt.Sprintf("%d B  %d %s", size, lineCount, label)
}

func formatHPAScaleTargetRef(object map[string]interface{}) string {
	target := nestedMap(object, "spec", "scaleTargetRef")
	if len(target) == 0 {
		return ""
	}
	parts := []string{kindNameString(nestedString(target, "kind"), nestedString(target, "name"))}
	if apiVersion := nestedString(target, "apiVersion"); apiVersion != "" {
		parts = append(parts, "api="+apiVersion)
	}
	return strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "  "))
}

func renderHPAMetricsSection(object map[string]interface{}) string {
	specMetrics, found, _ := unstructured.NestedSlice(object, "spec", "metrics")
	statusMetrics, _, _ := unstructured.NestedSlice(object, "status", "currentMetrics")
	lines := make([]string, 0, len(specMetrics)+1)
	if found {
		for idx, item := range specMetrics {
			metric, _ := item.(map[string]interface{})
			var current map[string]interface{}
			if idx < len(statusMetrics) {
				current, _ = statusMetrics[idx].(map[string]interface{})
			}
			if line := formatHPAMetricLine(metric, current); line != "" {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		currentCPU := nestedIntString(object, "status", "currentCPUUtilizationPercentage")
		if currentCPU != "" {
			line := "- resource=cpu"
			if targetCPU := nestedIntString(object, "spec", "targetCPUUtilizationPercentage"); targetCPU != "" {
				line += "  target=avgUtil=" + targetCPU + "%"
			}
			line += "  current=avgUtil=" + currentCPU + "%"
			lines = append(lines, line)
		}
	}
	return renderDetailSection("Metrics", lines)
}

func formatHPAMetricLine(metric map[string]interface{}, current map[string]interface{}) string {
	if len(metric) == 0 {
		return ""
	}
	label, target := formatHPAMetricSpec(metric)
	parts := make([]string, 0, 3)
	if label != "" {
		parts = append(parts, label)
	}
	if target != "" {
		parts = append(parts, "target="+target)
	}
	if currentValue := formatHPACurrentMetric(metric, current); currentValue != "" {
		parts = append(parts, "current="+currentValue)
	}
	if len(parts) == 0 {
		return ""
	}
	return "- " + strings.Join(parts, "  ")
}

func formatHPAMetricSpec(metric map[string]interface{}) (string, string) {
	switch nestedString(metric, "type") {
	case "Resource":
		resource := nestedMap(metric, "resource")
		return "resource=" + nestedString(resource, "name"), formatHPAMetricValue(nestedMap(resource, "target"))
	case "ContainerResource":
		resource := nestedMap(metric, "containerResource")
		label := "containerResource=" + nestedString(resource, "name")
		if container := nestedString(resource, "container"); container != "" {
			label += " container=" + container
		}
		return label, formatHPAMetricValue(nestedMap(resource, "target"))
	case "Pods":
		pods := nestedMap(metric, "pods")
		return "pods=" + nestedString(nestedMap(pods, "metric"), "name"), formatHPAMetricValue(nestedMap(pods, "target"))
	case "Object":
		objectMetric := nestedMap(metric, "object")
		label := "object=" + nestedString(nestedMap(objectMetric, "metric"), "name")
		if described := formatHPAObjectReference(nestedMap(objectMetric, "describedObject")); described != "" {
			label += " on=" + described
		}
		return label, formatHPAMetricValue(nestedMap(objectMetric, "target"))
	case "External":
		external := nestedMap(metric, "external")
		return "external=" + nestedString(nestedMap(external, "metric"), "name"), formatHPAMetricValue(nestedMap(external, "target"))
	default:
		metricType := nestedString(metric, "type")
		if metricType == "" {
			return "", ""
		}
		return "type=" + metricType, ""
	}
}

func formatHPACurrentMetric(metric map[string]interface{}, current map[string]interface{}) string {
	if len(current) == 0 {
		return ""
	}
	switch nestedString(metric, "type") {
	case "Resource":
		return formatHPAMetricValue(nestedMap(nestedMap(current, "resource"), "current"))
	case "ContainerResource":
		return formatHPAMetricValue(nestedMap(nestedMap(current, "containerResource"), "current"))
	case "Pods":
		return formatHPAMetricValue(nestedMap(nestedMap(current, "pods"), "current"))
	case "Object":
		return formatHPAMetricValue(nestedMap(nestedMap(current, "object"), "current"))
	case "External":
		return formatHPAMetricValue(nestedMap(nestedMap(current, "external"), "current"))
	default:
		return ""
	}
}

func formatHPAMetricValue(value map[string]interface{}) string {
	if len(value) == 0 {
		return ""
	}
	parts := make([]string, 0, 4)
	if metricType := nestedString(value, "type"); metricType != "" {
		parts = append(parts, strings.ToLower(metricType))
	}
	if averageUtilization := nestedScalarString(value, "averageUtilization"); averageUtilization != "" {
		parts = append(parts, "avgUtil="+averageUtilization+"%")
	}
	if averageValue := nestedScalarString(value, "averageValue"); averageValue != "" {
		parts = append(parts, "avgValue="+averageValue)
	}
	if metricValue := nestedScalarString(value, "value"); metricValue != "" {
		parts = append(parts, "value="+metricValue)
	}
	return strings.Join(parts, " ")
}

func formatHPAObjectReference(object map[string]interface{}) string {
	if len(object) == 0 {
		return ""
	}
	parts := []string{kindNameString(nestedString(object, "kind"), nestedString(object, "name"))}
	if apiVersion := nestedString(object, "apiVersion"); apiVersion != "" {
		parts = append(parts, "api="+apiVersion)
	}
	return strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "  "))
}

func renderHPABehaviorSection(object map[string]interface{}) string {
	behavior := nestedMap(object, "spec", "behavior")
	if len(behavior) == 0 {
		return ""
	}
	lines := make([]string, 0, 8)
	for _, direction := range []string{"scaleUp", "scaleDown"} {
		rule := nestedMap(behavior, direction)
		if len(rule) == 0 {
			continue
		}
		parts := []string{"- " + direction}
		if stabilize := nestedScalarString(rule, "stabilizationWindowSeconds"); stabilize != "" {
			parts = append(parts, "stabilize="+stabilize+"s")
		}
		if selectPolicy := nestedString(rule, "selectPolicy"); selectPolicy != "" {
			parts = append(parts, "select="+selectPolicy)
		}
		lines = append(lines, strings.Join(parts, "  "))
		policies, found, _ := unstructured.NestedSlice(rule, "policies")
		if !found {
			continue
		}
		for _, item := range policies {
			policy, _ := item.(map[string]interface{})
			lines = append(lines, fmt.Sprintf("  policy=%s %s/%ss", nestedString(policy, "type"), nestedScalarString(policy, "value"), nestedScalarString(policy, "periodSeconds")))
		}
	}
	return renderDetailSection("Behavior", lines)
}

func renderPVCBindingSourceSection(claim corev1.PersistentVolumeClaim) string {
	lines := make([]string, 0, 5)
	if claim.Spec.VolumeName != "" {
		lines = append(lines, "- Bound volume: "+claim.Spec.VolumeName)
	}
	if selector := metav1.FormatLabelSelector(claim.Spec.Selector); selector != "" {
		lines = append(lines, "- Selector: "+selector)
	}
	if dataSource := formatTypedLocalObjectReference(claim.Spec.DataSource); dataSource != "" {
		lines = append(lines, "- Data source: "+dataSource)
	}
	if dataSourceRef := formatTypedObjectReference(claim.Spec.DataSourceRef); dataSourceRef != "" {
		lines = append(lines, "- Data source ref: "+dataSourceRef)
	}
	return renderDetailSection("Binding/source", lines)
}

func renderPVCConditionSection(conditions []corev1.PersistentVolumeClaimCondition) string {
	if len(conditions) == 0 {
		return ""
	}
	lines := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		line := fmt.Sprintf("- %s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			line += " reason=" + condition.Reason
		}
		if condition.Message != "" {
			line += " msg=" + condition.Message
		}
		if !condition.LastTransitionTime.IsZero() {
			line += " changed=" + condition.LastTransitionTime.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Conditions", lines)
}

func persistentVolumeClaimFromObject(object map[string]interface{}) (corev1.PersistentVolumeClaim, bool) {
	var claim corev1.PersistentVolumeClaim
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object, &claim); err != nil {
		return corev1.PersistentVolumeClaim{}, false
	}
	return claim, true
}

func formatPersistentVolumeAccessModes(modes []corev1.PersistentVolumeAccessMode) string {
	if len(modes) == 0 {
		return ""
	}
	values := make([]string, 0, len(modes))
	for _, mode := range modes {
		values = append(values, string(mode))
	}
	return strings.Join(values, ", ")
}

func formatPersistentVolumeMode(mode *corev1.PersistentVolumeMode) string {
	if mode == nil {
		return ""
	}
	return string(*mode)
}

func resourceListQuantityString(values corev1.ResourceList, resourceName corev1.ResourceName) string {
	if len(values) == 0 {
		return ""
	}
	quantity, ok := values[resourceName]
	if !ok {
		return ""
	}
	return quantity.String()
}

func formatTypedLocalObjectReference(reference *corev1.TypedLocalObjectReference) string {
	if reference == nil {
		return ""
	}
	parts := []string{kindNameString(reference.Kind, reference.Name)}
	if reference.APIGroup != nil && *reference.APIGroup != "" {
		parts = append(parts, "group="+*reference.APIGroup)
	}
	return strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "  "))
}

func formatTypedObjectReference(reference *corev1.TypedObjectReference) string {
	if reference == nil {
		return ""
	}
	parts := []string{kindNameString(reference.Kind, reference.Name)}
	if reference.APIGroup != nil && *reference.APIGroup != "" {
		parts = append(parts, "group="+*reference.APIGroup)
	}
	if reference.Namespace != nil && *reference.Namespace != "" {
		parts = append(parts, "ns="+*reference.Namespace)
	}
	return strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "  "))
}

func labelSelectorStringFromObject(object map[string]interface{}, fields ...string) string {
	value := nestedMap(object, fields...)
	if len(value) == 0 {
		return ""
	}
	var selector metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(value, &selector); err == nil {
		return metav1.FormatLabelSelector(&selector)
	}
	path := append(append([]string{}, fields...), "matchLabels")
	return formatObjectMapInline(nestedStringMap(object, path...))
}

func kindNameString(kind string, name string) string {
	switch {
	case kind != "" && name != "":
		return kind + "/" + name
	case name != "":
		return name
	default:
		return kind
	}
}

func filterEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func podSpecFromObject(object map[string]interface{}, fields ...string) corev1.PodSpec {
	value, found, _ := unstructured.NestedMap(object, fields...)
	if !found {
		return corev1.PodSpec{}
	}
	var spec corev1.PodSpec
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(value, &spec)
	return spec
}

func nestedMap(object map[string]interface{}, fields ...string) map[string]interface{} {
	value, found, _ := unstructured.NestedMap(object, fields...)
	if !found {
		return nil
	}
	return value
}

func nestedBool(object map[string]interface{}, fields ...string) bool {
	value, found, _ := unstructured.NestedBool(object, fields...)
	if !found {
		return false
	}
	return value
}

func nestedStringMap(object map[string]interface{}, fields ...string) map[string]string {
	value, found, _ := unstructured.NestedStringMap(object, fields...)
	if !found {
		return nil
	}
	return value
}

func nestedString(object map[string]interface{}, fields ...string) string {
	value, _, _ := unstructured.NestedString(object, fields...)
	return value
}

func nestedScalarString(object map[string]interface{}, fields ...string) string {
	value, found, _ := unstructured.NestedFieldNoCopy(object, fields...)
	if !found || value == nil {
		return ""
	}
	return scalarString(value)
}

func scalarString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%g", typed)
	case bool:
		return boolString(typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func nestedInt64(object map[string]interface{}, fields ...string) int64 {
	value, found, _ := unstructured.NestedInt64(object, fields...)
	if found {
		return value
	}
	valueFloat, foundFloat, _ := unstructured.NestedFloat64(object, fields...)
	if foundFloat {
		return int64(valueFloat)
	}
	return 0
}

func nestedIntString(object map[string]interface{}, fields ...string) string {
	value, found, _ := unstructured.NestedInt64(object, fields...)
	if found {
		return fmt.Sprintf("%d", value)
	}
	valueFloat, foundFloat, _ := unstructured.NestedFloat64(object, fields...)
	if foundFloat {
		return fmt.Sprintf("%d", int64(valueFloat))
	}
	return ""
}

func formatObjectMapInline(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ", ")
}

func renderDetailFieldSection(title string, fields []detailField) string {
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%-16s %s", field.Label+":", value))
	}
	return renderDetailCardContent(theme.HeaderStyle.Render(title)+"\n"+strings.Join(lines, "\n"), true)
}

func renderDetailFieldSectionSized(title string, fields []detailField, width int, standout bool) string {
	return renderDetailLayoutCard(detailLayoutCard{Title: title, Fields: fields, Standout: standout, Span: detailLayoutSpanFull}, width)
}

func renderDetailLinesSectionSized(title string, lines []string, width int, standout bool) string {
	return renderDetailLayoutCard(detailLayoutCard{Title: title, Lines: lines, Standout: standout, Span: detailLayoutSpanFull}, width)
}

func renderDetailSection(title string, lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return renderDetailCardContent(theme.HeaderStyle.Render(title)+"\n"+strings.Join(lines, "\n"), false)
}

func renderDetailCardContent(content string, standout bool) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	style := detailCardStyle
	if standout {
		style = detailSummaryCardStyle
	}
	return style.Render(content)
}

func renderDetailInsetLayoutCard(card detailLayoutCard, width int) string {
	style := detailInsetCardStyle.Copy()
	innerWidth := 0
	if width > 0 {
		if style.GetHorizontalFrameSize() > 0 {
			innerWidth = max(1, width-style.GetHorizontalFrameSize())
			style = style.Width(innerWidth)
		} else {
			innerWidth = max(1, width-style.GetHorizontalPadding())
			style = style.Width(innerWidth)
		}
	}
	content := renderDetailInsetLayoutCardContent(card, innerWidth)
	return style.Render(content)
}

func renderDetailInsetLayoutCardContent(card detailLayoutCard, width int) string {
	parts := make([]string, 0, 3)
	if len(card.Fields) != 0 {
		parts = append(parts, renderDetailInsetFieldGrid(card.Fields, width))
	}
	if lines := strings.TrimSpace(strings.Join(card.Lines, "\n")); lines != "" {
		parts = append(parts, lines)
	}
	body := strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "\n"))
	if strings.TrimSpace(card.Title) == "" {
		return body
	}
	if body == "" {
		return theme.HeaderStyle.Render(card.Title)
	}
	return strings.TrimSpace(theme.HeaderStyle.Render(card.Title) + "\n" + body)
}

func renderDetailInsetFieldGrid(fields []detailField, width int) string {
	filtered := make([]detailField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		filtered = append(filtered, field)
	}
	if len(filtered) == 0 {
		return ""
	}
	if width < 36 || len(filtered) == 1 {
		return strings.Join(detailFieldLines(filtered), "\n")
	}
	const gap = 2
	columns := 2
	cellWidth := max(14, (width-gap)/columns)
	rows := make([]string, 0, (len(filtered)+columns-1)/columns)
	for start := 0; start < len(filtered); start += columns {
		end := min(len(filtered), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, lipgloss.NewStyle().Width(cellWidth).Render(detailFieldLines(filtered[idx : idx+1])[0]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return strings.Join(rows, "\n")
}

func joinDetailSections(sections ...string) string {
	filtered := make([]string, 0, len(sections))
	for _, section := range sections {
		if strings.TrimSpace(section) == "" {
			continue
		}
		filtered = append(filtered, section)
	}
	return strings.Join(filtered, "\n\n")
}

func renderMetadataSection(labelsMap map[string]string, annotations map[string]string) string {
	lines := make([]string, 0, len(labelsMap)+len(annotations)+2)
	lines = appendDetailGroupLines(lines, "Labels", stringMapLines(labelsMap))
	lines = appendDetailGroupLines(lines, "Annotations", stringMapLines(annotations))
	if len(lines) == 0 {
		return ""
	}
	return renderDetailLayoutCard(detailLayoutCard{Title: "Metadata", Lines: lines, Span: detailLayoutSpanCompact}, 0)
}

func renderStringMapSection(title string, values map[string]string) string {
	card, ok := renderStringMapCard(title, values, detailLayoutSpanCompact)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func renderStringMapCard(title string, values map[string]string, span detailLayoutSpan) (detailLayoutCard, bool) {
	lines := stringMapLines(values)
	if len(lines) == 0 {
		return detailLayoutCard{}, false
	}
	return detailLayoutCard{Title: title, Lines: lines, Span: span}, true
}

func appendDetailGroupLines(dst []string, title string, lines []string) []string {
	if len(lines) == 0 {
		return dst
	}
	if len(dst) != 0 {
		dst = append(dst, "")
	}
	dst = append(dst, title+":")
	return append(dst, lines...)
}

func stringMapLines(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := sortedMapKeys(values)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- %s=%s", key, values[key]))
	}
	return lines
}

func ownerReferenceLines(refs []metav1.OwnerReference) []string {
	if len(refs) == 0 {
		return nil
	}
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		line := "- " + kindNameString(ref.Kind, ref.Name)
		if ref.Controller != nil {
			line += " controller=" + boolString(*ref.Controller)
		}
		if ref.APIVersion != "" {
			line += " api=" + ref.APIVersion
		}
		lines = append(lines, line)
	}
	return lines
}

func stringListLines(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	lines := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		lines = append(lines, "- "+value)
	}
	return lines
}

func renderDeploymentRolloutSection(deployment *appsv1.Deployment) string {
	lines := deploymentRolloutLines(deployment)
	if len(lines) == 0 {
		return ""
	}
	return renderDetailSection("Rollout", lines)
}

func deploymentRolloutLines(deployment *appsv1.Deployment) []string {
	if deployment == nil {
		return nil
	}
	lines := []string{
		fmt.Sprintf("Desired:      %d", deploymentReplicaCount(deployment)),
		fmt.Sprintf("Replicas:     %d", deployment.Status.Replicas),
		fmt.Sprintf("Updated:      %d", deployment.Status.UpdatedReplicas),
		fmt.Sprintf("Ready:        %d", deployment.Status.ReadyReplicas),
		fmt.Sprintf("Available:    %d", deployment.Status.AvailableReplicas),
		fmt.Sprintf("Unavailable:  %d", deployment.Status.UnavailableReplicas),
	}
	if deployment.Status.ObservedGeneration > 0 {
		lines = append(lines, fmt.Sprintf("Observed gen: %d", deployment.Status.ObservedGeneration))
	}
	if deployment.Status.CollisionCount != nil {
		lines = append(lines, fmt.Sprintf("Collisions:   %d", *deployment.Status.CollisionCount))
	}
	for idx := range lines {
		lines[idx] = "- " + lines[idx]
	}
	return lines
}

func renderDeploymentTemplateResourcesSection(containers []corev1.Container) string {
	lines := deploymentTemplateResourceLines(containers)
	if len(lines) == 0 {
		return ""
	}
	return renderDetailSection("Template resources", lines)
}

func deploymentTemplateResourceLines(containers []corev1.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	requestTotals := make(corev1.ResourceList)
	limitTotals := make(corev1.ResourceList)
	lines := make([]string, 0, len(containers)*2+4)
	lines = append(lines, "Requests:")
	for _, container := range containers {
		requests := formatResourceList(container.Resources.Requests)
		if requests == "" {
			requests = "none"
		} else {
			accumulateResourceList(requestTotals, container.Resources.Requests)
		}
		lines = append(lines, fmt.Sprintf("- %s  %s", container.Name, requests))
	}
	if total := formatResourceList(requestTotals); total != "" {
		lines = append(lines, "- total  "+total)
	}
	lines = append(lines, "", "Limits:")
	for _, container := range containers {
		limits := formatResourceList(container.Resources.Limits)
		if limits == "" {
			limits = "none"
		} else {
			accumulateResourceList(limitTotals, container.Resources.Limits)
		}
		lines = append(lines, fmt.Sprintf("- %s  %s", container.Name, limits))
	}
	if total := formatResourceList(limitTotals); total != "" {
		lines = append(lines, "- total  "+total)
	}
	return lines
}

func (a *App) renderDeploymentRuntimeUsageSection() string {
	lines := a.deploymentRuntimeUsageLines()
	if len(lines) == 0 {
		return ""
	}
	return renderDetailSection("Runtime usage", lines)
}

func (a *App) deploymentRuntimeUsageLines() []string {
	if len(a.activeDeploymentPods) == 0 {
		return nil
	}
	if !a.podUsageSnapshotReady() {
		return []string{"- waiting for pod metrics..."}
	}
	cpuUsed := int64(0)
	cpuRef := int64(0)
	memUsed := int64(0)
	memRef := int64(0)
	lines := make([]string, 0, len(a.activeDeploymentPods)+3)
	for _, row := range a.activeDeploymentPods {
		usage := a.podUsageForRow(row)
		cpuUsed += usage.CPUUsedMilli
		cpuRef += podUsageDenominator(usage.CPULimitMilli, usage.CPURequestMilli)
		memUsed += usage.MemoryUsedBytes
		memRef += podUsageDenominator(usage.MemoryLimitBytes, usage.MemoryRequestBytes)
		cpu := "n/a"
		if usage.HasCPUUsage {
			cpu = formatCPU(usage.CPUUsedMilli)
			if ref := podUsageReference(formatCPUReference(usage.CPULimitMilli), formatCPUReference(usage.CPURequestMilli)); ref != "" {
				cpu += "/" + ref
			}
		}
		memory := "n/a"
		if usage.HasMemoryUsage {
			memory = formatBytes(usage.MemoryUsedBytes)
			if ref := podUsageReference(formatBytesReference(usage.MemoryLimitBytes), formatBytesReference(usage.MemoryRequestBytes)); ref != "" {
				memory += "/" + ref
			}
		}
		lines = append(lines, fmt.Sprintf("- %s  cpu=%s  mem=%s", row.Name, cpu, memory))
	}
	if cpuUsed > 0 || memUsed > 0 {
		totalCPU := formatCPU(cpuUsed)
		if cpuRef > 0 {
			totalCPU += "/" + formatCPUReference(cpuRef)
		}
		totalMem := formatBytes(memUsed)
		if memRef > 0 {
			totalMem += "/" + formatBytesReference(memRef)
		}
		lines = append([]string{fmt.Sprintf("- total  cpu=%s  mem=%s", totalCPU, totalMem)}, lines...)
	}
	return lines
}

func (a *App) renderDeploymentTopRow(width int, deployment *appsv1.Deployment) string {
	rolloutLines := deploymentRolloutLines(deployment)
	resourceLines := deploymentTemplateResourceLines(deployment.Spec.Template.Spec.Containers)
	runtimeLines := a.deploymentRuntimeUsageLines()
	conditionLines := deploymentConditionLines(deployment.Status.Conditions)
	if width < 150 {
		return joinDetailSections(
			renderDetailSection("Rollout", rolloutLines),
			renderDetailSection("Template resources", resourceLines),
			renderDetailSection("Runtime usage", runtimeLines),
			renderDetailSection("Conditions", conditionLines),
		)
	}
	const gap = 2
	cellWidth := max(24, (width-3*gap)/4)
	parts := []string{
		renderDetailLinesSectionSized("Rollout", rolloutLines, cellWidth, false),
		renderDetailLinesSectionSized("Template resources", resourceLines, cellWidth, false),
		renderDetailLinesSectionSized("Runtime usage", runtimeLines, cellWidth, false),
		renderDetailLinesSectionSized("Conditions", conditionLines, cellWidth, false),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts[0], strings.Repeat(" ", gap), parts[1], strings.Repeat(" ", gap), parts[2], strings.Repeat(" ", gap), parts[3])
}

func renderDeploymentContainersSection(width int, containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	innerWidth := max(20, width-detailCardStyle.GetHorizontalFrameSize()-detailCardStyle.GetHorizontalPadding())
	const gap = 2
	columns := chooseDenseInsetColumns(innerWidth, gap, 22, 4)
	cellWidth := max(22, (innerWidth-(columns-1)*gap)/columns)
	rows := make([]string, 0, (len(containers)+columns-1)/columns)
	for start := 0; start < len(containers); start += columns {
		end := min(len(containers), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, renderDeploymentContainerCard(containers[idx], cellWidth))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return renderDetailLayoutCard(detailLayoutCard{Title: "Containers", Lines: []string{strings.Join(rows, "\n\n")}, Span: detailLayoutSpanFull}, width)
}

func renderDeploymentContainerCard(container corev1.Container, width int) string {
	requests := formatResourceList(container.Resources.Requests)
	if requests == "" {
		requests = "none"
	}
	limits := formatResourceList(container.Resources.Limits)
	if limits == "" {
		limits = "none"
	}
	lines := []string{container.Image}
	if ports := renderContainerPorts(container); ports != "" {
		lines = append(lines, strings.TrimSpace(strings.TrimPrefix(ports, "ports=")))
	}
	fields := []detailField{{Label: "Requests", Value: requests}, {Label: "Limits", Value: limits}}
	return renderDetailInsetLayoutCard(detailLayoutCard{Title: container.Name, Fields: fields, Lines: lines, Span: detailLayoutSpanCompact}, width)
}

func renderDeploymentMetadataAnnotationsRow(width int, labelsMap map[string]string, annotations map[string]string) string {
	if width < 120 {
		return joinDetailSections(
			renderDetailSection("Metadata", stringMapLines(labelsMap)),
			renderDetailSection("Annotations", stringMapLines(annotations)),
		)
	}
	const gap = 2
	leftWidth := max(24, (width-gap)/3)
	rightWidth := max(32, width-gap-leftWidth)
	left := renderDetailLinesSectionSized("Metadata", stringMapLines(labelsMap), leftWidth, false)
	right := renderDetailLinesSectionSized("Annotations", stringMapLines(annotations), rightWidth, false)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gap), right)
}

func renderDeploymentPodSpecSection(template corev1.PodTemplateSpec) string {
	card, ok := renderYAMLCard("Pod spec", template, detailLayoutSpanFull)
	if !ok {
		return ""
	}
	return renderDetailLayoutCard(card, 0)
}

func accumulateResourceList(dst corev1.ResourceList, src corev1.ResourceList) {
	for key, quantity := range src {
		current := dst[key]
		current.Add(quantity)
		dst[key] = current
	}
}

func renderPodConditionSection(conditions []corev1.PodCondition) string {
	if len(conditions) == 0 {
		return ""
	}
	lines := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		line := fmt.Sprintf("- %s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			line += " reason=" + condition.Reason
		}
		if condition.Message != "" {
			line += " msg=" + condition.Message
		}
		if !condition.LastTransitionTime.IsZero() {
			line += " changed=" + condition.LastTransitionTime.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Conditions", lines)
}

func renderDeploymentConditionSection(conditions []appsv1.DeploymentCondition) string {
	lines := deploymentConditionLines(conditions)
	if len(lines) == 0 {
		return ""
	}
	return renderDetailSection("Conditions", lines)
}

func deploymentConditionLines(conditions []appsv1.DeploymentCondition) []string {
	if len(conditions) == 0 {
		return nil
	}
	lines := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		line := fmt.Sprintf("- %s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			line += " reason=" + condition.Reason
		}
		if condition.Message != "" {
			line += " msg=" + condition.Message
		}
		if !condition.LastUpdateTime.IsZero() {
			line += " updated=" + condition.LastUpdateTime.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return lines
}

func renderNodeConditionSection(conditions []corev1.NodeCondition) string {
	if len(conditions) == 0 {
		return ""
	}
	lines := make([]string, 0, len(conditions))
	for _, condition := range conditions {
		line := fmt.Sprintf("- %s=%s", condition.Type, condition.Status)
		if condition.Reason != "" {
			line += " reason=" + condition.Reason
		}
		if condition.Message != "" {
			line += " msg=" + condition.Message
		}
		if !condition.LastTransitionTime.IsZero() {
			line += " changed=" + condition.LastTransitionTime.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Conditions", lines)
}

func renderPodSchedulingSection(spec corev1.PodSpec) string {
	lines := make([]string, 0, 8)
	if spec.NodeName != "" {
		lines = append(lines, "- Node: "+spec.NodeName)
	}
	if spec.ServiceAccountName != "" {
		lines = append(lines, "- Service account: "+spec.ServiceAccountName)
	}
	if len(spec.NodeSelector) != 0 {
		lines = append(lines, "- Node selector: "+formatMapInline(spec.NodeSelector))
	}
	if len(spec.Tolerations) != 0 {
		for _, toleration := range spec.Tolerations {
			lines = append(lines, "- Toleration: "+formatToleration(toleration))
		}
	}
	if spec.Affinity != nil {
		lines = append(lines, "- Affinity: configured")
	}
	if spec.PriorityClassName != "" {
		lines = append(lines, "- Priority class: "+spec.PriorityClassName)
	}
	if spec.RuntimeClassName != nil && *spec.RuntimeClassName != "" {
		lines = append(lines, "- Runtime class: "+*spec.RuntimeClassName)
	}
	return renderDetailSection("Scheduling", lines)
}

func renderPodTemplateSection(spec corev1.PodSpec, labelsMap map[string]string) string {
	sections := make([]string, 0, 3)
	templateFields := []detailField{
		{Label: "Service account", Value: defaultString(spec.ServiceAccountName, "default")},
		{Label: "Node selector", Value: formatMapInline(spec.NodeSelector)},
		{Label: "Affinity", Value: boolLabel(spec.Affinity != nil, "configured", "none")},
		{Label: "Priority class", Value: spec.PriorityClassName},
		{Label: "Runtime class", Value: derefString(spec.RuntimeClassName)},
	}
	lines := make([]string, 0, 8)
	if len(spec.Tolerations) != 0 {
		tolerationLines := make([]string, 0, len(spec.Tolerations))
		for _, toleration := range spec.Tolerations {
			tolerationLines = append(tolerationLines, "- "+formatToleration(toleration))
		}
		lines = appendDetailGroupLines(lines, "Tolerations", tolerationLines)
	}
	if len(labelsMap) != 0 {
		lines = appendDetailGroupLines(lines, "Template labels", stringMapLines(labelsMap))
	}
	sections = append(sections, renderDetailLayoutCard(detailLayoutCard{Title: "Pod template", Fields: templateFields, Lines: lines, Span: detailLayoutSpanCompact}, 0))
	sections = append(sections, renderPodContainerSection("Containers", spec.Containers))
	sections = append(sections, renderPodContainerSection("Init containers", spec.InitContainers))
	return joinDetailSections(sections...)
}

func renderPodContainerSection(title string, containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
	}
	lines := make([]string, 0, len(containers)*4)
	for _, container := range containers {
		lines = append(lines, "- "+container.Name+"  image="+container.Image)
		if ports := renderContainerPorts(container); ports != "" {
			lines = append(lines, "  "+strings.TrimSpace(ports))
		}
		if requests := formatResourceList(container.Resources.Requests); requests != "" {
			lines = append(lines, "  requests="+requests)
		}
		if limits := formatResourceList(container.Resources.Limits); limits != "" {
			lines = append(lines, "  limits="+limits)
		}
	}
	return renderDetailSection(title, lines)
}

func renderServicePortsSection(ports []corev1.ServicePort) string {
	lines := renderServicePortLines(ports)
	if len(lines) == 0 {
		return ""
	}
	return renderDetailSection("Ports", lines)
}

func renderServicePortLines(ports []corev1.ServicePort) []string {
	if len(ports) == 0 {
		return nil
	}
	lines := make([]string, 0, len(ports))
	for _, port := range ports {
		parts := make([]string, 0, 6)
		if port.Name != "" {
			parts = append(parts, port.Name)
		}
		parts = append(parts, fmt.Sprintf("%d/%s", port.Port, port.Protocol))
		if port.TargetPort.String() != "" {
			parts = append(parts, "target="+port.TargetPort.String())
		}
		if port.NodePort != 0 {
			parts = append(parts, fmt.Sprintf("nodePort=%d", port.NodePort))
		}
		if port.AppProtocol != nil && *port.AppProtocol != "" {
			parts = append(parts, "app="+*port.AppProtocol)
		}
		lines = append(lines, "- "+strings.Join(parts, "  "))
	}
	return lines
}

func renderServiceSummarySection(width int, details state.ServiceDetails, service *corev1.Service) string {
	if service == nil {
		return ""
	}
	card := detailLayoutCard{
		Title: "Service",
		Groups: []detailLayoutGroup{
			{Title: "Overview", Fields: []detailField{{Label: "Name", Value: details.Row.Name}, {Label: "Namespace", Value: details.Row.Namespace}, {Label: "Cluster", Value: details.Row.Cluster}, {Label: "Age", Value: details.Row.Age}}},
			{Title: "Service", Fields: []detailField{{Label: "Type", Value: string(service.Spec.Type)}, {Label: "Cluster IP", Value: serviceClusterIPs(service)}, {Label: "External IPs", Value: serviceExternalEndpoints(service)}}},
			{Title: "Traffic", Fields: []detailField{{Label: "Traffic policy", Value: serviceTrafficPolicy(service)}, {Label: "Session affinity", Value: string(service.Spec.SessionAffinity)}}},
		},
		Standout: true,
		Span:     detailLayoutSpanFull,
	}
	return renderDetailLayoutCard(card, width)
}

func renderServiceAuxiliarySection(width int, service *corev1.Service) string {
	if service == nil {
		return ""
	}
	if width < 150 {
		return joinDetailSections(
			renderDetailSection("Selector", []string{"- " + formatMapInline(service.Spec.Selector)}),
			renderDetailSection("Labels", stringMapLines(service.Labels)),
			renderDetailSection("Annotations", stringMapLines(service.Annotations)),
		)
	}
	const gap = 2
	leftWidth := max(24, (width-2*gap)/4)
	middleWidth := max(24, (width-2*gap)/4)
	rightWidth := max(32, width-2*gap-leftWidth-middleWidth)
	parts := []string{
		renderDetailLayoutCard(detailLayoutCard{Title: "Selector", Lines: []string{"- " + formatMapInline(service.Spec.Selector)}, Span: detailLayoutSpanCompact}, leftWidth),
		renderDetailLayoutCard(detailLayoutCard{Title: "Labels", Lines: stringMapLines(service.Labels), Span: detailLayoutSpanCompact}, middleWidth),
		renderDetailLayoutCard(detailLayoutCard{Title: "Annotations", Lines: stringMapLines(service.Annotations), Span: detailLayoutSpanCompact}, rightWidth),
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts[0], strings.Repeat(" ", gap), parts[1], strings.Repeat(" ", gap), parts[2])
}

func renderServicePortCardsSection(width int, ports []corev1.ServicePort) string {
	if len(ports) == 0 {
		return ""
	}
	innerWidth := max(20, width-detailCardStyle.GetHorizontalFrameSize()-detailCardStyle.GetHorizontalPadding())
	const gap = 2
	columns := chooseDenseInsetColumns(innerWidth, gap, 20, 4)
	cellWidth := max(20, (innerWidth-(columns-1)*gap)/columns)
	rows := make([]string, 0, (len(ports)+columns-1)/columns)
	for start := 0; start < len(ports); start += columns {
		end := min(len(ports), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, renderServicePortCard(ports[idx], cellWidth))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return renderDetailLayoutCard(detailLayoutCard{Title: "Ports", Lines: []string{strings.Join(rows, "\n\n")}, Span: detailLayoutSpanFull}, width)
}

func renderServicePortCard(port corev1.ServicePort, width int) string {
	fields := []detailField{{Label: "Port", Value: fmt.Sprintf("%d/%s", port.Port, port.Protocol)}, {Label: "Target", Value: port.TargetPort.String()}}
	if port.NodePort != 0 {
		fields = append(fields, detailField{Label: "Node", Value: fmt.Sprintf("%d", port.NodePort)})
	}
	if port.AppProtocol != nil && *port.AppProtocol != "" {
		fields = append(fields, detailField{Label: "App", Value: *port.AppProtocol})
	}
	return renderDetailInsetLayoutCard(detailLayoutCard{Title: defaultString(port.Name, "Port"), Fields: fields, Span: detailLayoutSpanCompact}, width)
}

func chooseDenseInsetColumns(width int, gap int, minCellWidth int, maxColumns int) int {
	for columns := maxColumns; columns > 1; columns-- {
		cellWidth := (width - (columns-1)*gap) / columns
		if cellWidth >= minCellWidth {
			return columns
		}
	}
	return 1
}

func renderNodeTaintsSection(taints []corev1.Taint) string {
	if len(taints) == 0 {
		return ""
	}
	lines := make([]string, 0, len(taints))
	for _, taint := range taints {
		line := fmt.Sprintf("- %s=%s:%s", taint.Key, taint.Value, taint.Effect)
		if taint.TimeAdded != nil && !taint.TimeAdded.IsZero() {
			line += " added=" + taint.TimeAdded.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Taints", lines)
}

func (a *App) renderNodeUtilizationSection(width int, node *corev1.Node) string {
	if node == nil {
		return ""
	}
	totals := a.nodePodResourceTotals(node.Name, a.activeNode.Row.Cluster)
	types := make([]string, 0, 3)
	if a.activeNodeUsage.HasCPUUsage || a.activeNodeUsage.CPUAllocatableMilli > 0 || totals.CPURequestMilli != 0 || totals.CPULimitMilli != 0 {
		types = append(types, "cpu")
	}
	if a.activeNodeUsage.HasMemoryUsage || a.activeNodeUsage.MemoryAllocatable > 0 || totals.MemoryRequestBytes != 0 || totals.MemoryLimitBytes != 0 {
		types = append(types, "memory")
	}
	if a.activeNodeUsage.HasGPU || a.activeNodeUsage.GPUAllocatable > 0 || totals.GPURequest != 0 || totals.GPULimit != 0 {
		types = append(types, "gpu")
	}
	if len(types) == 0 {
		return ""
	}
	innerWidth := max(20, width-detailCardStyle.GetHorizontalFrameSize()-detailCardStyle.GetHorizontalPadding())
	const gap = 2
	columns := min(len(types), chooseDenseInsetColumns(innerWidth, gap, 22, 4))
	cellWidth := max(22, (innerWidth-(columns-1)*gap)/columns)
	cards := make([]string, 0, len(types))
	for _, kind := range types {
		switch kind {
		case "cpu":
			cards = append(cards, renderNodeUtilizationCard("CPU", cellWidth, a.activeNodeUsage.CPUUsedMilli, a.activeNodeUsage.CPUAllocatableMilli, totals.CPURequestMilli, totals.CPULimitMilli, formatCPUReference))
		case "memory":
			cards = append(cards, renderNodeUtilizationCard("Memory", cellWidth, a.activeNodeUsage.MemoryUsedBytes, a.activeNodeUsage.MemoryAllocatable, totals.MemoryRequestBytes, totals.MemoryLimitBytes, formatBytesReference))
		case "gpu":
			cards = append(cards, renderNodeUtilizationCard("GPU", cellWidth, a.activeNodeUsage.GPUAllocated, a.activeNodeUsage.GPUAllocatable, totals.GPURequest, totals.GPULimit, formatCountReference))
		}
	}
	rows := make([]string, 0, (len(cards)+columns-1)/columns)
	for start := 0; start < len(cards); start += columns {
		end := min(len(cards), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, lipgloss.NewStyle().Width(cellWidth).Render(cards[idx]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return renderDetailLayoutCard(detailLayoutCard{Title: "Utilization", Lines: []string{strings.Join(rows, "\n\n")}, Span: detailLayoutSpanFull}, width)
}

type nodePodResourceTotals struct {
	CPURequestMilli    int64
	CPULimitMilli      int64
	MemoryRequestBytes int64
	MemoryLimitBytes   int64
	GPURequest         int64
	GPULimit           int64
}

func (a *App) nodePodResourceTotals(nodeName string, clusterName string) nodePodResourceTotals {
	totals := nodePodResourceTotals{}
	if strings.TrimSpace(nodeName) == "" || strings.TrimSpace(clusterName) == "" {
		return totals
	}
	a.store.ForEachPodObject(func(row state.PodRow, pod *corev1.Pod) bool {
		if row.Cluster != clusterName || pod == nil || pod.Spec.NodeName != nodeName {
			return true
		}
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			return true
		}
		cpuRequestMilli, cpuLimitMilli, memoryRequestBytes, memoryLimitBytes, gpuRequest, gpuLimit := podNodeResourceRequestsAndLimits(pod)
		totals.CPURequestMilli += cpuRequestMilli
		totals.CPULimitMilli += cpuLimitMilli
		totals.MemoryRequestBytes += memoryRequestBytes
		totals.MemoryLimitBytes += memoryLimitBytes
		totals.GPURequest += gpuRequest
		totals.GPULimit += gpuLimit
		return true
	})
	return totals
}

func podNodeResourceRequestsAndLimits(pod *corev1.Pod) (int64, int64, int64, int64, int64, int64) {
	if pod == nil {
		return 0, 0, 0, 0, 0, 0
	}
	cpuRequestMilli := int64(0)
	cpuLimitMilli := int64(0)
	memoryRequestBytes := int64(0)
	memoryLimitBytes := int64(0)
	gpuRequest := int64(0)
	gpuLimit := int64(0)
	containers := append([]corev1.Container(nil), pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, container := range containers {
		cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
		cpuLimit := container.Resources.Limits[corev1.ResourceCPU]
		memoryRequest := container.Resources.Requests[corev1.ResourceMemory]
		memoryLimit := container.Resources.Limits[corev1.ResourceMemory]
		cpuRequestMilli += cpuRequest.MilliValue()
		cpuLimitMilli += cpuLimit.MilliValue()
		memoryRequestBytes += memoryRequest.Value()
		memoryLimitBytes += memoryLimit.Value()
		gpuRequest += resourceListGPUValue(container.Resources.Requests)
		gpuLimit += resourceListGPUValue(container.Resources.Limits)
	}
	return cpuRequestMilli, cpuLimitMilli, memoryRequestBytes, memoryLimitBytes, gpuRequest, gpuLimit
}

func resourceListGPUValue(resources corev1.ResourceList) int64 {
	total := int64(0)
	for name, quantity := range resources {
		if !isGPUResourceNameLocal(name) {
			continue
		}
		total += quantity.Value()
	}
	return total
}

func isGPUResourceNameLocal(name corev1.ResourceName) bool {
	return strings.Contains(strings.ToLower(string(name)), "gpu")
}

func renderNodeUtilizationCard(title string, width int, usage int64, allocatable int64, requests int64, limits int64, formatter func(int64) string) string {
	if allocatable <= 0 && usage <= 0 && requests <= 0 && limits <= 0 {
		return ""
	}
	usageValue := formatNodeUtilizationValue(usage, allocatable, formatter)
	requestValue := formatNodeUtilizationValue(requests, allocatable, formatter)
	limitValue := formatNodeUtilizationValue(limits, allocatable, formatter)
	fields := []detailField{{Label: "Usage", Value: usageValue}, {Label: "Requests", Value: requestValue}, {Label: "Limits", Value: limitValue}, {Label: "Allocatable", Value: formatter(allocatable)}}
	return renderDetailInsetLayoutCard(detailLayoutCard{Title: title, Fields: fields, Span: detailLayoutSpanCompact}, max(22, width))
}

func formatNodeUtilizationValue(value int64, allocatable int64, formatter func(int64) string) string {
	if value <= 0 {
		if allocatable > 0 {
			return "0 / " + formatter(allocatable)
		}
		return "0"
	}
	if allocatable > 0 {
		return formatter(value) + " / " + formatter(allocatable)
	}
	return formatter(value)
}

func renderAssociatedPodSection(title string, rows []state.PodRow) string {
	if len(rows) == 0 {
		return ""
	}
	limit := len(rows)
	if limit > detailAssociatedPodLimit {
		limit = detailAssociatedPodLimit
	}
	lines := make([]string, 0, limit+1)
	for _, row := range rows[:limit] {
		line := fmt.Sprintf("- %s  %s  ready=%s", row.Name, row.Status, row.Ready)
		if row.Node != "" {
			line += "  node=" + row.Node
		}
		if row.Age != "" {
			line += "  age=" + row.Age
		}
		lines = append(lines, line)
	}
	if len(rows) > limit {
		lines = append(lines, fmt.Sprintf("- … %d more", len(rows)-limit))
	}
	return renderDetailSection(title, lines)
}

func formatResourceList(values corev1.ResourceList) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		quantity := values[corev1.ResourceName(key)]
		parts = append(parts, key+"="+quantity.String())
	}
	return strings.Join(parts, "  ")
}

func formatMapInline(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := sortedMapKeys(values)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	return strings.Join(parts, ", ")
}

func deploymentReplicaCount(deployment *appsv1.Deployment) int32 {
	if deployment == nil || deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}

func deploymentStrategyString(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
	}
	strategy := string(deployment.Spec.Strategy.Type)
	if deployment.Spec.Strategy.RollingUpdate == nil {
		return strategy
	}
	return fmt.Sprintf("%s  maxUnavailable=%s  maxSurge=%s", strategy, deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.String(), deployment.Spec.Strategy.RollingUpdate.MaxSurge.String())
}

func serviceClusterIPs(service *corev1.Service) string {
	if service == nil {
		return ""
	}
	values := make([]string, 0, 1+len(service.Spec.ClusterIPs))
	for _, value := range service.Spec.ClusterIPs {
		if value == "" || value == corev1.ClusterIPNone {
			continue
		}
		values = append(values, value)
	}
	if len(values) == 0 && service.Spec.ClusterIP != "" && service.Spec.ClusterIP != corev1.ClusterIPNone {
		values = append(values, service.Spec.ClusterIP)
	}
	if len(values) == 0 {
		return service.Spec.ClusterIP
	}
	return strings.Join(values, ", ")
}

func serviceExternalEndpoints(service *corev1.Service) string {
	if service == nil {
		return ""
	}
	values := append([]string(nil), service.Spec.ExternalIPs...)
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			values = append(values, ingress.IP)
		}
		if ingress.Hostname != "" {
			values = append(values, ingress.Hostname)
		}
	}
	return strings.Join(values, ", ")
}

func serviceTrafficPolicy(service *corev1.Service) string {
	if service == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if service.Spec.ExternalTrafficPolicy != "" {
		parts = append(parts, "external="+string(service.Spec.ExternalTrafficPolicy))
	}
	if service.Spec.InternalTrafficPolicy != nil {
		parts = append(parts, "internal="+string(*service.Spec.InternalTrafficPolicy))
	}
	return strings.Join(parts, "  ")
}

func formatToleration(toleration corev1.Toleration) string {
	parts := make([]string, 0, 5)
	if toleration.Key != "" {
		parts = append(parts, toleration.Key)
	}
	if toleration.Operator != "" {
		parts = append(parts, string(toleration.Operator))
	}
	if toleration.Value != "" {
		parts = append(parts, toleration.Value)
	}
	if toleration.Effect != "" {
		parts = append(parts, string(toleration.Effect))
	}
	if toleration.TolerationSeconds != nil {
		parts = append(parts, fmt.Sprintf("%ds", *toleration.TolerationSeconds))
	}
	return strings.Join(parts, " ")
}

func formatMetaTime(value *metav1.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Time.Format(time.RFC3339)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func boolLabel(value bool, yes string, no string) string {
	if value {
		return yes
	}
	return no
}
