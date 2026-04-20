package cluster

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/elijahrou/surfsk8s/internal/state"
)

type PodResourceUsage struct {
	Key state.PodKey

	CPUUsedMilli          int64
	CPURequestMilli       int64
	CPULimitMilli         int64
	HasCPUUsage           bool
	MemoryUsedBytes       int64
	MemoryRequestBytes    int64
	MemoryLimitBytes      int64
	HasMemoryUsage        bool
	EphemeralUsedBytes    int64
	EphemeralRequestBytes int64
	EphemeralLimitBytes   int64
	HasEphemeralUsage     bool
	GPUAllocated          int64
	HasGPU                bool
}

type NodeResourceUsage struct {
	Key state.NodeKey

	CPUUsedMilli         int64
	CPUAllocatableMilli  int64
	HasCPUUsage          bool
	MemoryUsedBytes      int64
	MemoryAllocatable    int64
	HasMemoryUsage       bool
	EphemeralUsedBytes   int64
	EphemeralAllocatable int64
	HasEphemeralUsage    bool
	GPUAllocated         int64
	GPUAllocatable       int64
	HasGPU               bool
}

type podMetricSample struct {
	CPUUsedMilli    int64
	MemoryUsedBytes int64
}

type nodeMetricSample struct {
	CPUUsedMilli    int64
	MemoryUsedBytes int64
}

func (m *Manager) PodResourceUsage(ctx context.Context, details state.PodDetails) PodResourceUsage {
	if ctx == nil {
		panic("cluster.Manager.PodResourceUsage: nil context")
	}
	usage := PodResourceUsage{Key: details.Row.Key}
	pod := details.Pod
	if pod == nil {
		return usage
	}

	usage.CPURequestMilli, usage.CPULimitMilli, usage.MemoryRequestBytes, usage.MemoryLimitBytes, usage.EphemeralRequestBytes, usage.EphemeralLimitBytes, usage.GPUAllocated = podResourceRequestsAndLimits(pod)
	usage.HasGPU = usage.GPUAllocated > 0

	conn := m.connectionForCluster(details.Row.Cluster)
	if conn == nil {
		return usage
	}

	if cpuMilli, memoryBytes, ok := fetchPodMetrics(ctx, conn, details.Row.Namespace, details.Row.Name); ok {
		usage.CPUUsedMilli = cpuMilli
		usage.MemoryUsedBytes = memoryBytes
		usage.HasCPUUsage = true
		usage.HasMemoryUsage = true
	}
	if pod.Spec.NodeName != "" {
		if usedBytes, ok := fetchPodEphemeralUsage(ctx, conn, pod); ok {
			usage.EphemeralUsedBytes = usedBytes
			usage.HasEphemeralUsage = true
		}
	}
	return usage
}

func (m *Manager) NodeResourceUsage(ctx context.Context, details state.NodeDetails) NodeResourceUsage {
	if ctx == nil {
		panic("cluster.Manager.NodeResourceUsage: nil context")
	}
	usage := NodeResourceUsage{Key: details.Row.Key}
	node := details.Node
	if node == nil {
		return usage
	}

	usage.CPUAllocatableMilli = quantityMilliValue(node.Status.Allocatable[corev1.ResourceCPU])
	usage.MemoryAllocatable = quantityByteValue(node.Status.Allocatable[corev1.ResourceMemory])
	usage.EphemeralAllocatable = quantityByteValue(node.Status.Allocatable[corev1.ResourceEphemeralStorage])
	usage.GPUAllocatable = nodeGPUCapacity(node)
	usage.GPUAllocated = m.nodeGPUAllocated(node.Name, details.Row.Cluster)
	usage.HasGPU = usage.GPUAllocatable > 0 || usage.GPUAllocated > 0

	conn := m.connectionForCluster(details.Row.Cluster)
	if conn == nil {
		return usage
	}

	if cpuMilli, memoryBytes, ok := fetchNodeMetrics(ctx, conn, details.Row.Name); ok {
		usage.CPUUsedMilli = cpuMilli
		usage.MemoryUsedBytes = memoryBytes
		usage.HasCPUUsage = true
		usage.HasMemoryUsage = true
	}
	if usedBytes, ok := fetchNodeEphemeralUsage(ctx, conn, details.Row.Name); ok {
		usage.EphemeralUsedBytes = usedBytes
		usage.HasEphemeralUsage = true
	}
	return usage
}

