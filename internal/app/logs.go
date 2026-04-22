package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/elijahrou/surfsk8s/internal/actions"
	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

const (
	defaultLogTailLines    int64 = 50
	defaultNodeTailBytes   int64 = 64 * 1024
	logLiveRefreshInterval       = time.Second
	maxLiveLogEntries            = 10000
	logFetchConcurrency          = 4
)

var writeTextFile = os.WriteFile

type logTarget uint8

const (
	logTargetNone logTarget = iota
	logTargetPod
	logTargetDeployment
	logTargetNode
)

type logRange uint8

const (
	logRangeLive logRange = iota
	logRange5m
	logRange10m
	logRange30m
	logRange1h
	logRangeAll
)

type logEntry struct {
	UniqueKey     string
	Timestamp     time.Time
	HasTimestamp  bool
	TimestampText string
	SourceKey     string
	SourceLabel   string
	Message       string
	Order         int
}

func logEntryLess(left logEntry, right logEntry) bool {
	if left.HasTimestamp && right.HasTimestamp && !left.Timestamp.Equal(right.Timestamp) {
		return left.Timestamp.Before(right.Timestamp)
	}
	if left.SourceLabel != right.SourceLabel {
		return left.SourceLabel < right.SourceLabel
	}
	return left.Order < right.Order
}

func mergeSortedLogEntries(existing []logEntry, incoming []logEntry) []logEntry {
	if len(existing) == 0 {
		return append([]logEntry(nil), incoming...)
	}
	if len(incoming) == 0 {
		return existing
	}
	merged := make([]logEntry, 0, len(existing)+len(incoming))
	i := 0
	j := 0
	for i < len(existing) && j < len(incoming) {
		if logEntryLess(existing[i], incoming[j]) {
			merged = append(merged, existing[i])
			i++
			continue
		}
		merged = append(merged, incoming[j])
		j++
	}
	merged = append(merged, existing[i:]...)
	merged = append(merged, incoming[j:]...)
	return merged
}

type logFetchSource struct {
	Key         string
	Label       string
	Pod         state.PodDetails
	Container   string
	Node        state.NodeDetails
	NodeLogPath string
}

type logsResultMsg struct {
	Token   uint64
	Replace bool
	Cursor  time.Time
	Entries []logEntry
	Err     error
}

type nodeLogPickerResultMsg struct {
	Token   uint64
	Key     state.NodeKey
	Dir     string
	Title   string
	Entries []cluster.NodeLogEntry
	Err     error
}

func (r logRange) label() string {
	switch r {
	case logRangeLive:
		return "live"
	case logRange5m:
		return "5m"
	case logRange10m:
		return "10m"
	case logRange30m:
		return "30m"
	case logRange1h:
		return "1h"
	case logRangeAll:
		return "all"
	default:
		return "live"
	}
}

func (r logRange) menuLabel() string {
	switch r {
	case logRangeLive:
		return "live"
	case logRange5m:
		return "5 minutes"
	case logRange10m:
		return "10 minutes"
	case logRange30m:
		return "30 minutes"
	case logRange1h:
		return "1 hour"
	case logRangeAll:
		return "all time"
	default:
		return "live"
	}
}

func (r logRange) sinceTime(now time.Time) *time.Time {
	switch r {
	case logRange5m:
		value := now.Add(-5 * time.Minute)
		return &value
	case logRange10m:
		value := now.Add(-10 * time.Minute)
		return &value
	case logRange30m:
		value := now.Add(-30 * time.Minute)
		return &value
	case logRange1h:
		value := now.Add(-time.Hour)
		return &value
	default:
		return nil
	}
}

func (r logRange) nodeTailBytes() int64 {
	switch r {
	case logRangeLive:
		return defaultNodeTailBytes
	case logRange5m:
		return 128 * 1024
	case logRange10m:
		return 256 * 1024
	case logRange30m:
		return 512 * 1024
	case logRange1h:
		return 1024 * 1024
	case logRangeAll:
		return 0
	default:
		return defaultNodeTailBytes
	}
}

