package app

import (
	"container/heap"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

type overviewTone uint8

const (
	overviewToneOK overviewTone = iota
	overviewToneWarn
	overviewToneError
	overviewToneMuted
)

type overviewMetric struct {
	Label string
	Count int
	Tone  overviewTone
}

type overviewCard struct {
	Title   string
	Metrics []overviewMetric
}

type overviewLine struct {
	Primary   string
	Secondary string
	Tone      overviewTone
}

type catalogOverviewData struct {
	ScopeLabel string
	Cards      []overviewCard
	Warnings   []overviewLine
	Restarts   []overviewLine
}

const catalogOverviewRefreshInterval = 15 * time.Second

func (a *App) maybeRefreshCatalogOverviewCmd(now time.Time) tea.Cmd {
	if a.screen != screenCatalog {
		return nil
	}
	if a.catalogOverviewLoading {
		return nil
	}
	scopeKey := a.catalogOverviewCurrentScopeKey()
	storeVersion := a.store.Version()
	managerVersion := a.manager.Version()
	if a.catalogOverviewScopeKey == scopeKey && a.catalogOverviewStoreVersion == storeVersion && a.catalogOverviewManagerVersion == managerVersion && !a.catalogOverviewFetchedAt.IsZero() && now.Sub(a.catalogOverviewFetchedAt) < catalogOverviewRefreshInterval {
		return nil
	}
	a.catalogOverviewLoading = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return catalogOverviewResultMsg{scopeKey: scopeKey, storeVersion: storeVersion, managerVersion: managerVersion, data: a.buildCatalogOverviewWithContext(ctx, now)}
	}
}

func (a *App) catalogOverviewCurrentScopeKey() string {
	return a.contextScope + "|" + a.namespace
}

func (a *App) buildCatalogOverview(now time.Time) catalogOverviewData {
	return a.buildCatalogOverviewWithContext(context.Background(), now)
}

func (a *App) buildCatalogOverviewWithContext(ctx context.Context, now time.Time) catalogOverviewData {
	data := catalogOverviewData{ScopeLabel: a.catalogOverviewScopeLabel()}
	data.Cards = []overviewCard{
		a.buildPodOverviewCard(),
		a.buildGenericWorkloadOverviewCard(ctx, "Deployments", "apps", "deployments"),
		a.buildGenericWorkloadOverviewCard(ctx, "ReplicaSets", "apps", "replicasets"),
		a.buildGenericWorkloadOverviewCard(ctx, "DaemonSets", "apps", "daemonsets"),
		a.buildGenericWorkloadOverviewCard(ctx, "StatefulSets", "apps", "statefulsets"),
		a.buildCronJobOverviewCard(ctx),
		a.buildJobOverviewCard(ctx),
	}
	data.Warnings = a.buildRecentWarningLines(ctx, now)
	data.Restarts = a.buildRecentRestartLines(now)
	return data
}

func (a *App) renderCatalogBody() string {
	bodyWidth := max(24, a.width-4)
	bodyHeight := a.bodyHeight(a.catalogTopRows())
	overview := renderCatalogOverview(a.catalogOverview, bodyWidth, max(0, bodyHeight-4))
	if overview == "" && a.catalogOverviewLoading {
		overview = theme.Muted.Render("Loading overview...")
	}
	navHeight := bodyHeight
	if overview != "" {
		navHeight = max(4, bodyHeight-countLines(overview)-1)
	}
	a.navTable.SetSize(bodyWidth, navHeight)
	if overview == "" {
		return a.navTable.View()
	}
	return overview + "\n\n" + a.navTable.View()
}

func (a *App) catalogTopRows() int {
	rows := 2
	if a.statusMessage != "" {
		rows++
	}
	if a.filter.Active() || strings.TrimSpace(a.currentQuery()) != "" {
		rows++
	}
	return rows + 1
}

func (a *App) catalogOverviewScopeLabel() string {
	if strings.TrimSpace(a.namespace) == "" {
		return "All namespaces"
	}
	return a.namespace
}

