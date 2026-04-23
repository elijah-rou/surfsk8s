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

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

const detailAssociatedPodLimit = 24

var (
	detailCardStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	detailSummaryCardStyle = detailCardStyle.Copy().BorderForeground(lipgloss.Color("14")).Bold(true)
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
	sections := make([]string, 0, 12)
	sections = append(sections, renderDetailFieldSection("Deployment", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Ready", Value: details.Row.Ready},
		{Label: "Selector", Value: metav1.FormatLabelSelector(deployment.Spec.Selector)},
		{Label: "Strategy", Value: deploymentStrategyString(deployment)},
	}))
	sections = append(sections, renderDeploymentRolloutSection(deployment))
	sections = append(sections, renderDeploymentConditionSection(deployment.Status.Conditions))
	sections = append(sections, renderDeploymentTemplateResourcesSection(deployment.Spec.Template.Spec.Containers))
	sections = append(sections, a.renderDeploymentRuntimeUsageSection())
	sections = append(sections, renderPodTemplateSection(deployment.Spec.Template.Spec, deployment.Spec.Template.Labels))
	sections = append(sections, renderMetadataSection(deployment.Labels, deployment.Annotations))
	return joinDetailSections(sections...)
}

func (a *App) renderServiceDetails() string {
	details := a.activeService
	service := details.Service
	if service == nil {
		return "service disappeared"
	}
	sections := make([]string, 0, 8)
	sections = append(sections, renderDetailFieldSection("Service", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Type", Value: string(service.Spec.Type)},
		{Label: "Selector", Value: formatMapInline(service.Spec.Selector)},
		{Label: "Cluster IP", Value: serviceClusterIPs(service)},
		{Label: "External IPs", Value: serviceExternalEndpoints(service)},
		{Label: "Traffic policy", Value: serviceTrafficPolicy(service)},
		{Label: "Session affinity", Value: string(service.Spec.SessionAffinity)},
	}))
	sections = append(sections, renderServicePortsSection(service.Spec.Ports))
	sections = append(sections, renderMetadataSection(service.Labels, service.Annotations))
	return joinDetailSections(sections...)
}

func (a *App) renderNodeDetails() string {
	details := a.activeNode
	node := details.Node
	if node == nil {
		return "node disappeared"
	}
	info := node.Status.NodeInfo
	sections := make([]string, 0, 10)
	sections = append(sections, renderDetailFieldSection("Node", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Status", Value: details.Row.Status},
		{Label: "Roles", Value: details.Row.Roles},
		{Label: "Unschedulable", Value: boolString(node.Spec.Unschedulable)},
		{Label: "OS image", Value: info.OSImage},
		{Label: "Kernel", Value: info.KernelVersion},
		{Label: "Kubelet", Value: info.KubeletVersion},
		{Label: "Kube-proxy", Value: info.KubeProxyVersion},
		{Label: "Container runtime", Value: info.ContainerRuntimeVersion},
		{Label: "Architecture", Value: info.Architecture},
		{Label: "Provider ID", Value: node.Spec.ProviderID},
		{Label: "Pod CIDR", Value: strings.Join(node.Spec.PodCIDRs, ", ")},
		{Label: "Hostname", Value: nodeAddress(node, corev1.NodeHostName)},
		{Label: "Internal IP", Value: nodeAddress(node, corev1.NodeInternalIP)},
		{Label: "External IP", Value: nodeAddress(node, corev1.NodeExternalIP)},
	}))
	if usageSection := renderNodeUsageSectionWithLabel(a.activeNodeUsage, formatUsageDetailLabel(time.Now(), a.nodeUsageFetchedAt, a.nodeUsageLoading)); usageSection != "" {
		sections = append(sections, renderDetailCardContent(usageSection, false))
	}
	sections = append(sections, renderNodeConditionSection(node.Status.Conditions))
	sections = append(sections, renderNodeTaintsSection(node.Spec.Taints))
	sections = append(sections, renderNodeCapacitySection(node.Status.Capacity, node.Status.Allocatable))
	sections = append(sections, renderMetadataSection(node.Labels, node.Annotations))
	return joinDetailSections(sections...)
}

