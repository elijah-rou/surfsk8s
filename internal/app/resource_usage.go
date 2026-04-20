package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

const resourceUsageRefreshInterval = 15 * time.Second

func (a *App) refreshPodUsageSnapshot(now time.Time, storeVersion uint64, visibleRows int) {
	if a.podUsageListLoading {
		return
	}
	if a.podUsageListVersion == storeVersion && !a.podUsageListFetchedAt.IsZero() && now.Sub(a.podUsageListFetchedAt) < resourceUsageRefreshInterval {
		return
	}
	a.podUsageListLoading = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	usages := a.manager.ListPodResourceUsages(ctx, a.contextScope, a.namespace)
	cancel()
	a.podUsageByKey = usages
	a.podUsageListVersion = storeVersion
	if visibleRows > 0 && len(usages) == 0 {
		a.podUsageListFetchedAt = time.Time{}
	} else {
		a.podUsageListFetchedAt = now
	}
	a.podUsageListLoading = false
}

func (a *App) refreshNodeUsageSnapshot(now time.Time, storeVersion uint64, visibleRows int) {
	if a.nodeUsageListLoading {
		return
	}
	if a.nodeUsageListVersion == storeVersion && !a.nodeUsageListFetchedAt.IsZero() && now.Sub(a.nodeUsageListFetchedAt) < resourceUsageRefreshInterval {
		return
	}
	a.nodeUsageListLoading = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	usages := a.manager.ListNodeResourceUsages(ctx, a.contextScope)
	cancel()
	a.nodeUsageByKey = usages
	a.nodeUsageListVersion = storeVersion
	if visibleRows > 0 && len(usages) == 0 {
		a.nodeUsageListFetchedAt = time.Time{}
	} else {
		a.nodeUsageListFetchedAt = now
	}
	a.nodeUsageListLoading = false
}

func (a *App) maybeRefreshResourceUsageCmd(now time.Time) tea.Cmd {
	switch a.screen {
	case screenPodDetails:
		if a.activePod.Pod == nil || a.podUsageLoading {
			return nil
		}
		if usage, ok := a.podUsageByKey[a.activePod.Row.Key.String()]; ok {
			a.activePodUsage = usage
		}
		if !a.podUsageFetchedAt.IsZero() && now.Sub(a.podUsageFetchedAt) < resourceUsageRefreshInterval {
			return nil
		}
		a.podUsageLoading = true
		details := a.activePod
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			return podUsageResultMsg{key: details.Row.Key, usage: a.manager.PodResourceUsage(ctx, details)}
		}
	case screenResourceDetails:
		if a.activeResource.Resource != "nodes" || a.activeResource.APIGroup != "" || a.activeNode.Node == nil || a.nodeUsageLoading {
			return nil
		}
		if usage, ok := a.nodeUsageByKey[a.activeNode.Row.Key.String()]; ok {
			a.activeNodeUsage = usage
		}
		if !a.nodeUsageFetchedAt.IsZero() && now.Sub(a.nodeUsageFetchedAt) < resourceUsageRefreshInterval {
			return nil
		}
		a.nodeUsageLoading = true
		details := a.activeNode
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			return nodeUsageResultMsg{key: details.Row.Key, usage: a.manager.NodeResourceUsage(ctx, details)}
		}
	default:
		return nil
	}
}

func (a *App) podUsageScopeKey() string {
	return a.contextScope + "|" + a.namespace
}

func (a *App) nodeUsageScopeKey() string {
	return a.contextScope
}

func (a *App) podUsageForRow(row state.PodRow) cluster.PodResourceUsage {
	if usage, ok := a.podUsageByKey[row.Key.String()]; ok {
		return usage
	}
	return cluster.PodResourceUsage{Key: row.Key}
}

func (a *App) nodeUsageForRow(row state.NodeRow) cluster.NodeResourceUsage {
	if usage, ok := a.nodeUsageByKey[row.Key.String()]; ok {
		return usage
	}
	return cluster.NodeResourceUsage{Key: row.Key}
}

func (a *App) podCells(row state.PodRow) []string {
	usage := a.podUsageForRow(row)
	return []string{row.Cluster, row.Namespace, row.Name, row.Ready, row.Status, formatPodTableCPU(usage), formatPodTableMemory(usage), formatPodTableEphemeral(usage), formatPodTableGPU(usage), fmt.Sprintf("%d", row.Restarts), row.Age, row.Node}
}

