package app

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

var usageANSIPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripUsageANSI(value string) string {
	return usageANSIPattern.ReplaceAllString(value, "")
}

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

func TestFormatUsageBarCellPutsBarFirstAndFractionAfter(t *testing.T) {
	rendered := stripUsageANSI(formatUsageBarCell("120m", "500m", 0.24, 0.5))
	if !strings.Contains(rendered, "120m/500m") {
		t.Fatalf("missing usage fraction: %q", rendered)
	}
	if strings.Index(rendered, "120m/500m") < 8 {
		t.Fatalf("expected fixed-width bar prefix before fraction: %q", rendered)
	}
	if !strings.Contains(rendered, "│") {
		t.Fatalf("missing request marker: %q", rendered)
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

func TestUsageCellsShowLoadingUntilSnapshotReady(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.podUsageByKey = make(map[string]cluster.PodResourceUsage)
	app.nodeUsageByKey = make(map[string]cluster.NodeResourceUsage)
	podRow := state.PodRow{Key: state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}}
	nodeRow := state.NodeRow{Key: state.NodeKey{Cluster: "dev", Name: "node-a"}}

	if got := app.podCPUCell(podRow); got != "loading…" {
		t.Fatalf("pod loading cell = %q, want loading placeholder", got)
	}
	if got := app.nodeEphemeralCell(nodeRow); got != "loading…" {
		t.Fatalf("node loading cell = %q, want loading placeholder", got)
	}

	app.podUsageListScopeKey = app.podUsageScopeKey()
	app.podUsageListFetchedAt = time.Now()
	app.podUsageByKey[podRow.Key.String()] = cluster.PodResourceUsage{Key: podRow.Key, CPUUsedMilli: 120, CPULimitMilli: 500, HasCPUUsage: true}
	if got := stripUsageANSI(app.podCPUCell(podRow)); !strings.Contains(got, "120m/") {
		t.Fatalf("pod ready cell = %q, want rendered usage", got)
	}

	app.nodeUsageListScopeKey = app.nodeUsageScopeKey()
	app.nodeUsageListFetchedAt = time.Now()
	app.nodeUsageByKey[nodeRow.Key.String()] = cluster.NodeResourceUsage{Key: nodeRow.Key, EphemeralUsedBytes: 50 * 1024 * 1024 * 1024, EphemeralAllocatable: 100 * 1024 * 1024 * 1024, HasEphemeralUsage: true}
	if got := stripUsageANSI(app.nodeEphemeralCell(nodeRow)); !strings.Contains(got, "50") {
		t.Fatalf("node ready cell = %q, want rendered usage", got)
	}
}