func (a *App) buildPodOverviewCard() overviewCard {
	counts := make(map[string]overviewMetric, 8)
	a.store.ForEachPod(func(row state.PodRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		label, tone := podOverviewBucket(row.Status)
		metric := counts[label]
		metric.Label = label
		metric.Tone = tone
		metric.Count++
		counts[label] = metric
		return true
	})
	return overviewCard{Title: "Pods", Metrics: orderedOverviewMetrics(counts, []string{"Running", "Error", "Pending", "Unschedulable", "ImagePullBackOff", "CrashLoopBackOff", "Completed"})}
}

func (a *App) buildGenericWorkloadOverviewCard(ctx context.Context, title string, apiGroup string, resource string) overviewCard {
	counts := make(map[string]overviewMetric, 4)
	a.forEachFilteredCatalogRow(ctx, apiGroup, resource, func(row cluster.GenericResourceRow) bool {
		label, tone := genericReplicaOverviewBucket(row)
		metric := counts[label]
		metric.Label = label
		metric.Tone = tone
		metric.Count++
		counts[label] = metric
		return true
	})
	return overviewCard{Title: title, Metrics: orderedOverviewMetrics(counts, []string{"Running", "Pending", "Unavailable", "Idle", "Failed"})}
}

func (a *App) buildCronJobOverviewCard(ctx context.Context) overviewCard {
	counts := make(map[string]overviewMetric, 3)
	a.forEachFilteredCatalogRow(ctx, "batch", "cronjobs", func(row cluster.GenericResourceRow) bool {
		label, tone := cronJobOverviewBucket(row.Object)
		metric := counts[label]
		metric.Label = label
		metric.Tone = tone
		metric.Count++
		counts[label] = metric
		return true
	})
	return overviewCard{Title: "CronJobs", Metrics: orderedOverviewMetrics(counts, []string{"Scheduled", "Suspended", "Failed"})}
}

func (a *App) buildJobOverviewCard(ctx context.Context) overviewCard {
	counts := make(map[string]overviewMetric, 4)
	a.forEachFilteredCatalogRow(ctx, "batch", "jobs", func(row cluster.GenericResourceRow) bool {
		label, tone := jobOverviewBucket(row.Object)
		metric := counts[label]
		metric.Label = label
		metric.Tone = tone
		metric.Count++
		counts[label] = metric
		return true
	})
	return overviewCard{Title: "Jobs", Metrics: orderedOverviewMetrics(counts, []string{"Running", "Failed", "Complete", "Pending"})}
}

// forEachFilteredCatalogRow streams generic rows for the catalog scope without materializing a full slice.
func (a *App) forEachFilteredCatalogRow(ctx context.Context, apiGroup string, resource string, visit func(cluster.GenericResourceRow) bool) {
	kind, ok := a.catalogResourceKind(apiGroup, resource)
	if !ok {
		return
	}
	_ = a.manager.ForEachGenericResourceRow(ctx, kind, func(row cluster.GenericResourceRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if kind.Namespaced && a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		return visit(row)
	})
}

func (a *App) catalogResourceKind(apiGroup string, resource string) (cluster.ResourceKind, bool) {
	id := apiGroup + "/" + resource
	for _, group := range a.catalog {
		for _, kind := range group.Resources {
			if kind.ID == id {
				return kind, true
			}
		}
	}
	return cluster.ResourceKind{}, false
}

type eventOverviewItem struct {
	Cluster string
	Reason  string
	Count   int64
	When    time.Time
}

// warningMinHeap keeps the newest Warning events by time using a min-heap of size k (oldest of the k at index 0).
type warningMinHeap []eventOverviewItem

func (h warningMinHeap) Len() int           { return len(h) }
func (h warningMinHeap) Less(i, j int) bool { return h[i].When.Before(h[j].When) }
func (h warningMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *warningMinHeap) Push(x interface{}) { *h = append(*h, x.(eventOverviewItem)) }