func (a *App) nodeCells(row state.NodeRow) []string {
	usage := a.nodeUsageForRow(row)
	return []string{row.Cluster, row.Name, row.Status, formatNodeTableCPU(usage), formatNodeTableMemory(usage), formatNodeTableEphemeral(usage), formatNodeTableGPU(usage), row.Roles, row.Version, row.Age}
}

func (a *App) podTableRows(rows []state.PodRow, now time.Time) [][]string {
	result := make([][]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, a.podCells(row.WithAge(now)))
	}
	return result
}

func (a *App) nodeTableRows(rows []state.NodeRow, now time.Time) [][]string {
	result := make([][]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, a.nodeCells(row.WithAge(now)))
	}
	return result
}

func renderPodUsageSection(usage cluster.PodResourceUsage) string {
	lines := make([]string, 0, 6)
	if usage.HasCPUUsage || usage.CPURequestMilli > 0 || usage.CPULimitMilli > 0 {
		lines = append(lines, renderUsageLine("CPU", formatCPU(usage.CPUUsedMilli), podUsageReference(formatCPUReference(usage.CPULimitMilli), formatCPUReference(usage.CPURequestMilli)), barRatio(float64(usage.CPUUsedMilli), float64(podUsageDenominator(usage.CPULimitMilli, usage.CPURequestMilli)))))
	}
	if usage.HasMemoryUsage || usage.MemoryRequestBytes > 0 || usage.MemoryLimitBytes > 0 {
		lines = append(lines, renderUsageLine("Memory", formatBytes(usage.MemoryUsedBytes), podUsageReference(formatBytesReference(usage.MemoryLimitBytes), formatBytesReference(usage.MemoryRequestBytes)), barRatio(float64(usage.MemoryUsedBytes), float64(podUsageDenominator(usage.MemoryLimitBytes, usage.MemoryRequestBytes)))))
	}
	if usage.HasEphemeralUsage || usage.EphemeralRequestBytes > 0 || usage.EphemeralLimitBytes > 0 {
		lines = append(lines, renderUsageLine("Ephemeral", formatBytes(usage.EphemeralUsedBytes), podUsageReference(formatBytesReference(usage.EphemeralLimitBytes), formatBytesReference(usage.EphemeralRequestBytes)), barRatio(float64(usage.EphemeralUsedBytes), float64(podUsageDenominator(usage.EphemeralLimitBytes, usage.EphemeralRequestBytes)))))
	}
	if usage.HasGPU {
		lines = append(lines, renderUsageLine("GPU", fmt.Sprintf("%d allocated", usage.GPUAllocated), "", -1))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Resource usage:\n" + strings.Join(lines, "\n")
}

func renderNodeUsageSection(usage cluster.NodeResourceUsage) string {
	lines := make([]string, 0, 4)
	if usage.HasCPUUsage || usage.CPUAllocatableMilli > 0 {
		lines = append(lines, renderUsageLine("CPU", formatCPU(usage.CPUUsedMilli), formatCPUReference(usage.CPUAllocatableMilli), barRatio(float64(usage.CPUUsedMilli), float64(usage.CPUAllocatableMilli))))
	}
	if usage.HasMemoryUsage || usage.MemoryAllocatable > 0 {
		lines = append(lines, renderUsageLine("Memory", formatBytes(usage.MemoryUsedBytes), formatBytesReference(usage.MemoryAllocatable), barRatio(float64(usage.MemoryUsedBytes), float64(usage.MemoryAllocatable))))
	}
	if usage.HasEphemeralUsage || usage.EphemeralAllocatable > 0 {
		lines = append(lines, renderUsageLine("Ephemeral", formatBytes(usage.EphemeralUsedBytes), formatBytesReference(usage.EphemeralAllocatable), barRatio(float64(usage.EphemeralUsedBytes), float64(usage.EphemeralAllocatable))))
	}
	if usage.HasGPU {
		lines = append(lines, renderUsageLine("GPU", fmt.Sprintf("%d", usage.GPUAllocated), formatCountReference(usage.GPUAllocatable), barRatio(float64(usage.GPUAllocated), float64(usage.GPUAllocatable))))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Resource usage:\n" + strings.Join(lines, "\n")
}

func renderUsageLine(label string, used string, reference string, ratio float64) string {
	base := fmt.Sprintf("- %-10s %s", label, used)
	if strings.TrimSpace(reference) != "" {
		base += " / " + reference
	}
	bar := renderUsageBar(ratio, 16)
	if bar != "" {
		base += "  " + bar
	}
	return base
}

func renderUsageBar(ratio float64, width int) string {
	if width <= 0 || ratio < 0 {
		return ""
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled == 0 && ratio > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	empty := width - filled
	style := theme.StatusOK
	switch {
	case ratio >= 0.9:
		style = theme.StatusError
	case ratio >= 0.7:
		style = theme.StatusWarn
	}
	parts := make([]string, 0, 2)
	if filled > 0 {
		parts = append(parts, style.Render(strings.Repeat("█", filled)))
	}
	if empty > 0 {
		parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("█", empty)))
	}
	return strings.Join(parts, "")
}

func podUsageReference(limit string, request string) string {
	if limit != "" {
		return limit
	}
	return request
}

func podUsageDenominator(limit int64, request int64) int64 {
	if limit > 0 {
		return limit
	}
	return request
}

func barRatio(used float64, total float64) float64 {
	if total <= 0 || used < 0 {
		return -1
	}
	return used / total
}

func formatCPU(milli int64) string {
	if milli == 0 {
		return "0m"
	}
	if milli%1000 == 0 {
		return fmt.Sprintf("%d", milli/1000)
	}
	return fmt.Sprintf("%dm", milli)
}

func formatCPUReference(milli int64) string {
	if milli <= 0 {
		return ""
	}
	return formatCPU(milli)
}

func formatCountReference(value int64) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", value)
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0B"
	}
	units := []string{"B", "Ki", "Mi", "Gi", "Ti"}
	value := float64(bytes)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if value >= 10 || unit == 0 {
		return fmt.Sprintf("%.0f%s", value, units[unit])
	}
	return fmt.Sprintf("%.1f%s", value, units[unit])
}