func logRangeOptions(current logRange) []actionOption {
	ranges := []logRange{logRangeLive, logRange5m, logRange10m, logRange30m, logRange1h, logRangeAll}
	options := make([]actionOption, 0, len(ranges))
	for _, item := range ranges {
		label := item.menuLabel()
		if item == current {
			label = "● " + label
		}
		options = append(options, actionOption{Label: label, LogRange: item})
	}
	return options
}

func (a *App) nextAsyncTokenValue() uint64 {
	a.nextAsyncToken++
	return a.nextAsyncToken
}

func (a *App) updateLogKeys(msg tea.KeyMsg) tea.Cmd {
	if a.updateTextViewportKeys(msg) {
		return nil
	}
	switch msg.String() {
	case "esc", "backspace":
		a.stopLogsScreen()
		return nil
	case "u":
		return a.refreshLogs(true)
	case " ":
		a.logAutoRefreshPaused = !a.logAutoRefreshPaused
		return nil
	case "y":
		return a.yankLogs()
	case "s":
		return a.openLogSavePrompt()
	case "f":
		return a.openLogExactFilter()
	case "c":
		return a.openLogSourcePicker()
	case "p":
		return a.openLogRangePicker()
	case "t":
		a.logShowTimestamps = !a.logShowTimestamps
		return nil
	case "w":
		a.logWrap = !a.logWrap
		return nil
	}
	return nil
}

func (a *App) stopLogsScreen() {
	a.logRequestToken = 0
	a.logLoading = false
	a.activity = ""
	a.screen = a.logsReturnScreen
	a.refreshCurrentScreen(time.Now())
}

func (a *App) openLogSavePrompt() tea.Cmd {
	a.inputMode = inputModeLogSavePath
	a.filter.SetPrompt("save> ")
	a.filter.SetPlaceholder("path")
	a.filter.SetValue(defaultLogSavePath())
	a.filter.Activate()
	return nil
}

func defaultLogSavePath() string {
	return "surfsk8s-logs-" + time.Now().Format("20060102-150405") + ".log"
}

func (a *App) openLogExactFilter() tea.Cmd {
	a.inputMode = inputModeLogExactFilter
	a.filter.SetPrompt("f> ")
	a.filter.SetPlaceholder("exact log filter")
	a.filter.SetValue(strings.TrimPrefix(a.logFilterQuery, "="))
	a.filter.Activate()
	return nil
}

func (a *App) openLogRangePicker() tea.Cmd {
	if a.screen != screenLogs {
		return nil
	}
	return a.openActionPicker("select log range", "enter select  esc cancel", pendingActionLogRange, logRangeOptions(a.logRange))
}

func (a *App) openLogSourcePicker() tea.Cmd {
	switch a.logTarget {
	case logTargetPod:
		return a.openPodLogSourcePicker()
	case logTargetDeployment:
		return a.openDeploymentLogSourcePicker()
	default:
		a.statusMessage = "source toggle unavailable"
		return nil
	}
}

func (a *App) runOpenLogs() tea.Cmd {
	switch a.screen {
	case screenPodDetails:
		return a.runOpenPodLogs()
	case screenResourceDetails:
		switch {
		case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
			return a.runOpenDeploymentLogs()
		case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
			return a.runOpenNodeLogs()
		default:
			a.statusMessage = "logs unsupported for this resource"
			return nil
		}
	default:
		a.statusMessage = "logs unsupported on this screen"
		return nil
	}
}

