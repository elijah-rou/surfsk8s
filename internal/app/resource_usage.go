package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

const (
	resourceUsageRefreshInterval = 15 * time.Second
	podUsageInitialDelay         = time.Second
)

var (
	usageLoadingCellText     = theme.Muted.Render("loading…")
	usageUnavailableCellText = theme.Muted.Render("n/a")
)

func usageSpinner(now time.Time) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if len(frames) == 0 {
		return ""
	}
	idx := int(now.UnixNano()/int64(120*time.Millisecond)) % len(frames)
	if idx < 0 {
		idx = 0
	}
	return frames[idx]
}

func usageStatusStyle(age time.Duration, loading bool, fetchedAt time.Time) lipgloss.Style {
	if loading && fetchedAt.IsZero() {
		return theme.StatusWarn.Copy()
	}
	if fetchedAt.IsZero() {
		return theme.Muted.Copy()
	}
	switch {
	case age <= 15*time.Second:
		return theme.StatusOK.Copy()
	case age <= 45*time.Second:
		return theme.StatusWarn.Copy()
	default:
		return theme.StatusError.Copy()
	}
}

func formatUsageAge(now time.Time, fetchedAt time.Time, loading bool) string {
	if loading && fetchedAt.IsZero() {
		return usageStatusStyle(0, loading, fetchedAt).Render("usage:loading " + usageSpinner(now))
	}
	if fetchedAt.IsZero() {
		return usageStatusStyle(0, loading, fetchedAt).Render("usage:pending")
	}
	age := now.Sub(fetchedAt)
	if age < 0 {
		age = 0
	}
	label := "usage:" + formatOverviewAge(age)
	if loading {
		label += " " + usageSpinner(now) + " refresh"
	}
	return usageStatusStyle(age, loading, fetchedAt).Render(label)
}

func formatUsageDetailLabel(now time.Time, fetchedAt time.Time, loading bool) string {
	if loading && fetchedAt.IsZero() {
		return usageStatusStyle(0, loading, fetchedAt).Render("loading " + usageSpinner(now))
	}
	if fetchedAt.IsZero() {
		return usageStatusStyle(0, loading, fetchedAt).Render("pending")
	}
	age := now.Sub(fetchedAt)
	if age < 0 {
		age = 0
	}
	label := "updated " + formatOverviewAge(age) + " ago"
	if loading {
		label = "refreshing " + usageSpinner(now) + " · " + label
	}
	return usageStatusStyle(age, loading, fetchedAt).Render(label)
}

func (a *App) listUsageStatus(now time.Time) string {
	switch {
	case a.screen == screenPods:
		return formatUsageAge(now, a.podUsageListFetchedAt, a.podUsageListLoading)
	case a.screen == screenResourceList && a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return formatUsageAge(now, a.nodeUsageListFetchedAt, a.nodeUsageListLoading)
	default:
		return ""
	}
}

func (a *App) maybeRefreshPodUsageListCmd(now time.Time) tea.Cmd {
	if a.screen != screenPods || a.podUsageListLoading {
		return nil
	}
	scopeKey := a.podUsageScopeKey()
	storeVersion := a.store.PodVersion()
	if a.podUsageListScopeKey == scopeKey && a.podUsageListVersion == storeVersion && !a.podUsageListFetchedAt.IsZero() && now.Sub(a.podUsageListFetchedAt) < resourceUsageRefreshInterval {
		return nil
	}
	if a.podUsageListFetchedAt.IsZero() && !a.lastTick.IsZero() && now.Sub(a.lastTick) < podUsageInitialDelay {
		return nil
	}
	a.podUsageListLoading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return podUsageSnapshotMsg{scopeKey: scopeKey, storeVersion: storeVersion, usages: a.manager.ListPodResourceUsages(ctx, a.contextScope, a.namespace)}
	}
}

func (a *App) maybeRefreshNodeUsageListCmd(now time.Time) tea.Cmd {
	if a.screen != screenResourceList || a.activeResource.Resource != "nodes" || a.activeResource.APIGroup != "" || a.nodeUsageListLoading {
		return nil
	}
	scopeKey := a.nodeUsageScopeKey()
	storeVersion := a.store.NodeVersion()
	if a.nodeUsageListScopeKey == scopeKey && a.nodeUsageListVersion == storeVersion && !a.nodeUsageListFetchedAt.IsZero() && now.Sub(a.nodeUsageListFetchedAt) < resourceUsageRefreshInterval {
		return nil
	}
	a.nodeUsageListLoading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return nodeUsageSnapshotMsg{scopeKey: scopeKey, storeVersion: storeVersion, usages: a.manager.ListNodeResourceUsages(ctx, a.contextScope)}
	}
}