func formatBytesReference(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	return formatBytes(bytes)
}

func formatPodTableCPU(usage cluster.PodResourceUsage) string {
	if !usage.HasCPUUsage {
		return ""
	}
	return formatCPU(usage.CPUUsedMilli)
}

func formatPodTableMemory(usage cluster.PodResourceUsage) string {
	if !usage.HasMemoryUsage {
		return ""
	}
	return formatBytes(usage.MemoryUsedBytes)
}

func formatPodTableEphemeral(usage cluster.PodResourceUsage) string {
	if !usage.HasEphemeralUsage {
		return ""
	}
	return formatBytes(usage.EphemeralUsedBytes)
}

func formatPodTableGPU(usage cluster.PodResourceUsage) string {
	if !usage.HasGPU {
		return ""
	}
	return fmt.Sprintf("%d", usage.GPUAllocated)
}

func formatNodeTableCPU(usage cluster.NodeResourceUsage) string {
	if !usage.HasCPUUsage {
		return ""
	}
	return usedWithTotal(formatCPU(usage.CPUUsedMilli), formatCPUReference(usage.CPUAllocatableMilli))
}

func formatNodeTableMemory(usage cluster.NodeResourceUsage) string {
	if !usage.HasMemoryUsage {
		return ""
	}
	return usedWithTotal(formatBytes(usage.MemoryUsedBytes), formatBytesReference(usage.MemoryAllocatable))
}

func formatNodeTableEphemeral(usage cluster.NodeResourceUsage) string {
	if !usage.HasEphemeralUsage {
		return ""
	}
	return usedWithTotal(formatBytes(usage.EphemeralUsedBytes), formatBytesReference(usage.EphemeralAllocatable))
}

func formatNodeTableGPU(usage cluster.NodeResourceUsage) string {
	if !usage.HasGPU {
		return ""
	}
	return fmt.Sprintf("%d/%d", usage.GPUAllocated, usage.GPUAllocatable)
}

func usedWithTotal(used string, total string) string {
	if strings.TrimSpace(total) == "" {
		return used
	}
	return used + "/" + total
}