func (a *App) runOpenPodLogs() tea.Cmd {
	a.logTarget = logTargetPod
	a.logPod = a.activePod
	containers, err := actions.PodContainerNames(a.activePod.Pod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if a.logSelectedContainers == nil {
		a.logSelectedContainers = selectAllLogs(containers)
	}
	if len(containers) == 1 {
		a.logSelectedContainers = selectAllLogs(containers)
		return a.startPodLogs()
	}
	return a.openPodLogSourcePicker()
}

func (a *App) runOpenDeploymentLogs() tea.Cmd {
	a.logTarget = logTargetDeployment
	a.logDeployment = a.activeDeployment
	containers, err := deploymentContainerNames(a.activeDeployment.Deployment)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if a.logSelectedContainers == nil {
		a.logSelectedContainers = selectAllLogs(containers)
	}
	if len(containers) == 1 {
		a.logSelectedContainers = selectAllLogs(containers)
		return a.startDeploymentLogs()
	}
	return a.openDeploymentLogSourcePicker()
}

func (a *App) runOpenNodeLogs() tea.Cmd {
	a.logTarget = logTargetNode
	a.logNode = a.activeNode
	return a.openNodeLogPicker("")
}

func deploymentContainerNames(deployment *appsv1.Deployment) ([]string, error) {
	if deployment == nil {
		return nil, fmt.Errorf("deployment disappeared")
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil, fmt.Errorf("deployment has no containers")
	}
	names := make([]string, 0, len(deployment.Spec.Template.Spec.Containers))
	for _, container := range deployment.Spec.Template.Spec.Containers {
		names = append(names, container.Name)
	}
	return names, nil
}

func selectAllLogs(names []string) map[string]bool {
	selected := make(map[string]bool, len(names))
	for _, name := range names {
		selected[name] = true
	}
	return selected
}

func selectedLogContainers(selected map[string]bool, available []string) []string {
	result := make([]string, 0, len(available))
	for _, name := range available {
		if selected[name] {
			result = append(result, name)
		}
	}
	return result
}

func (a *App) openPodLogSourcePicker() tea.Cmd {
	containers, err := actions.PodContainerNames(a.logPod.Pod)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if len(a.logSelectedContainers) == 0 {
		a.logSelectedContainers = selectAllLogs(containers)
	}
	options := make([]actionOption, 0, len(containers))
	for _, name := range containers {
		options = append(options, actionOption{Label: name, Container: name, Selected: a.logSelectedContainers[name]})
	}
	return a.openActionPicker("select log containers", "space toggle  enter apply  esc cancel", pendingActionPodLogsSources, options)
}

func (a *App) openDeploymentLogSourcePicker() tea.Cmd {
	containers, err := deploymentContainerNames(a.logDeployment.Deployment)
	if err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	if len(a.logSelectedContainers) == 0 {
		a.logSelectedContainers = selectAllLogs(containers)
	}
	options := make([]actionOption, 0, len(containers))
	for _, name := range containers {
		options = append(options, actionOption{Label: name, Container: name, Selected: a.logSelectedContainers[name]})
	}
	return a.openActionPicker("select log containers", "space toggle  enter apply  esc cancel", pendingActionDeploymentLogsSources, options)
}

func (a *App) startPodLogs() tea.Cmd {
	a.logTarget = logTargetPod
	a.logPod = a.activePod
	a.logNodePath = ""
	return a.openLogsScreen(fmt.Sprintf("pod logs · %s/%s", a.logPod.Row.Namespace, a.logPod.Row.Name), true)
}

func (a *App) startDeploymentLogs() tea.Cmd {
	a.logTarget = logTargetDeployment
	a.logDeployment = a.activeDeployment
	a.logNodePath = ""
	return a.openLogsScreen(fmt.Sprintf("deployment logs · %s/%s", a.logDeployment.Row.Namespace, a.logDeployment.Row.Name), true)
}

func (a *App) startNodeLogs(logPath string) tea.Cmd {
	a.logTarget = logTargetNode
	a.logNode = a.activeNode
	a.logNodePath = logPath
	return a.openLogsScreen(fmt.Sprintf("node logs · %s · %s", a.logNode.Row.Name, logPath), true)
}

func (a *App) openNodeLogPicker(dir string) tea.Cmd {
	if a.activeNode.Node == nil {
		a.statusMessage = "node disappeared"
		return nil
	}
	token := a.nextAsyncTokenValue()
	a.nodeLogPickerToken = token
	a.activity = "loading node logs"
	details := a.activeNode
	title := buildNodeLogPickerTitle(dir)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		entries, err := a.manager.NodeLogEntries(ctx, details, dir)
		return nodeLogPickerResultMsg{Token: token, Key: details.Row.Key, Dir: dir, Title: title, Entries: entries, Err: err}
	}
}

func buildNodeLogPickerTitle(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return "select node log"
	}
	return "select node log · " + strings.TrimSpace(dir)
}

