package cluster

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodResourceRequestsAndLimitsSumsGPUAndResources(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "api",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("250m"),
					corev1.ResourceMemory:           resource.MustParse("128Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:                    resource.MustParse("500m"),
					corev1.ResourceMemory:                 resource.MustParse("256Mi"),
					corev1.ResourceEphemeralStorage:       resource.MustParse("2Gi"),
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
				},
			},
		}}},
	}
	cpuReq, cpuLim, memReq, memLim, ephReq, ephLim, gpu := podResourceRequestsAndLimits(pod)
	if cpuReq != 250 || cpuLim != 500 {
		t.Fatalf("cpu = %d/%d, want 250/500", cpuReq, cpuLim)
	}
	if memReq == 0 || memLim == 0 || ephReq == 0 || ephLim == 0 {
		t.Fatalf("expected non-zero memory/ephemeral quantities")
	}
	if gpu != 2 {
		t.Fatalf("gpu = %d, want 2", gpu)
	}
}

func TestNodeGPUCapacityCountsAllocatableGPUs(t *testing.T) {
	node := &corev1.Node{Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("8"),
	}}}
	if got, want := nodeGPUCapacity(node), int64(8); got != want {
		t.Fatalf("gpu capacity = %d, want %d", got, want)
	}
}

func TestNodeEphemeralUsageFromSummary(t *testing.T) {
	used := uint64(123456)
	summary := summaryStats{}
	summary.Node.Fs = &struct {
		UsedBytes *uint64 `json:"usedBytes"`
	}{UsedBytes: &used}
	if got, ok := nodeEphemeralUsageFromSummary(summary); !ok || got != int64(used) {
		t.Fatalf("ephemeral usage = %d,%t want %d,true", got, ok, used)
	}

	if got, ok := nodeEphemeralUsageFromSummary(summaryStats{}); ok || got != 0 {
		t.Fatalf("empty summary = %d,%t want 0,false", got, ok)
	}
}