func (m *Manager) ListPodResourceUsages(ctx context.Context, clusterScope string, namespace string) map[string]PodResourceUsage {
	if ctx == nil {
		panic("cluster.Manager.ListPodResourceUsages: nil context")
	}
	now := time.Now()
	result := make(map[string]PodResourceUsage, 256)
	podsByCluster := make(map[string][]state.PodDetails, 8)
	m.store.ForEachPod(func(row state.PodRow) bool {
		if clusterScope != "" && row.Cluster != clusterScope {
			return true
		}
		if namespace != "" && row.Namespace != namespace {
			return true
		}
		details, ok := m.store.PodDetailsByKey(row.Key, now)
		if !ok {
			return true
		}
		usage := PodResourceUsage{Key: row.Key}
		if details.Pod != nil {
			usage.CPURequestMilli, usage.CPULimitMilli, usage.MemoryRequestBytes, usage.MemoryLimitBytes, usage.EphemeralRequestBytes, usage.EphemeralLimitBytes, usage.GPUAllocated = podResourceRequestsAndLimits(details.Pod)
			usage.HasGPU = usage.GPUAllocated > 0
		}
		result[row.Key.String()] = usage
		podsByCluster[row.Cluster] = append(podsByCluster[row.Cluster], details)
		return true
	})
	for clusterName, pods := range podsByCluster {
		conn := m.connectionForCluster(clusterName)
		if conn == nil {
			continue
		}
		metrics := fetchPodMetricsList(ctx, conn)
		for _, details := range pods {
			usage := result[details.Row.Key.String()]
			if sample, ok := metrics[details.Row.Key.String()]; ok {
				usage.CPUUsedMilli = sample.CPUUsedMilli
				usage.MemoryUsedBytes = sample.MemoryUsedBytes
				usage.HasCPUUsage = true
				usage.HasMemoryUsage = true
			}
			result[details.Row.Key.String()] = usage
		}
	}
	return result
}

func (m *Manager) ListNodeResourceUsages(ctx context.Context, clusterScope string) map[string]NodeResourceUsage {
	if ctx == nil {
		panic("cluster.Manager.ListNodeResourceUsages: nil context")
	}
	result := make(map[string]NodeResourceUsage, 64)
	nodesByCluster := make(map[string][]state.NodeDetails, 8)
	now := time.Now()
	gpuByNode := make(map[string]int64, 64)
	m.store.ForEachPod(func(row state.PodRow) bool {
		if clusterScope != "" && row.Cluster != clusterScope {
			return true
		}
		details, ok := m.store.PodDetailsByKey(row.Key, now)
		if !ok || details.Pod == nil || details.Pod.Spec.NodeName == "" {
			return true
		}
		if details.Pod.DeletionTimestamp != nil || details.Pod.Status.Phase == corev1.PodSucceeded || details.Pod.Status.Phase == corev1.PodFailed {
			return true
		}
		_, _, _, _, _, _, gpu := podResourceRequestsAndLimits(details.Pod)
		gpuByNode[row.Cluster+"/"+details.Pod.Spec.NodeName] += gpu
		return true
	})
	m.store.ForEachNode(func(row state.NodeRow) bool {
		if clusterScope != "" && row.Cluster != clusterScope {
			return true
		}
		details, ok := m.store.NodeDetailsByKey(row.Key, now)
		if !ok {
			return true
		}
		usage := NodeResourceUsage{Key: row.Key}
		if details.Node != nil {
			usage.CPUAllocatableMilli = quantityMilliValue(details.Node.Status.Allocatable[corev1.ResourceCPU])
			usage.MemoryAllocatable = quantityByteValue(details.Node.Status.Allocatable[corev1.ResourceMemory])
			usage.EphemeralAllocatable = quantityByteValue(details.Node.Status.Allocatable[corev1.ResourceEphemeralStorage])
			usage.GPUAllocatable = nodeGPUCapacity(details.Node)
		}
		usage.GPUAllocated = gpuByNode[row.Cluster+"/"+row.Name]
		usage.HasGPU = usage.GPUAllocatable > 0 || usage.GPUAllocated > 0
		result[row.Key.String()] = usage
		nodesByCluster[row.Cluster] = append(nodesByCluster[row.Cluster], details)
		return true
	})
	for clusterName, nodes := range nodesByCluster {
		conn := m.connectionForCluster(clusterName)
		if conn == nil {
			continue
		}
		metrics := fetchNodeMetricsList(ctx, conn)
		for _, details := range nodes {
			usage := result[details.Row.Key.String()]
			if sample, ok := metrics[details.Row.Name]; ok {
				usage.CPUUsedMilli = sample.CPUUsedMilli
				usage.MemoryUsedBytes = sample.MemoryUsedBytes
				usage.HasCPUUsage = true
				usage.HasMemoryUsage = true
			}
			if usedBytes, ok := fetchNodeEphemeralUsage(ctx, conn, details.Row.Name); ok {
				usage.EphemeralUsedBytes = usedBytes
				usage.HasEphemeralUsage = true
			}
			result[details.Row.Key.String()] = usage
		}
	}
	return result
}