func buildNodeLogPickerOptions(entries []cluster.NodeLogEntry) []actionOption {
	options := make([]actionOption, 0, len(entries))
	for _, entry := range entries {
		label := entry.Name
		if entry.Directory && !strings.HasSuffix(label, "/") {
			label += "/"
		}
		options = append(options, actionOption{Label: label, Path: entry.Path, Directory: entry.Directory})
	}
	return options
}

func (a *App) invalidateLogsRenderCache() {
	a.logRenderedContent = ""
	a.logRenderedVersion = 0
}

func (a *App) openLogsScreen(title string, reset bool) tea.Cmd {
	returnScreen := a.screen
	if a.screen == screenActionPicker {
		returnScreen = a.actionReturnScreen
	}
	a.logsReturnScreen = returnScreen
	a.screen = screenLogs
	a.logTitle = title
	if reset {
		a.logEntries = a.logEntries[:0]
		a.logEntrySeen = nil
		a.logCursor = time.Time{}
		a.logFetchedAt = time.Time{}
		a.logEntriesVersion++
		a.invalidateLogsRenderCache()
	}
	a.resetTextViewport()
	return a.refreshLogs(true)
}

func (a *App) maybeRefreshLogsCmd(now time.Time) tea.Cmd {
	if a.screen != screenLogs || a.logTarget == logTargetNone || a.logRange != logRangeLive || a.logLoading || a.logAutoRefreshPaused {
		return nil
	}
	if !a.logFetchedAt.IsZero() && now.Sub(a.logFetchedAt) < logLiveRefreshInterval {
		return nil
	}
	return a.refreshLogs(false)
}

func (a *App) refreshLogs(force bool) tea.Cmd {
	if a.logTarget == logTargetNone {
		return nil
	}
	if a.logLoading && !force {
		return nil
	}

	a.invalidateLogsRenderCache()
	now := time.Now()
	token := a.nextAsyncTokenValue()
	a.logRequestToken = token
	a.logLoading = true
	a.activity = "loading logs"

	fetchTarget := a.logTarget
	fetchRange := a.logRange
	pod := a.logPod
	deployment := a.logDeployment
	node := a.logNode
	nodePath := a.logNodePath
	selected := make(map[string]bool, len(a.logSelectedContainers))
	for key, value := range a.logSelectedContainers {
		selected[key] = value
	}
	cursor := a.logCursor
	replace := true
	var sinceTime *time.Time
	var tailLines *int64

	switch fetchTarget {
	case logTargetPod, logTargetDeployment:
		switch fetchRange {
		case logRangeLive:
			if !a.logFetchedAt.IsZero() && !cursor.IsZero() {
				replace = false
				since := cursor.Add(-1 * time.Second)
				sinceTime = &since
			} else {
				tail := defaultLogTailLines
				tailLines = &tail
			}
		case logRangeAll:
			replace = true
		default:
			replace = true
			sinceTime = fetchRange.sinceTime(now)
		}
	case logTargetNode:
		replace = true
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		entries, nextCursor, err := a.fetchLogEntries(ctx, fetchTarget, pod, deployment, node, nodePath, selected, fetchRange, sinceTime, tailLines)
		return logsResultMsg{Token: token, Replace: replace, Cursor: nextCursor, Entries: entries, Err: err}
	}
}