func (a *App) maybeRefreshResourceUsageCmd(now time.Time) tea.Cmd {
	switch a.screen {
	case screenPods:
		return a.maybeRefreshPodUsageListCmd(now)
	case screenResourceList:
		return a.maybeRefreshNodeUsageListCmd(now)
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

type podUsageTableCells struct {
	CPU       string
	Memory    string
	Ephemeral string
	GPU       string
}

type nodeUsageTableCells struct {
	CPU       string
	Memory    string
	Ephemeral string
	GPU       string
}

func buildPodUsageTableCellCache(usages map[string]cluster.PodResourceUsage) map[string]podUsageTableCells {
	if len(usages) == 0 {
		return nil
	}
	cache := make(map[string]podUsageTableCells, len(usages))
	for key, usage := range usages {
		cache[key] = podUsageTableCells{
			CPU:       formatPodTableCPU(usage),
			Memory:    formatPodTableMemory(usage),
			Ephemeral: formatPodTableEphemeral(usage),
			GPU:       formatPodTableGPU(usage),
		}
	}
	return cache
}

func buildNodeUsageTableCellCache(usages map[string]cluster.NodeResourceUsage) map[string]nodeUsageTableCells {
	if len(usages) == 0 {
		return nil
	}
	cache := make(map[string]nodeUsageTableCells, len(usages))
	for key, usage := range usages {
		cache[key] = nodeUsageTableCells{
			CPU:       formatNodeTableCPU(usage),
			Memory:    formatNodeTableMemory(usage),
			Ephemeral: formatNodeTableEphemeral(usage),
			GPU:       formatNodeTableGPU(usage),
		}
	}
	return cache
}

func (a *App) podUsageSnapshotReady() bool {
	return a.podUsageListScopeKey == a.podUsageScopeKey() && !a.podUsageListFetchedAt.IsZero()
}

func (a *App) nodeUsageSnapshotReady() bool {
	return a.nodeUsageListScopeKey == a.nodeUsageScopeKey() && !a.nodeUsageListFetchedAt.IsZero()
}

func usageLoadingCell() string {
	return usageLoadingCellText
}

func usageUnavailableCell() string {
	return usageUnavailableCellText
}

func podCPUCellFromUsage(usage cluster.PodResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasCPUUsage {
		return usageUnavailableCell()
	}
	return formatPodTableCPU(usage)
}

func podMemoryCellFromUsage(usage cluster.PodResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasMemoryUsage {
		return usageUnavailableCell()
	}
	return formatPodTableMemory(usage)
}

func podEphemeralCellFromUsage(usage cluster.PodResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasEphemeralUsage {
		return usageUnavailableCell()
	}
	return formatPodTableEphemeral(usage)
}

func nodeCPUCellFromUsage(usage cluster.NodeResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasCPUUsage {
		return usageUnavailableCell()
	}
	return formatNodeTableCPU(usage)
}

func nodeMemoryCellFromUsage(usage cluster.NodeResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasMemoryUsage {
		return usageUnavailableCell()
	}
	return formatNodeTableMemory(usage)
}

func nodeEphemeralCellFromUsage(usage cluster.NodeResourceUsage, ready bool) string {
	if !ready {
		return usageLoadingCell()
	}
	if !usage.HasEphemeralUsage {
		return usageUnavailableCell()
	}
	return formatNodeTableEphemeral(usage)
}

func (a *App) podUsageCells(row state.PodRow) podUsageTableCells {
	if a.podUsageSnapshotReady() {
		if cached, ok := a.podUsageCellByKey[row.Key.String()]; ok {
			return cached
		}
	}
	usage := a.podUsageForRow(row)
	ready := a.podUsageSnapshotReady()
	cells := podUsageTableCells{
		CPU:       podCPUCellFromUsage(usage, ready),
		Memory:    podMemoryCellFromUsage(usage, ready),
		Ephemeral: podEphemeralCellFromUsage(usage, ready),
		GPU:       formatPodTableGPU(usage),
	}
	if ready {
		if a.podUsageCellByKey == nil {
			a.podUsageCellByKey = make(map[string]podUsageTableCells, 64)
		}
		a.podUsageCellByKey[row.Key.String()] = cells
	}
	return cells
}

func (a *App) podCPUCell(row state.PodRow) string {
	return a.podUsageCells(row).CPU
}

func (a *App) podMemoryCell(row state.PodRow) string {
	return a.podUsageCells(row).Memory
}

func (a *App) podEphemeralCell(row state.PodRow) string {
	return a.podUsageCells(row).Ephemeral
}

func (a *App) nodeCPUCell(row state.NodeRow) string {
	return nodeCPUCellFromUsage(a.nodeUsageForRow(row), a.nodeUsageSnapshotReady())
}

func (a *App) nodeMemoryCell(row state.NodeRow) string {
	return nodeMemoryCellFromUsage(a.nodeUsageForRow(row), a.nodeUsageSnapshotReady())
}

func (a *App) nodeEphemeralCell(row state.NodeRow) string {
	return nodeEphemeralCellFromUsage(a.nodeUsageForRow(row), a.nodeUsageSnapshotReady())
}

func (a *App) fillPodCells(dst []string, row state.PodRow) {
	if len(dst) < 12 {
		panic("app.fillPodCells: short dst")
	}
	usageCells := a.podUsageCells(row)
	dst[0] = row.Cluster
	dst[1] = row.Namespace
	dst[2] = row.Name
	dst[3] = row.Ready
	dst[4] = row.Status
	dst[5] = usageCells.CPU
	dst[6] = usageCells.Memory
	dst[7] = usageCells.Ephemeral
	dst[8] = usageCells.GPU
	dst[9] = strconv.Itoa(row.Restarts)
	dst[10] = row.Age
	dst[11] = row.Node
}

func (a *App) podCells(row state.PodRow) []string {
	cells := make([]string, 12)
	a.fillPodCells(cells, row)
	return cells
}

func (a *App) fillNodeCells(dst []string, row state.NodeRow) {
	if len(dst) < 10 {
		panic("app.fillNodeCells: short dst")
	}
	ready := a.nodeUsageSnapshotReady()
	dst[0] = row.Cluster
	dst[1] = row.Name
	dst[2] = row.Status
	if ready {
		if cached, ok := a.nodeUsageCellByKey[row.Key.String()]; ok {
			dst[3] = cached.CPU
			dst[4] = cached.Memory
			dst[5] = cached.Ephemeral
			dst[6] = cached.GPU
		} else {
			usage := a.nodeUsageForRow(row)
			dst[3] = nodeCPUCellFromUsage(usage, true)
			dst[4] = nodeMemoryCellFromUsage(usage, true)
			dst[5] = nodeEphemeralCellFromUsage(usage, true)
			dst[6] = formatNodeTableGPU(usage)
		}
	} else {
		usage := a.nodeUsageForRow(row)
		dst[3] = nodeCPUCellFromUsage(usage, false)
		dst[4] = nodeMemoryCellFromUsage(usage, false)
		dst[5] = nodeEphemeralCellFromUsage(usage, false)
		dst[6] = formatNodeTableGPU(usage)
	}
	dst[7] = row.Roles
	dst[8] = row.Version
	dst[9] = row.Age
}

func (a *App) nodeCells(row state.NodeRow) []string {
	cells := make([]string, 10)
	a.fillNodeCells(cells, row)
	return cells
}

// podCellStringAt returns the string for a single pods table column without building the full cell slice.
// Only the AGE column (index 10) calls WithAge (formats age string).
func (a *App) podCellStringAt(row state.PodRow, columnIndex int, now time.Time) string {
	switch columnIndex {
	case 0:
		return row.Cluster
	case 1:
		return row.Namespace
	case 2:
		return row.Name
	case 3:
		return row.Ready
	case 4:
		return row.Status
	case 5:
		return a.podCPUCell(row)
	case 6:
		return a.podMemoryCell(row)
	case 7:
		return a.podEphemeralCell(row)
	case 8:
		return a.podUsageCells(row).GPU
	case 9:
		return strconv.Itoa(row.Restarts)
	case 10:
		return row.WithAge(now).Age
	case 11:
		return row.Node
	default:
		return ""
	}
}

// nodeCellStringAt mirrors nodeCells for a single column; only AGE (index 9) formats via WithAge.
func (a *App) nodeCellStringAt(row state.NodeRow, columnIndex int, now time.Time) string {
	switch columnIndex {
	case 0:
		return row.Cluster
	case 1:
		return row.Name
	case 2:
		return row.Status
	case 3:
		return a.nodeCPUCell(row)
	case 4:
		return a.nodeMemoryCell(row)
	case 5:
		return a.nodeEphemeralCell(row)
	case 6:
		return formatNodeTableGPU(a.nodeUsageForRow(row))
	case 7:
		return row.Roles
	case 8:
		return row.Version
	case 9:
		return row.WithAge(now).Age
	default:
		return ""
	}
}

func ensureRowCellBuffer(buf *[][]string, rows int, columns int) [][]string {
	if rows < 0 {
		panic("app.ensureRowCellBuffer: negative rows")
	}
	if columns <= 0 {
		panic("app.ensureRowCellBuffer: non-positive columns")
	}
	if cap(*buf) < rows {
		grown := make([][]string, rows)
		copy(grown, *buf)
		for i := 0; i < rows; i++ {
			grown[i] = make([]string, columns)
		}
		*buf = grown
	}
	result := (*buf)[:rows]
	for i := range result {
		if cap(result[i]) < columns {
			result[i] = make([]string, columns)
			continue
		}
		result[i] = result[i][:columns]
	}
	return result
}

func (a *App) podTableRows(rows []state.PodRow, now time.Time) [][]string {
	result := ensureRowCellBuffer(&a.podTableRowBuf, len(rows), 12)
	for i, row := range rows {
		a.fillPodCells(result[i], row.WithAge(now))
	}
	return result
}

func (a *App) nodeTableRows(rows []state.NodeRow, now time.Time) [][]string {
	result := ensureRowCellBuffer(&a.nodeTableRowBuf, len(rows), 10)
	for i, row := range rows {
		a.fillNodeCells(result[i], row.WithAge(now))
	}
	return result
}

func renderUsageSection(title string, lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return title + "\n" + strings.Join(lines, "\n")
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
	return renderUsageSection("Resource usage:", lines)
}

func renderPodUsageSectionWithLabel(usage cluster.PodResourceUsage, label string) string {
	base := renderPodUsageSection(usage)
	if base == "" {
		if strings.TrimSpace(label) == "" {
			return ""
		}
		return "Resource usage (" + label + "):\n- waiting for metrics..."
	}
	if strings.TrimSpace(label) == "" {
		return base
	}
	return strings.Replace(base, "Resource usage:", "Resource usage ("+label+"):", 1)
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
	return renderUsageSection("Resource usage:", lines)
}

func renderNodeUsageSectionWithLabel(usage cluster.NodeResourceUsage, label string) string {
	base := renderNodeUsageSection(usage)
	if base == "" {
		if strings.TrimSpace(label) == "" {
			return ""
		}
		return "Resource usage (" + label + "):\n- waiting for metrics..."
	}
	if strings.TrimSpace(label) == "" {
		return base
	}
	return strings.Replace(base, "Resource usage:", "Resource usage ("+label+"):", 1)
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

const tableUsageBarWidth = 8

func formatPodTableCPU(usage cluster.PodResourceUsage) string {
	if !usage.HasCPUUsage {
		return ""
	}
	total := podUsageReference(formatCPUReference(usage.CPULimitMilli), formatCPUReference(usage.CPURequestMilli))
	return formatUsageBarCell(formatCPU(usage.CPUUsedMilli), total, barRatio(float64(usage.CPUUsedMilli), float64(podUsageDenominator(usage.CPULimitMilli, usage.CPURequestMilli))), requestRatio(usage.CPURequestMilli, podUsageDenominator(usage.CPULimitMilli, usage.CPURequestMilli)))
}

func formatPodTableMemory(usage cluster.PodResourceUsage) string {
	if !usage.HasMemoryUsage {
		return ""
	}
	total := podUsageReference(formatBytesReference(usage.MemoryLimitBytes), formatBytesReference(usage.MemoryRequestBytes))
	return formatUsageBarCell(formatBytes(usage.MemoryUsedBytes), total, barRatio(float64(usage.MemoryUsedBytes), float64(podUsageDenominator(usage.MemoryLimitBytes, usage.MemoryRequestBytes))), requestRatio(usage.MemoryRequestBytes, podUsageDenominator(usage.MemoryLimitBytes, usage.MemoryRequestBytes)))
}

func formatPodTableEphemeral(usage cluster.PodResourceUsage) string {
	if !usage.HasEphemeralUsage {
		return ""
	}
	total := podUsageReference(formatBytesReference(usage.EphemeralLimitBytes), formatBytesReference(usage.EphemeralRequestBytes))
	return formatUsageBarCell(formatBytes(usage.EphemeralUsedBytes), total, barRatio(float64(usage.EphemeralUsedBytes), float64(podUsageDenominator(usage.EphemeralLimitBytes, usage.EphemeralRequestBytes))), requestRatio(usage.EphemeralRequestBytes, podUsageDenominator(usage.EphemeralLimitBytes, usage.EphemeralRequestBytes)))
}

func formatPodTableGPU(usage cluster.PodResourceUsage) string {
	if !usage.HasGPU {
		return ""
	}
	total := formatCountReference(usage.GPUAllocated)
	return formatUsageBarCell(fmt.Sprintf("%d", usage.GPUAllocated), total, 1, -1)
}

func formatNodeTableCPU(usage cluster.NodeResourceUsage) string {
	if !usage.HasCPUUsage {
		return ""
	}
	return formatUsageBarCell(formatCPU(usage.CPUUsedMilli), formatCPUReference(usage.CPUAllocatableMilli), barRatio(float64(usage.CPUUsedMilli), float64(usage.CPUAllocatableMilli)), -1)
}

func formatNodeTableMemory(usage cluster.NodeResourceUsage) string {
	if !usage.HasMemoryUsage {
		return ""
	}
	return formatUsageBarCell(formatBytes(usage.MemoryUsedBytes), formatBytesReference(usage.MemoryAllocatable), barRatio(float64(usage.MemoryUsedBytes), float64(usage.MemoryAllocatable)), -1)
}

func formatNodeTableEphemeral(usage cluster.NodeResourceUsage) string {
	if !usage.HasEphemeralUsage {
		return ""
	}
	return formatUsageBarCell(formatBytes(usage.EphemeralUsedBytes), formatBytesReference(usage.EphemeralAllocatable), barRatio(float64(usage.EphemeralUsedBytes), float64(usage.EphemeralAllocatable)), -1)
}

func formatNodeTableGPU(usage cluster.NodeResourceUsage) string {
	if !usage.HasGPU {
		return ""
	}
	return formatUsageBarCell(fmt.Sprintf("%d", usage.GPUAllocated), formatCountReference(usage.GPUAllocatable), barRatio(float64(usage.GPUAllocated), float64(usage.GPUAllocatable)), -1)
}

func formatUsageBarCell(current string, total string, usedRatio float64, markerRatio float64) string {
	if strings.TrimSpace(current) == "" {
		return ""
	}
	bar := renderMiniUsageBar(usedRatio, markerRatio, tableUsageBarWidth)
	if strings.TrimSpace(total) == "" {
		if bar == "" {
			return current
		}
		return bar + " " + current
	}
	if bar == "" {
		return current + "/" + total
	}
	return bar + " " + current + "/" + total
}

func renderMiniUsageBar(usedRatio float64, markerRatio float64, width int) string {
	if width <= 0 || usedRatio < 0 {
		return ""
	}
	if usedRatio > 1 {
		usedRatio = 1
	}
	filled := int(usedRatio * float64(width))
	if filled == 0 && usedRatio > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	marker := -1
	if markerRatio >= 0 && markerRatio < 1 {
		marker = int(markerRatio * float64(width))
		if marker >= width {
			marker = width - 1
		}
		if marker < 0 {
			marker = 0
		}
	}
	fillStyle := usageBarFillStyle(usedRatio)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	markerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true)
	parts := make([]string, 0, width)
	for idx := 0; idx < width; idx++ {
		if idx == marker {
			parts = append(parts, markerStyle.Render("│"))
			continue
		}
		if idx < filled {
			parts = append(parts, fillStyle.Render("█"))
			continue
		}
		parts = append(parts, emptyStyle.Render("░"))
	}
	return strings.Join(parts, "")
}

func usageBarFillStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 0.9:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	case ratio >= 0.75:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	case ratio >= 0.55:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	}
}

func requestRatio(request int64, total int64) float64 {
	if request <= 0 || total <= 0 || request >= total {
		return -1
	}
	return float64(request) / float64(total)
}