func (h *warningMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func keepWarningCandidate(h *warningMinHeap, item eventOverviewItem, k int) {
	if k <= 0 {
		return
	}
	if h.Len() < k {
		heap.Push(h, item)
		return
	}
	// Root holds the oldest Warning among the current top-k by time.
	if item.When.After((*h)[0].When) {
		heap.Pop(h)
		heap.Push(h, item)
	}
}

func (a *App) buildRecentWarningLines(ctx context.Context, now time.Time) []overviewLine {
	const topK = 5
	h := &warningMinHeap{}
	a.forEachFilteredCatalogRow(ctx, "", "events", func(row cluster.GenericResourceRow) bool {
		if row.Object == nil {
			return true
		}
		eventType, _, _ := unstructured.NestedString(row.Object.Object, "type")
		if !strings.EqualFold(eventType, "Warning") {
			return true
		}
		reason, _, _ := unstructured.NestedString(row.Object.Object, "reason")
		count, found, _ := unstructured.NestedInt64(row.Object.Object, "count")
		if !found || count <= 0 {
			count = 1
		}
		when := overviewEventTime(row.Object)
		item := eventOverviewItem{Cluster: row.Cluster, Reason: firstNonEmptyString(reason, row.Status, row.Name), Count: count, When: when}
		keepWarningCandidate(h, item, topK)
		return true
	})
	items := append([]eventOverviewItem(nil), *h...)
	sort.SliceStable(items, func(i int, j int) bool {
		if !items[i].When.Equal(items[j].When) {
			return items[i].When.After(items[j].When)
		}
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Reason < items[j].Reason
	})
	lines := make([]overviewLine, 0, max(1, len(items)))
	showCluster := a.contextScope == "" && len(a.manager.ConnectedContextNames()) > 1
	for _, item := range items {
		primary := fmt.Sprintf("%s (%dx)", item.Reason, item.Count)
		if showCluster {
			primary = item.Cluster + " / " + primary
		}
		secondary := ageAgo(now, item.When)
		lines = append(lines, overviewLine{Primary: primary, Secondary: secondary, Tone: overviewToneWarn})
	}
	if len(lines) == 0 {
		lines = append(lines, overviewLine{Primary: "No recent warnings", Tone: overviewToneMuted})
	}
	return lines
}

type restartOverviewItem struct {
	Cluster   string
	Namespace string
	Pod       string
	Reason    string
	ExitCode  int32
	Restarts  int
	When      time.Time
}

// restartWorse reports whether a should sort after b (lower priority for the overview).
func restartWorse(a, b restartOverviewItem) bool {
	if !a.When.Equal(b.When) {
		return a.When.Before(b.When)
	}
	if a.Restarts != b.Restarts {
		return a.Restarts < b.Restarts
	}
	return a.Pod > b.Pod
}

// restartMinHeap keeps the best restartOverviewItem rows using a min-heap where the root is the worst among k.
type restartMinHeap []restartOverviewItem

func (h restartMinHeap) Len() int { return len(h) }
func (h restartMinHeap) Less(i, j int) bool {
	return restartWorse(h[i], h[j])
}
func (h restartMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *restartMinHeap) Push(x interface{}) { *h = append(*h, x.(restartOverviewItem)) }

func (h *restartMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func keepRestartCandidate(h *restartMinHeap, item restartOverviewItem, k int) {
	if k <= 0 {
		return
	}
	if h.Len() < k {
		heap.Push(h, item)
		return
	}
	if restartWorse((*h)[0], item) {
		heap.Pop(h)
		heap.Push(h, item)
	}
}

func (a *App) buildRecentRestartLines(now time.Time) []overviewLine {
	const topK = 5
	h := &restartMinHeap{}
	a.store.ForEachPod(func(row state.PodRow) bool {
		if !a.contextMatches(row.Cluster) {
			return true
		}
		if a.namespace != "" && row.Namespace != a.namespace {
			return true
		}
		if row.Restarts == 0 {
			return true
		}
		pod, ok := a.store.PodObjectByKey(row.Key)
		if !ok {
			return true
		}
		item, ok := podRestartOverviewItem(pod, row)
		if !ok {
			return true
		}
		keepRestartCandidate(h, item, topK)
		return true
	})
	items := append([]restartOverviewItem(nil), *h...)
	sort.SliceStable(items, func(i int, j int) bool {
		if !items[i].When.Equal(items[j].When) {
			return items[i].When.After(items[j].When)
		}
		if items[i].Restarts != items[j].Restarts {
			return items[i].Restarts > items[j].Restarts
		}
		return items[i].Pod < items[j].Pod
	})
	lines := make([]overviewLine, 0, max(1, len(items)))
	showCluster := a.contextScope == "" && len(a.manager.ConnectedContextNames()) > 1
	for _, item := range items {
		location := item.Namespace + " / " + item.Pod
		if showCluster {
			location = item.Cluster + " / " + location
		}
		reason := item.Reason
		if item.ExitCode != 0 || item.Reason != "" {
			reason = fmt.Sprintf("%s (ExitCode: %d)", firstNonEmptyString(item.Reason, "Restarted"), item.ExitCode)
		}
		lines = append(lines, overviewLine{
			Primary:   location,
			Secondary: fmt.Sprintf("%s  (%dx)  %s", reason, item.Restarts, ageAgo(now, item.When)),
			Tone:      overviewToneError,
		})
	}
	if len(lines) == 0 {
		lines = append(lines, overviewLine{Primary: "No recent restarts", Tone: overviewToneMuted})
	}
	return lines
}

func podRestartOverviewItem(pod *corev1.Pod, row state.PodRow) (restartOverviewItem, bool) {
	if pod == nil {
		return restartOverviewItem{}, false
	}
	best := restartOverviewItem{}
	found := false
	consider := func(statuses []corev1.ContainerStatus) {
		for _, status := range statuses {
			if status.RestartCount == 0 {
				continue
			}
			terminated := status.LastTerminationState.Terminated
			if terminated == nil {
				terminated = status.State.Terminated
			}
			when := pod.CreationTimestamp.Time
			reason := row.Status
			var exitCode int32
			if terminated != nil {
				if !terminated.FinishedAt.Time.IsZero() {
					when = terminated.FinishedAt.Time
				}
				reason = firstNonEmptyString(terminated.Reason, reason)
				exitCode = terminated.ExitCode
			}
			item := restartOverviewItem{
				Cluster:   row.Cluster,
				Namespace: row.Namespace,
				Pod:       row.Name,
				Reason:    reason,
				ExitCode:  exitCode,
				Restarts:  int(status.RestartCount),
				When:      when,
			}
			if !found || item.When.After(best.When) || (item.When.Equal(best.When) && item.Restarts > best.Restarts) {
				best = item
				found = true
			}
		}
	}
	consider(pod.Status.InitContainerStatuses)
	consider(pod.Status.ContainerStatuses)
	return best, found
}

func podOverviewBucket(status string) (string, overviewTone) {
	normalized := strings.TrimSpace(status)
	switch normalized {
	case "Running":
		return "Running", overviewToneOK
	case "Pending":
		return "Pending", overviewToneWarn
	case "Succeeded", "Completed":
		return "Completed", overviewToneMuted
	case "Unschedulable":
		return "Unschedulable", overviewToneError
	case "ImagePullBackOff", "ErrImagePull":
		return "ImagePullBackOff", overviewToneError
	case "CrashLoopBackOff":
		return "CrashLoopBackOff", overviewToneError
	case "Failed", "Error", "CreateContainerConfigError", "CreateContainerError", "RunContainerError", "OOMKilled":
		return "Error", overviewToneError
	default:
		return normalized, overviewToneError
	}
}

func genericReplicaOverviewBucket(row cluster.GenericResourceRow) (string, overviewTone) {
	ready, desired, ok := parseFraction(row.Ready)
	if ok {
		if desired == 0 {
			return "Idle", overviewToneMuted
		}
		if ready >= desired {
			return "Running", overviewToneOK
		}
		if ready == 0 {
			return "Unavailable", overviewToneError
		}
		return "Pending", overviewToneWarn
	}
	status := strings.ToLower(strings.TrimSpace(row.Status))
	switch {
	case status == "", status == "true", status == "ready":
		return "Running", overviewToneOK
	case strings.Contains(status, "fail"), strings.Contains(status, "error"):
		return "Failed", overviewToneError
	default:
		return "Pending", overviewToneWarn
	}
}

func cronJobOverviewBucket(object *unstructured.Unstructured) (string, overviewTone) {
	if object == nil {
		return "Scheduled", overviewToneOK
	}
	suspended, found, _ := unstructured.NestedBool(object.Object, "spec", "suspend")
	if found && suspended {
		return "Suspended", overviewToneMuted
	}
	return "Scheduled", overviewToneOK
}

func jobOverviewBucket(object *unstructured.Unstructured) (string, overviewTone) {
	if object == nil {
		return "Pending", overviewToneWarn
	}
	failed, _, _ := unstructured.NestedInt64(object.Object, "status", "failed")
	succeeded, _, _ := unstructured.NestedInt64(object.Object, "status", "succeeded")
	active, _, _ := unstructured.NestedInt64(object.Object, "status", "active")
	if failed > 0 {
		return "Failed", overviewToneError
	}
	if active > 0 {
		return "Running", overviewToneOK
	}
	if succeeded > 0 {
		return "Complete", overviewToneMuted
	}
	return "Pending", overviewToneWarn
}

func overviewEventTime(object *unstructured.Unstructured) time.Time {
	if object == nil {
		return time.Time{}
	}
	for _, path := range [][]string{{"eventTime"}, {"lastTimestamp"}, {"metadata", "creationTimestamp"}} {
		value, found, _ := unstructured.NestedString(object.Object, path...)
		if !found || strings.TrimSpace(value) == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return parsed
		}
	}
	return object.GetCreationTimestamp().Time
}

func orderedOverviewMetrics(counts map[string]overviewMetric, preferred []string) []overviewMetric {
	ordered := make([]overviewMetric, 0, len(counts))
	seen := make(map[string]struct{}, len(counts))
	for _, label := range preferred {
		metric, ok := counts[label]
		if !ok || metric.Count == 0 {
			continue
		}
		ordered = append(ordered, metric)
		seen[label] = struct{}{}
	}
	extra := make([]overviewMetric, 0, len(counts))
	for label, metric := range counts {
		if _, ok := seen[label]; ok || metric.Count == 0 {
			continue
		}
		extra = append(extra, metric)
	}
	sort.SliceStable(extra, func(i int, j int) bool {
		if extra[i].Count != extra[j].Count {
			return extra[i].Count > extra[j].Count
		}
		return extra[i].Label < extra[j].Label
	})
	ordered = append(ordered, extra...)
	if len(ordered) > 5 {
		ordered = ordered[:5]
	}
	if len(ordered) == 0 {
		return []overviewMetric{{Label: "Idle", Count: 0, Tone: overviewToneMuted}}
	}
	return ordered
}

func renderCatalogOverview(data catalogOverviewData, width int, maxLines int) string {
	if width <= 0 || maxLines < 6 {
		return ""
	}
	sections := make([]string, 0, 3)
	sections = append(sections, renderOverviewScopeLabel(data.ScopeLabel))
	sections = append(sections, renderOverviewCardGrid(data.Cards, width))
	if maxLines >= 13 {
		sections = append(sections, renderOverviewBottomSections(data.Warnings, data.Restarts, width))
	}
	return strings.Join(sections, "\n\n")
}

func renderOverviewScopeLabel(label string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Background(lipgloss.Color("236")).Padding(0, 1).Render(label)
}

func renderOverviewCardGrid(cards []overviewCard, width int) string {
	if len(cards) == 0 {
		return ""
	}
	cardsPerRow := min(len(cards), max(1, width/18))
	gap := 2
	cardWidth := max(16, (width-(cardsPerRow-1)*gap)/cardsPerRow)
	rows := make([]string, 0, (len(cards)+cardsPerRow-1)/cardsPerRow)
	for start := 0; start < len(cards); start += cardsPerRow {
		end := min(len(cards), start+cardsPerRow)
		rendered := make([]string, 0, end-start+(end-start-1))
		for idx, card := range cards[start:end] {
			if idx != 0 {
				rendered = append(rendered, strings.Repeat(" ", gap))
			}
			rendered = append(rendered, renderOverviewCard(card, cardWidth))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rendered...))
	}
	return strings.Join(rows, "\n\n")
}