func (a *App) fetchLogEntries(ctx context.Context, target logTarget, pod state.PodDetails, deployment state.DeploymentDetails, node state.NodeDetails, nodePath string, selected map[string]bool, fetchRange logRange, sinceTime *time.Time, tailLines *int64) ([]logEntry, time.Time, error) {
	now := time.Now()
	sources, err := a.buildLogFetchSources(now, target, pod, deployment, node, nodePath, selected)
	if err != nil {
		return nil, time.Time{}, err
	}

	type logFetchResult struct {
		entries []logEntry
		cursor  time.Time
		err     string
	}

	results := make([]logFetchResult, len(sources))
	concurrency := min(logFetchConcurrency, len(sources))
	if concurrency <= 0 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for idx, source := range sources {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int, source logFetchSource) {
			defer wg.Done()
			defer func() { <-sem }()

			var content string
			var fetchErr error
			switch {
			case source.Container != "":
				content, fetchErr = a.manager.PodLogsWithOptions(ctx, source.Pod, cluster.PodLogsOptions{
					Container:  source.Container,
					Timestamps: true,
					TailLines:  tailLines,
					SinceTime:  sinceTime,
				})
			case source.NodeLogPath != "":
				options := cluster.NodeLogOptions{Path: source.NodeLogPath, TailBytes: fetchRange.nodeTailBytes()}
				if fetchRange == logRangeAll {
					options.All = true
				}
				content, fetchErr = a.manager.NodeLogWithOptions(ctx, source.Node, options)
			default:
				fetchErr = fmt.Errorf("invalid log source")
			}
			if fetchErr != nil {
				results[index].err = source.Label + ": " + fetchErr.Error()
				return
			}
			results[index].entries, results[index].cursor = parseLogEntries(content, source.Key, source.Label, index<<20)
		}(idx, source)
	}
	wg.Wait()

	entries := make([]logEntry, 0, 256)
	cursor := time.Time{}
	errors := make([]string, 0, len(sources))
	for _, result := range results {
		if result.err != "" {
			errors = append(errors, result.err)
			continue
		}
		entries = append(entries, result.entries...)
		if result.cursor.After(cursor) {
			cursor = result.cursor
		}
	}

	sort.SliceStable(entries, func(i int, j int) bool { return logEntryLess(entries[i], entries[j]) })

	if cursor.IsZero() {
		cursor = now
	}
	if len(errors) != 0 {
		sort.Strings(errors)
		joined := strings.Join(errors, "; ")
		if len(entries) == 0 {
			return nil, cursor, fmt.Errorf("%s", joined)
		}
		entries = append(entries, logEntry{UniqueKey: "__error__|" + joined, Message: "Errors: " + joined, SourceKey: "errors", SourceLabel: "errors", Order: len(entries) + 1})
	}
	return entries, cursor, nil
}

func (a *App) buildLogFetchSources(now time.Time, target logTarget, pod state.PodDetails, deployment state.DeploymentDetails, node state.NodeDetails, nodePath string, selected map[string]bool) ([]logFetchSource, error) {
	switch target {
	case logTargetPod:
		containers, err := actions.PodContainerNames(pod.Pod)
		if err != nil {
			return nil, err
		}
		selectedNames := selectedLogContainers(selected, containers)
		if len(selectedNames) == 0 {
			return nil, fmt.Errorf("select at least one container")
		}
		sources := make([]logFetchSource, 0, len(selectedNames))
		for _, container := range selectedNames {
			label := container
			if len(selectedNames) == 1 {
				label = ""
			}
			sources = append(sources, logFetchSource{
				Key:       pod.Row.Name + "/" + container,
				Label:     label,
				Pod:       pod,
				Container: container,
			})
		}
		return sources, nil
	case logTargetDeployment:
		containers, err := deploymentContainerNames(deployment.Deployment)
		if err != nil {
			return nil, err
		}
		selectedNames := selectedLogContainers(selected, containers)
		if len(selectedNames) == 0 {
			return nil, fmt.Errorf("select at least one container")
		}
		pods, err := a.manager.DeploymentPodDetails(deployment, now)
		if err != nil {
			return nil, err
		}
		sources := make([]logFetchSource, 0, len(pods)*len(selectedNames))
		for _, podDetails := range pods {
			for _, container := range selectedNames {
				sources = append(sources, logFetchSource{
					Key:       podDetails.Row.Name + "/" + container,
					Label:     podDetails.Row.Name + "/" + container,
					Pod:       podDetails,
					Container: container,
				})
			}
		}
		return sources, nil
	case logTargetNode:
		if nodePath == "" {
			return nil, fmt.Errorf("node log path missing")
		}
		return []logFetchSource{{Key: nodePath, Label: nodePath, Node: node, NodeLogPath: nodePath}}, nil
	default:
		return nil, fmt.Errorf("logs unavailable")
	}
}