func (m *Manager) connectionForCluster(clusterName string) *ClusterConn {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conns[clusterName]
}

func (m *Manager) nodeGPUAllocated(nodeName string, clusterName string) int64 {
	now := time.Now()
	total := int64(0)
	m.store.ForEachPod(func(row state.PodRow) bool {
		if row.Cluster != clusterName || row.Node != nodeName {
			return true
		}
		details, ok := m.store.PodDetailsByKey(row.Key, now)
		if !ok || details.Pod == nil {
			return true
		}
		if details.Pod.DeletionTimestamp != nil || details.Pod.Status.Phase == corev1.PodSucceeded || details.Pod.Status.Phase == corev1.PodFailed {
			return true
		}
		_, _, _, _, _, _, gpu := podResourceRequestsAndLimits(details.Pod)
		total += gpu
		return true
	})
	return total
}

func fetchPodMetrics(ctx context.Context, conn *ClusterConn, namespace string, name string) (int64, int64, bool) {
	object, err := conn.Dynamic.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || object == nil {
		return 0, 0, false
	}
	containers, found, err := unstructured.NestedSlice(object.Object, "containers")
	if err != nil || !found {
		return 0, 0, false
	}
	cpuMilli := int64(0)
	memoryBytes := int64(0)
	for _, rawContainer := range containers {
		container, ok := rawContainer.(map[string]interface{})
		if !ok {
			continue
		}
		usage, ok := container["usage"].(map[string]interface{})
		if !ok {
			continue
		}
		if cpuValue, ok := usage["cpu"].(string); ok {
			quantity, err := resource.ParseQuantity(cpuValue)
			if err == nil {
				cpuMilli += quantity.MilliValue()
			}
		}
		if memoryValue, ok := usage["memory"].(string); ok {
			quantity, err := resource.ParseQuantity(memoryValue)
			if err == nil {
				memoryBytes += quantity.Value()
			}
		}
	}
	return cpuMilli, memoryBytes, true
}

func fetchNodeMetrics(ctx context.Context, conn *ClusterConn, name string) (int64, int64, bool) {
	object, err := conn.Dynamic.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}).Get(ctx, name, metav1.GetOptions{})
	if err != nil || object == nil {
		return 0, 0, false
	}
	usage, found, err := unstructured.NestedStringMap(object.Object, "usage")
	if err != nil || !found {
		return 0, 0, false
	}
	cpuMilli := int64(0)
	memoryBytes := int64(0)
	if cpuValue, ok := usage["cpu"]; ok {
		quantity, err := resource.ParseQuantity(cpuValue)
		if err == nil {
			cpuMilli = quantity.MilliValue()
		}
	}
	if memoryValue, ok := usage["memory"]; ok {
		quantity, err := resource.ParseQuantity(memoryValue)
		if err == nil {
			memoryBytes = quantity.Value()
		}
	}
	return cpuMilli, memoryBytes, true
}

