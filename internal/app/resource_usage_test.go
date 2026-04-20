package app

import (
	"strings"
	"testing"

	"github.com/elijahrou/surfsk8s/internal/cluster"
)

func TestRenderPodUsageSectionIncludesGPUAndEphemeral(t *testing.T) {
	rendered := renderPodUsageSection(cluster.PodResourceUsage{
		CPUUsedMilli:        120,
		CPURequestMilli:     500,
		HasCPUUsage:         true,
		MemoryUsedBytes:     200 * 1024 * 1024,
		MemoryLimitBytes:    512 * 1024 * 1024,
		HasMemoryUsage:      true,
		EphemeralUsedBytes:  64 * 1024 * 1024,
		EphemeralLimitBytes: 2 * 1024 * 1024 * 1024,
		HasEphemeralUsage:   true,
		GPUAllocated:        1,
		HasGPU:              true,
	})
	for _, fragment := range []string{"Resource usage:", "CPU", "Memory", "Ephemeral", "GPU", "allocated"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestRenderNodeUsageSectionIncludesAllocatedGPU(t *testing.T) {
	rendered := renderNodeUsageSection(cluster.NodeResourceUsage{
		CPUUsedMilli:         1200,
		CPUAllocatableMilli:  4000,
		HasCPUUsage:          true,
		MemoryUsedBytes:      8 * 1024 * 1024 * 1024,
		MemoryAllocatable:    16 * 1024 * 1024 * 1024,
		HasMemoryUsage:       true,
		EphemeralUsedBytes:   50 * 1024 * 1024 * 1024,
		EphemeralAllocatable: 100 * 1024 * 1024 * 1024,
		HasEphemeralUsage:    true,
		GPUAllocated:         3,
		GPUAllocatable:       8,
		HasGPU:               true,
	})
	for _, fragment := range []string{"CPU", "Memory", "Ephemeral", "GPU", "3 / 8"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}