func parseLogEntries(content string, sourceKey string, sourceLabel string, orderStart int) ([]logEntry, time.Time) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimRight(content, "\n"), "\r\n", "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, time.Time{}
	}
	entries := make([]logEntry, 0, len(lines))
	maxTime := time.Time{}
	for index, line := range lines {
		stamp, stampText, message, ok := splitLogTimestamp(line)
		entry := logEntry{
			SourceKey:     sourceKey,
			SourceLabel:   sourceLabel,
			Message:       message,
			Timestamp:     stamp,
			HasTimestamp:  ok,
			TimestampText: stampText,
			Order:         orderStart + index,
		}
		entry.UniqueKey = sourceKey + "|" + stampText + "|" + message
		entries = append(entries, entry)
		if ok && stamp.After(maxTime) {
			maxTime = stamp
		}
	}
	return entries, maxTime
}

func splitLogTimestamp(line string) (time.Time, string, string, bool) {
	line = strings.TrimRight(line, "\r")
	space := strings.IndexByte(line, ' ')
	if space <= 0 {
		return time.Time{}, "", line, false
	}
	token := line[:space]
	stamp, err := time.Parse(time.RFC3339Nano, token)
	if err != nil {
		stamp, err = time.Parse(time.RFC3339, token)
		if err != nil {
			return time.Time{}, "", line, false
		}
	}
	return stamp, token, strings.TrimLeft(line[space+1:], " "), true
}

func (a *App) trimLiveLogEntries() {
	if a.logRange != logRangeLive || len(a.logEntries) <= maxLiveLogEntries {
		return
	}
	drop := len(a.logEntries) - maxLiveLogEntries
	copy(a.logEntries, a.logEntries[drop:])
	a.logEntries = a.logEntries[:maxLiveLogEntries]
	a.logEntrySeen = make(map[string]struct{}, len(a.logEntries))
	for _, entry := range a.logEntries {
		a.logEntrySeen[entry.UniqueKey] = struct{}{}
	}
}

func (a *App) handleLogsResult(msg logsResultMsg) tea.Cmd {
	if msg.Token != a.logRequestToken {
		return nil
	}
	a.activity = ""
	a.logLoading = false
	if msg.Err != nil {
		a.invalidateLogsRenderCache()
		a.statusMessage = msg.Err.Error()
		return nil
	}
	if msg.Replace {
		a.logEntries = append(a.logEntries[:0], msg.Entries...)
		a.logEntrySeen = make(map[string]struct{}, len(msg.Entries))
		for _, entry := range msg.Entries {
			a.logEntrySeen[entry.UniqueKey] = struct{}{}
		}
	} else {
		incoming := make([]logEntry, 0, len(msg.Entries))
		if a.logEntrySeen == nil {
			a.logEntrySeen = make(map[string]struct{}, len(a.logEntries)+len(msg.Entries))
			for _, entry := range a.logEntries {
				a.logEntrySeen[entry.UniqueKey] = struct{}{}
			}
		}
		for _, entry := range msg.Entries {
			if _, ok := a.logEntrySeen[entry.UniqueKey]; ok {
				continue
			}
			a.logEntrySeen[entry.UniqueKey] = struct{}{}
			incoming = append(incoming, entry)
		}
		a.logEntries = mergeSortedLogEntries(a.logEntries, incoming)
	}
	a.trimLiveLogEntries()
	a.logEntriesVersion++
	a.invalidateLogsRenderCache()
	if msg.Cursor.After(a.logCursor) {
		a.logCursor = msg.Cursor
	}
	a.logFetchedAt = time.Now()
	return nil
}

func (a *App) handleNodeLogPickerResult(msg nodeLogPickerResultMsg) tea.Cmd {
	if msg.Token != a.nodeLogPickerToken {
		return nil
	}
	a.activity = ""
	if a.screen != screenResourceDetails || a.activeResource.Resource != "nodes" || a.activeResource.APIGroup != "" || a.activeNode.Row.Key != msg.Key {
		return nil
	}
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return nil
	}
	return a.openActionPicker(msg.Title, "enter select  esc cancel", pendingActionNodeLogsPath, buildNodeLogPickerOptions(msg.Entries))
}