func fetchPodMetricsList(ctx context.Context, conn *ClusterConn) map[string]podMetricSample {
	result := make(map[string]podMetricSample, 256)
	list, err := conn.Dynamic.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil || list == nil {
		return result
	}
	for idx := range list.Items {
		object := &list.Items[idx]
		containers, found, err := unstructured.NestedSlice(object.Object, "containers")
		if err != nil || !found {
			continue
		}
		sample := podMetricSample{}
		for _, rawContainer := range containers {
			container, ok := rawContainer.(map[string]interface{})
			if !ok {
				continue
			}
			usage, ok := container["usage"].(map[string]interface{})
			if !ok {
				continue
			}
			if cpuValue, ok := usage["cpu"].(string); ok {
				quantity, err := resource.ParseQuantity(cpuValue)
				if err == nil {
					sample.CPUUsedMilli += quantity.MilliValue()
				}
			}
			if memoryValue, ok := usage["memory"].(string); ok {
				quantity, err := resource.ParseQuantity(memoryValue)
				if err == nil {
					sample.MemoryUsedBytes += quantity.Value()
				}
			}
		}
		key := state.PodKey{Cluster: conn.Name, Namespace: object.GetNamespace(), Name: object.GetName()}.String()
		result[key] = sample
	}
	return result
}

func fetchNodeMetricsList(ctx context.Context, conn *ClusterConn) map[string]nodeMetricSample {
	result := make(map[string]nodeMetricSample, 64)
	list, err := conn.Dynamic.Resource(schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}).List(ctx, metav1.ListOptions{})
	if err != nil || list == nil {
		return result
	}
	for idx := range list.Items {
		object := &list.Items[idx]
		usage, found, err := unstructured.NestedStringMap(object.Object, "usage")
		if err != nil || !found {
			continue
		}
		sample := nodeMetricSample{}
		if cpuValue, ok := usage["cpu"]; ok {
			quantity, err := resource.ParseQuantity(cpuValue)
			if err == nil {
				sample.CPUUsedMilli = quantity.MilliValue()
			}
		}
		if memoryValue, ok := usage["memory"]; ok {
			quantity, err := resource.ParseQuantity(memoryValue)
			if err == nil {
				sample.MemoryUsedBytes = quantity.Value()
			}
		}
		result[object.GetName()] = sample
	}
	return result
}

type summaryStats struct {
	Node struct {
		Fs *struct {
			UsedBytes *uint64 `json:"usedBytes"`
		} `json:"fs"`
	} `json:"node"`
	Pods []struct {
		PodRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			UID       string `json:"uid"`
		} `json:"podRef"`
		EphemeralStorage *struct {
			UsedBytes *uint64 `json:"usedBytes"`
		} `json:"ephemeral-storage"`
		Containers []struct {
			Rootfs *struct {
				UsedBytes *uint64 `json:"usedBytes"`
			} `json:"rootfs"`
			Logs *struct {
				UsedBytes *uint64 `json:"usedBytes"`
			} `json:"logs"`
		} `json:"containers"`
	} `json:"pods"`
}

func fetchPodEphemeralUsage(ctx context.Context, conn *ClusterConn, pod *corev1.Pod) (int64, bool) {
	summary, ok := fetchNodeSummary(ctx, conn, pod.Spec.NodeName)
	if !ok {
		return 0, false
	}
	return podEphemeralUsageFromSummary(summary, pod)
}

func podEphemeralUsageFromSummary(summary summaryStats, pod *corev1.Pod) (int64, bool) {
	if pod == nil {
		return 0, false
	}
	for _, item := range summary.Pods {
		if item.PodRef.Namespace != pod.Namespace || item.PodRef.Name != pod.Name {
			continue
		}
		if item.PodRef.UID != "" && string(pod.UID) != "" && item.PodRef.UID != string(pod.UID) {
			continue
		}
		if item.EphemeralStorage != nil && item.EphemeralStorage.UsedBytes != nil {
			return int64(*item.EphemeralStorage.UsedBytes), true
		}
		used := int64(0)
		found := false
		for _, container := range item.Containers {
			if container.Rootfs != nil && container.Rootfs.UsedBytes != nil {
				used += int64(*container.Rootfs.UsedBytes)
				found = true
			}
			if container.Logs != nil && container.Logs.UsedBytes != nil {
				used += int64(*container.Logs.UsedBytes)
				found = true
			}
		}
		return used, found
	}
	return 0, false
}