func renderPodDetails(details state.PodDetails, usage cluster.PodResourceUsage, usageLabel string) string {
	pod := details.Pod
	if pod == nil {
		return "pod disappeared"
	}
	sections := make([]string, 0, 10)
	sections = append(sections, renderDetailFieldSection("Pod", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Phase", Value: string(pod.Status.Phase)},
		{Label: "Ready", Value: details.Row.Ready},
		{Label: "Restarts", Value: fmt.Sprintf("%d", details.Row.Restarts)},
		{Label: "Started", Value: formatMetaTime(pod.Status.StartTime)},
		{Label: "Pod IP", Value: pod.Status.PodIP},
		{Label: "Host IP", Value: pod.Status.HostIP},
		{Label: "Node", Value: pod.Spec.NodeName},
		{Label: "Service account", Value: defaultString(pod.Spec.ServiceAccountName, "default")},
		{Label: "QoS", Value: string(pod.Status.QOSClass)},
		{Label: "Priority class", Value: pod.Spec.PriorityClassName},
		{Label: "Runtime class", Value: derefString(pod.Spec.RuntimeClassName)},
	}))
	if usageSection := renderPodUsageSectionWithLabel(usage, usageLabel); usageSection != "" {
		sections = append(sections, renderDetailCardContent(usageSection, false))
	}
	sections = append(sections, renderPodConditionSection(pod.Status.Conditions))
	sections = append(sections, renderPodSchedulingSection(pod.Spec))
	sections = append(sections, renderPodContainerSection("Init containers", pod.Spec.InitContainers))
	sections = append(sections, renderPodContainerSection("Containers", pod.Spec.Containers))
	sections = append(sections, renderMetadataSection(pod.Labels, pod.Annotations))
	return joinDetailSections(sections...)
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
	switch {
	case a.activeResource.Resource == "daemonsets" && a.activeResource.APIGroup == "apps":
		return renderTypedDaemonSetDetails(a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "statefulsets" && a.activeResource.APIGroup == "apps":
		return renderTypedStatefulSetDetails(a.activeResource, a.activeGenericDetails), true
	case a.activeResource.Resource == "jobs" && a.activeResource.APIGroup == "batch":
		return renderTypedJobDetails(a.activeResource, a.activeGenericDetails), true
	default:
		return "", false
	}
}

func renderTypedDaemonSetDetails(resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	sections := make([]string, 0, 8)
	sections = append(sections, renderDetailFieldSection("DaemonSet", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")},
		{Label: "Selector", Value: formatObjectMapInline(nestedStringMap(obj.Object, "spec", "selector", "matchLabels"))},
		{Label: "Update strategy", Value: nestedString(obj.Object, "spec", "updateStrategy", "type")},
	}))
	sections = append(sections, renderDetailSection("Rollout", []string{
		fmt.Sprintf("- Desired:      %d", nestedInt64(obj.Object, "status", "desiredNumberScheduled")),
		fmt.Sprintf("- Current:      %d", nestedInt64(obj.Object, "status", "currentNumberScheduled")),
		fmt.Sprintf("- Updated:      %d", nestedInt64(obj.Object, "status", "updatedNumberScheduled")),
		fmt.Sprintf("- Ready:        %d", nestedInt64(obj.Object, "status", "numberReady")),
		fmt.Sprintf("- Available:    %d", nestedInt64(obj.Object, "status", "numberAvailable")),
		fmt.Sprintf("- Unavailable:  %d", nestedInt64(obj.Object, "status", "numberUnavailable")),
	}))
	sections = append(sections, renderGenericConditionsSection(obj.Object))
	sections = append(sections, renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")))
	sections = append(sections, renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()))
	sections = append(sections, renderGenericResourceContextSection(resource, details))
	return joinDetailSections(sections...)
}

func renderTypedStatefulSetDetails(resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	sections := make([]string, 0, 8)
	sections = append(sections, renderDetailFieldSection("StatefulSet", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")},
		{Label: "Service", Value: nestedString(obj.Object, "spec", "serviceName")},
		{Label: "Pod policy", Value: nestedString(obj.Object, "spec", "podManagementPolicy")},
		{Label: "Update strategy", Value: nestedString(obj.Object, "spec", "updateStrategy", "type")},
	}))
	sections = append(sections, renderDetailSection("Rollout", []string{
		fmt.Sprintf("- Desired:      %d", nestedInt64(obj.Object, "spec", "replicas")),
		fmt.Sprintf("- Current:      %d", nestedInt64(obj.Object, "status", "currentReplicas")),
		fmt.Sprintf("- Updated:      %d", nestedInt64(obj.Object, "status", "updatedReplicas")),
		fmt.Sprintf("- Ready:        %d", nestedInt64(obj.Object, "status", "readyReplicas")),
		fmt.Sprintf("- Available:    %d", nestedInt64(obj.Object, "status", "availableReplicas")),
	}))
	sections = append(sections, renderGenericConditionsSection(obj.Object))
	sections = append(sections, renderStatefulSetVolumeClaimsSection(obj.Object))
	sections = append(sections, renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")))
	sections = append(sections, renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()))
	sections = append(sections, renderGenericResourceContextSection(resource, details))
	return joinDetailSections(sections...)
}