func (a *App) filteredLogEntries() []logEntry {
	query := strings.TrimSpace(a.logFilterQuery)
	if query == "" {
		return a.logEntries
	}
	filtered := make([]logEntry, 0, len(a.logEntries))
	for _, entry := range a.logEntries {
		candidate := strings.Join([]string{entry.TimestampText, entry.SourceLabel, entry.Message}, " ")
		if _, ok := scoreSearchCandidate(candidate, query); ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (a *App) renderLogs() string {
	width := max(24, a.width-4)
	if a.logRenderedVersion == a.logEntriesVersion && a.logRenderedFilter == a.logFilterQuery && a.logRenderedWidth == width && a.logRenderedWrap == a.logWrap && a.logRenderedTimestamps == a.logShowTimestamps {
		return a.logRenderedContent
	}

	entries := a.filteredLogEntries()
	var content string
	if len(entries) == 0 {
		switch {
		case strings.TrimSpace(a.logFilterQuery) != "":
			content = "No log lines match filter."
		case a.logLoading:
			content = "Loading logs…"
		default:
			content = "No log output."
		}
	} else {
		parts := make([]string, 0, len(entries))
		for _, entry := range entries {
			parts = append(parts, a.renderLogEntry(entry, width))
		}
		content = strings.Join(parts, "\n")
	}

	a.logRenderedVersion = a.logEntriesVersion
	a.logRenderedFilter = a.logFilterQuery
	a.logRenderedWidth = width
	a.logRenderedWrap = a.logWrap
	a.logRenderedTimestamps = a.logShowTimestamps
	a.logRenderedContent = content
	return content
}

func (a *App) renderLogEntry(entry logEntry, width int) string {
	prefixParts := make([]string, 0, 2)
	prefixPlainParts := make([]string, 0, 2)
	if a.logShowTimestamps && entry.HasTimestamp {
		prefixParts = append(prefixParts, theme.Muted.Render(entry.TimestampText))
		prefixPlainParts = append(prefixPlainParts, entry.TimestampText)
	}
	if entry.SourceLabel != "" {
		prefixParts = append(prefixParts, logSourceStyle(entry.SourceKey).Render("["+entry.SourceLabel+"]"))
		prefixPlainParts = append(prefixPlainParts, "["+entry.SourceLabel+"]")
	}
	prefixStyled := strings.Join(prefixParts, " ")
	prefixPlain := strings.Join(prefixPlainParts, " ")
	body := renderLogBody(entry.Message, a.logWrap, max(16, width-len(prefixPlain)-2))
	return renderPrefixedLogBlock(prefixStyled, prefixPlain, body)
}

func renderPrefixedLogBlock(prefixStyled string, prefixPlain string, body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 {
		return prefixStyled
	}
	if prefixPlain == "" {
		return body
	}
	indent := strings.Repeat(" ", len([]rune(prefixPlain))+1)
	var b strings.Builder
	b.WriteString(prefixStyled)
	b.WriteByte(' ')
	b.WriteString(lines[0])
	for _, line := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(indent)
		b.WriteString(line)
	}
	return b.String()
}

func renderLogBody(message string, wrap bool, width int) string {
	trimmed := strings.TrimSpace(message)
	if rendered, ok := renderJSONLog(trimmed, wrap); ok {
		return rendered
	}
	if !wrap || width <= 0 {
		return message
	}
	return wrapLogText(message, width)
}

func renderJSONLog(message string, pretty bool) (string, bool) {
	value, ok := parseJSONLogValue(message)
	if !ok {
		return "", false
	}
	return renderJSONValue(value, pretty, 0), true
}

func parseJSONLogValue(message string) (interface{}, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, false
	}
	if !strings.HasPrefix(message, "{") && !strings.HasPrefix(message, "[") {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(message))
	decoder.UseNumber()
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	return value, true
}