func fetchNodeEphemeralUsage(ctx context.Context, conn *ClusterConn, nodeName string) (int64, bool) {
	summary, ok := fetchNodeSummary(ctx, conn, nodeName)
	if !ok {
		return 0, false
	}
	return nodeEphemeralUsageFromSummary(summary)
}

func nodeEphemeralUsageFromSummary(summary summaryStats) (int64, bool) {
	if summary.Node.Fs == nil || summary.Node.Fs.UsedBytes == nil {
		return 0, false
	}
	return int64(*summary.Node.Fs.UsedBytes), true
}

func fetchNodeSummary(ctx context.Context, conn *ClusterConn, nodeName string) (summaryStats, bool) {
	if conn == nil || conn.Clientset == nil || strings.TrimSpace(nodeName) == "" {
		return summaryStats{}, false
	}
	raw, err := conn.Clientset.CoreV1().RESTClient().Get().Resource("nodes").Name(nodeName).SubResource("proxy").Suffix("stats", "summary").Do(ctx).Raw()
	if err != nil {
		return summaryStats{}, false
	}
	var summary summaryStats
	if err := json.Unmarshal(raw, &summary); err != nil {
		return summaryStats{}, false
	}
	return summary, true
}

func podResourceRequestsAndLimits(pod *corev1.Pod) (int64, int64, int64, int64, int64, int64, int64) {
	if pod == nil {
		return 0, 0, 0, 0, 0, 0, 0
	}
	cpuRequestMilli := int64(0)
	cpuLimitMilli := int64(0)
	memoryRequestBytes := int64(0)
	memoryLimitBytes := int64(0)
	ephemeralRequestBytes := int64(0)
	ephemeralLimitBytes := int64(0)
	gpuAllocated := int64(0)
	containers := append([]corev1.Container(nil), pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, container := range containers {
		cpuRequestMilli += quantityMilliValue(container.Resources.Requests[corev1.ResourceCPU])
		cpuLimitMilli += quantityMilliValue(container.Resources.Limits[corev1.ResourceCPU])
		memoryRequestBytes += quantityByteValue(container.Resources.Requests[corev1.ResourceMemory])
		memoryLimitBytes += quantityByteValue(container.Resources.Limits[corev1.ResourceMemory])
		ephemeralRequestBytes += quantityByteValue(container.Resources.Requests[corev1.ResourceEphemeralStorage])
		ephemeralLimitBytes += quantityByteValue(container.Resources.Limits[corev1.ResourceEphemeralStorage])
		gpuAllocated += containerGPUAllocation(container)
	}
	return cpuRequestMilli, cpuLimitMilli, memoryRequestBytes, memoryLimitBytes, ephemeralRequestBytes, ephemeralLimitBytes, gpuAllocated
}

func containerGPUAllocation(container corev1.Container) int64 {
	allocated := int64(0)
	seen := make(map[string]struct{}, len(container.Resources.Limits)+len(container.Resources.Requests))
	for name, quantity := range container.Resources.Limits {
		if !isGPUResourceName(name) {
			continue
		}
		seen[string(name)] = struct{}{}
		allocated += quantity.Value()
	}
	for name, quantity := range container.Resources.Requests {
		if !isGPUResourceName(name) {
			continue
		}
		if _, ok := seen[string(name)]; ok {
			continue
		}
		allocated += quantity.Value()
	}
	return allocated
}

func nodeGPUCapacity(node *corev1.Node) int64 {
	if node == nil {
		return 0
	}
	total := int64(0)
	for name, quantity := range node.Status.Allocatable {
		if !isGPUResourceName(name) {
			continue
		}
		total += quantity.Value()
	}
	return total
}

func isGPUResourceName(name corev1.ResourceName) bool {
	value := strings.ToLower(string(name))
	return strings.Contains(value, "gpu")
}

func quantityMilliValue(q resource.Quantity) int64 {
	return q.MilliValue()
}

func quantityByteValue(q resource.Quantity) int64 {
	return q.Value()
}