func renderOverviewCard(card overviewCard, width int) string {
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Width(width).Render(card.Title)}
	lines = append(lines, renderOverviewBar(card.Metrics, width))
	for idx := 0; idx < 5; idx++ {
		if idx < len(card.Metrics) {
			metric := card.Metrics[idx]
			line := metricStyle(metric.Tone).Render(fmt.Sprintf("%d %s", metric.Count, metric.Label))
			lines = append(lines, lipgloss.NewStyle().Width(width).Render(line))
			continue
		}
		lines = append(lines, strings.Repeat(" ", width))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func renderOverviewBar(metrics []overviewMetric, width int) string {
	barWidth := max(8, width-2)
	total := 0
	for _, metric := range metrics {
		if metric.Count > 0 {
			total += metric.Count
		}
	}
	if total == 0 {
		return lipgloss.NewStyle().Background(lipgloss.Color("238")).Render(strings.Repeat(" ", barWidth))
	}
	widths := make([]int, len(metrics))
	used := 0
	active := 0
	for idx, metric := range metrics {
		if metric.Count <= 0 {
			continue
		}
		active++
		w := metric.Count * barWidth / total
		if w == 0 {
			w = 1
		}
		widths[idx] = w
		used += w
	}
	for used > barWidth {
		for idx := range widths {
			if used <= barWidth {
				break
			}
			if widths[idx] > 1 {
				widths[idx]--
				used--
			}
		}
	}
	for used < barWidth && active != 0 {
		for idx, metric := range metrics {
			if used >= barWidth {
				break
			}
			if metric.Count <= 0 {
				continue
			}
			widths[idx]++
			used++
		}
	}
	parts := make([]string, 0, len(metrics))
	for idx, metric := range metrics {
		if widths[idx] == 0 {
			continue
		}
		parts = append(parts, metricBarStyle(metric.Tone).Render(strings.Repeat(" ", widths[idx])))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func renderOverviewBottomSections(warnings []overviewLine, restarts []overviewLine, width int) string {
	if width < 90 {
		return renderOverviewList("Recent Warnings", warnings, width) + "\n\n" + renderOverviewList("Recent Restarts", restarts, width)
	}
	leftWidth := (width - 2) / 2
	rightWidth := width - 2 - leftWidth
	return lipgloss.JoinHorizontal(lipgloss.Top,
		renderOverviewList("Recent Warnings", warnings, leftWidth),
		"  ",
		renderOverviewList("Recent Restarts", restarts, rightWidth),
	)
}

func renderOverviewList(title string, lines []overviewLine, width int) string {
	rendered := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")).Width(width).Render(title)}
	for idx := 0; idx < 5; idx++ {
		if idx >= len(lines) {
			rendered = append(rendered, strings.Repeat(" ", width))
			continue
		}
		line := lines[idx]
		primary := metricStyle(line.Tone).Render(line.Primary)
		if strings.TrimSpace(line.Secondary) == "" {
			rendered = append(rendered, lipgloss.NewStyle().Width(width).Render(primary))
			continue
		}
		secondary := theme.Muted.Render(line.Secondary)
		rendered = append(rendered, lipgloss.NewStyle().Width(width).Render(primary+"  "+secondary))
	}
	return strings.Join(rendered, "\n")
}

func metricStyle(tone overviewTone) lipgloss.Style {
	switch tone {
	case overviewToneOK:
		return theme.StatusOK
	case overviewToneWarn:
		return theme.StatusWarn
	case overviewToneError:
		return theme.StatusError
	default:
		return theme.Muted
	}
}

func metricBarStyle(tone overviewTone) lipgloss.Style {
	switch tone {
	case overviewToneOK:
		return lipgloss.NewStyle().Background(lipgloss.Color("42"))
	case overviewToneWarn:
		return lipgloss.NewStyle().Background(lipgloss.Color("214"))
	case overviewToneError:
		return lipgloss.NewStyle().Background(lipgloss.Color("203"))
	default:
		return lipgloss.NewStyle().Background(lipgloss.Color("240"))
	}
}

func ageAgo(now time.Time, when time.Time) string {
	if when.IsZero() {
		return ""
	}
	return formatOverviewAge(now.Sub(when)) + " ago"
}

func formatOverviewAge(age time.Duration) string {
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

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func countLines(value string) int {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}