func renderJSONValue(value interface{}, pretty bool, indent int) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if !pretty {
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strconv.Quote(key))+theme.Muted.Render(": ")+renderJSONValue(typed[key], false, indent+1))
			}
			return theme.Muted.Render("{") + strings.Join(parts, theme.Muted.Render(", ")) + theme.Muted.Render("}")
		}
		pad := strings.Repeat("  ", indent)
		nextPad := strings.Repeat("  ", indent+1)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, nextPad+lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(strconv.Quote(key))+theme.Muted.Render(": ")+renderJSONValue(typed[key], true, indent+1))
		}
		return theme.Muted.Render("{") + "\n" + strings.Join(parts, theme.Muted.Render(",\n")) + "\n" + pad + theme.Muted.Render("}")
	case []interface{}:
		if !pretty {
			parts := make([]string, 0, len(typed))
			for _, item := range typed {
				parts = append(parts, renderJSONValue(item, false, indent+1))
			}
			return theme.Muted.Render("[") + strings.Join(parts, theme.Muted.Render(", ")) + theme.Muted.Render("]")
		}
		pad := strings.Repeat("  ", indent)
		nextPad := strings.Repeat("  ", indent+1)
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, nextPad+renderJSONValue(item, true, indent+1))
		}
		return theme.Muted.Render("[") + "\n" + strings.Join(parts, theme.Muted.Render(",\n")) + "\n" + pad + theme.Muted.Render("]")
	case string:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(strconv.Quote(typed))
	case json.Number:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(typed.String())
	case float64:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(formatJSONNumber(typed))
	case bool:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Render(strconv.FormatBool(typed))
	case nil:
		return theme.Muted.Render("null")
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(fmt.Sprintf("%v", typed))
	}
}

func formatJSONNumber(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func wrapLogText(text string, width int) string {
	if width <= 0 {
		return text
	}
	paragraphs := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	wrapped := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		if len([]rune(paragraph)) <= width {
			wrapped = append(wrapped, paragraph)
			continue
		}
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			wrapped = append(wrapped, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			candidate := line + " " + word
			if len([]rune(candidate)) <= width {
				line = candidate
				continue
			}
			wrapped = append(wrapped, line)
			line = word
		}
		wrapped = append(wrapped, line)
	}
	return strings.Join(wrapped, "\n")
}

func logSourceStyle(key string) lipgloss.Style {
	palette := []string{"14", "10", "11", "13", "9", "12"}
	sum := 0
	for _, r := range key {
		sum += int(r)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(palette[sum%len(palette)])).Bold(true)
}

func (a *App) logFooter() string {
	parts := []string{"j/k scroll", "pgup/pgdn page", "g/G edge", "/ fuzzy", "f exact", "p range", "t timestamps", "w wrap", "u refresh", "space pause", "y yank", "s save"}
	if a.logTarget == logTargetPod || a.logTarget == logTargetDeployment {
		parts = append(parts, "c containers")
	}
	parts = append(parts, "esc back")
	status := []string{strings.Join(parts, "  ")}
	status = append(status, "range:"+a.logRange.label())
	if a.logShowTimestamps {
		status = append(status, "ts:on")
	} else {
		status = append(status, "ts:off")
	}
	if a.logWrap {
		status = append(status, "wrap:on")
	} else {
		status = append(status, "wrap:off")
	}
	if a.logAutoRefreshPaused {
		status = append(status, "live:paused")
	} else {
		status = append(status, "live:on")
	}
	if a.logFetchedAt.IsZero() {
		if a.logLoading {
			status = append(status, "loading")
		}
		return strings.Join(status, "  · ")
	}
	age := time.Since(a.logFetchedAt)
	if age < 0 {
		age = 0
	}
	status = append(status, "updated "+formatOverviewAge(age)+" ago")
	if a.logLoading {
		status = append(status, "refreshing")
	}
	return strings.Join(status, "  · ")
}

func (a *App) renderedLogsPlain() string {
	return strings.TrimSpace(ansi.Strip(a.renderLogs()))
}

func (a *App) yankLogs() tea.Cmd {
	value := a.renderedLogsPlain()
	if value == "" {
		a.statusMessage = "no logs to copy"
		return nil
	}
	if err := writeClipboard(value); err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.statusMessage = "logs copied"
	return nil
}

func (a *App) saveLogsToPath(path string) tea.Cmd {
	value := a.renderedLogsPlain()
	if value == "" {
		a.statusMessage = "no logs to save"
		return nil
	}
	if err := writeTextFile(path, []byte(value+"\n"), 0o644); err != nil {
		a.statusMessage = err.Error()
		return nil
	}
	a.statusMessage = "logs saved to " + path
	return nil
}