func renderTypedJobDetails(resource cluster.ResourceKind, details cluster.GenericResourceDetails) string {
	obj := details.Object
	sections := make([]string, 0, 8)
	sections = append(sections, renderDetailFieldSection("Job", []detailField{
		{Label: "Name", Value: details.Row.Name},
		{Label: "Namespace", Value: details.Row.Namespace},
		{Label: "Cluster", Value: details.Row.Cluster},
		{Label: "Age", Value: details.Row.Age},
		{Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")},
		{Label: "Parallelism", Value: nestedIntString(obj.Object, "spec", "parallelism")},
		{Label: "Completions", Value: nestedIntString(obj.Object, "spec", "completions")},
		{Label: "Completion mode", Value: nestedString(obj.Object, "spec", "completionMode")},
		{Label: "Backoff limit", Value: nestedIntString(obj.Object, "spec", "backoffLimit")},
	}))
	sections = append(sections, renderDetailSection("Status", []string{
		fmt.Sprintf("- Active:      %d", nestedInt64(obj.Object, "status", "active")),
		fmt.Sprintf("- Succeeded:   %d", nestedInt64(obj.Object, "status", "succeeded")),
		fmt.Sprintf("- Failed:      %d", nestedInt64(obj.Object, "status", "failed")),
	}))
	sections = append(sections, renderGenericConditionsSection(obj.Object))
	sections = append(sections, renderPodTemplateSection(podSpecFromObject(obj.Object, "spec", "template", "spec"), nestedStringMap(obj.Object, "spec", "template", "metadata", "labels")))
	sections = append(sections, renderMetadataSection(obj.GetLabels(), obj.GetAnnotations()))
	sections = append(sections, renderGenericResourceContextSection(resource, details))
	return joinDetailSections(sections...)
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

func podSpecFromObject(object map[string]interface{}, fields ...string) corev1.PodSpec {
	value, found, _ := unstructured.NestedMap(object, fields...)
	if !found {
		return corev1.PodSpec{}
	}
	var spec corev1.PodSpec
	_ = runtime.DefaultUnstructuredConverter.FromUnstructured(value, &spec)
	return spec
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
	sections := make([]string, 0, 2)
	if section := renderStringMapSection("Labels", labelsMap); section != "" {
		sections = append(sections, section)
	}
	if section := renderStringMapSection("Annotations", annotations); section != "" {
		sections = append(sections, section)
	}
	return joinDetailSections(sections...)
}

func renderStringMapSection(title string, values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := sortedMapKeys(values)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- %s=%s", key, values[key]))
	}
	return renderDetailSection(title, lines)
}

func renderDeploymentRolloutSection(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
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
	return renderDetailSection("Rollout", lines)
}

func renderDeploymentTemplateResourcesSection(containers []corev1.Container) string {
	if len(containers) == 0 {
		return ""
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
	return renderDetailSection("Template resources", lines)
}

func (a *App) renderDeploymentRuntimeUsageSection() string {
	if len(a.activeDeploymentPods) == 0 {
		return ""
	}
	if !a.podUsageSnapshotReady() {
		return renderDetailSection("Runtime usage", []string{"- waiting for pod metrics..."})
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
	return renderDetailSection("Runtime usage", lines)
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
		if !condition.LastUpdateTime.IsZero() {
			line += " updated=" + condition.LastUpdateTime.Time.Format(time.RFC3339)
		}
		lines = append(lines, line)
	}
	return renderDetailSection("Conditions", lines)
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
	sections := make([]string, 0, 4)
	sections = append(sections, renderDetailFieldSection("Pod template", []detailField{
		{Label: "Service account", Value: defaultString(spec.ServiceAccountName, "default")},
		{Label: "Node selector", Value: formatMapInline(spec.NodeSelector)},
		{Label: "Affinity", Value: boolLabel(spec.Affinity != nil, "configured", "none")},
		{Label: "Priority class", Value: spec.PriorityClassName},
		{Label: "Runtime class", Value: derefString(spec.RuntimeClassName)},
	}))
	if len(spec.Tolerations) != 0 {
		lines := make([]string, 0, len(spec.Tolerations))
		for _, toleration := range spec.Tolerations {
			lines = append(lines, "- "+formatToleration(toleration))
		}
		sections = append(sections, renderDetailSection("Tolerations", lines))
	}
	sections = append(sections, renderPodContainerSection("Containers", spec.Containers))
	sections = append(sections, renderPodContainerSection("Init containers", spec.InitContainers))
	if len(labelsMap) != 0 {
		sections = append(sections, renderStringMapSection("Template labels", labelsMap))
	}
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
	if len(ports) == 0 {
		return ""
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
	return renderDetailSection("Ports", lines)
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

func renderNodeCapacitySection(capacity corev1.ResourceList, allocatable corev1.ResourceList) string {
	sections := make([]string, 0, 2)
	if formatted := formatResourceList(capacity); formatted != "" {
		sections = append(sections, renderDetailSection("Capacity", []string{"- " + formatted}))
	}
	if formatted := formatResourceList(allocatable); formatted != "" {
		sections = append(sections, renderDetailSection("Allocatable", []string{"- " + formatted}))
	}
	return joinDetailSections(sections...)
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
