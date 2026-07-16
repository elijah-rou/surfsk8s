package app

import (
	"context"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

const genericRequestTimeout = 5 * time.Second

func genericResourceIdentity(resource cluster.ResourceKind) string {
	if resource.ID != "" {
		return resource.ID
	}
	if resource.APIGroup == "" {
		return "/" + resource.Resource
	}
	return resource.APIGroup + "/" + resource.Resource
}

type genericListResultMsg struct {
	Token      uint64
	ResourceID string
	Rows       []cluster.GenericResourceRow
	Err        error
}

type genericDetailResultMsg struct {
	Token      uint64
	ResourceID string
	Key        cluster.GenericResourceKey
	Details    cluster.GenericResourceDetails
	Err        error
	Open       bool
}

type genericActionKind uint8

const (
	genericActionEdit genericActionKind = iota + 1
	genericActionDelete
)

type genericActionResultMsg struct {
	Token      uint64
	ResourceID string
	Key        cluster.GenericResourceKey
	Kind       genericActionKind
	Details    cluster.GenericResourceDetails
	Err        error
	ReturnTo   screen
}

type resourceJumpResultMsg struct {
	Token       uint64
	Fingerprint string
	Target      resourceJumpTarget
	Details     cluster.GenericResourceDetails
	Err         error
	Resource    cluster.ResourceKind
}

type jumpDiscoveryKind uint8

const (
	jumpDiscoverOwners jumpDiscoveryKind = iota + 1
	jumpDiscoverChildren
)

type resourceJumpDiscoveryResultMsg struct {
	Token       uint64
	Fingerprint string
	Kind        jumpDiscoveryKind
	Targets     []resourceJumpTarget
	Err         error
}

func (a *App) supportsGenericResourceList(resource cluster.ResourceKind) bool {
	return resource.Resource != ""
}

func (a *App) genericResourceTitle() string {
	if a.activeResource.Custom && a.activeResource.APIGroup != "" {
		return strings.ToLower(a.activeResource.Display) + " · " + a.activeResource.APIGroup
	}
	return strings.ToLower(a.activeResource.Display)
}

func (a *App) genericResourceVersion() uint64 {
	return a.genericBackend.GenericResourceVersion(a.activeResource.ID)
}

func (a *App) genericNeedsFetch(_ time.Time) bool {
	if a.genericRowsResourceID != a.activeResource.ID {
		return true
	}
	if a.lastGenericFetchAt.IsZero() {
		return true
	}
	return a.genericResourceVersion() != a.lastManagerVersion
}

func buildGenericRowIndex(rows []cluster.GenericResourceRow) map[string]cluster.GenericResourceRow {
	index := make(map[string]cluster.GenericResourceRow, len(rows))
	for _, row := range rows {
		index[row.Key.String()] = row
	}
	return index
}

func buildGenericNamespaceCounts(rows []cluster.GenericResourceRow) map[string]int {
	counts := make(map[string]int, 16)
	for _, row := range rows {
		if row.Namespace == "" {
			continue
		}
		counts[row.Namespace]++
	}
	return counts
}

func namespacesFromCounts(counts map[string]int) []string {
	namespaces := make([]string, 0, len(counts))
	for namespace, count := range counts {
		if count <= 0 || namespace == "" {
			continue
		}
		namespaces = append(namespaces, namespace)
	}
	if len(namespaces) == 0 {
		return []string{""}
	}
	sort.Strings(namespaces)
	return namespaces
}

func (a *App) rebuildGenericVisibleKeySet() {
	a.genericVisibleKeySet = make(map[string]struct{}, len(a.sortedGenericRows))
	for _, row := range a.sortedGenericRows {
		a.genericVisibleKeySet[row.Key.String()] = struct{}{}
	}
}

func (a *App) genericSearchText(row cluster.GenericResourceRow) string {
	key := row.Key.String()
	if value, ok := a.genericSearchTextCache[key]; ok {
		return value
	}
	printerValues := row.PrinterValues
	if len(a.activeResource.PrinterColumns) != 0 {
		printerValues = a.genericPrinterValues(row)
	}
	row.PrinterValues = printerValues
	value := strings.ToLower(row.SearchText())
	if a.genericSearchTextCache == nil {
		a.genericSearchTextCache = make(map[string]string, 64)
	}
	a.genericSearchTextCache[key] = value
	return value
}

func (a *App) genericFilterValue(row cluster.GenericResourceRow, columnIndex int, now time.Time, columns []components.Column) string {
	if columnIndex < 0 || columnIndex >= len(columns) {
		return ""
	}
	if normalizeSortTitle(columns[columnIndex].Title) == "AGE" {
		return normalizeStructuredFilterValue(ansi.Strip(a.genericCellValueAtNow(row, columnIndex, now)))
	}
	rowKey := row.Key.String()
	if rowCache, ok := a.genericFilterValueCache[rowKey]; ok {
		if value, ok := rowCache[columnIndex]; ok {
			return value
		}
	}
	value := normalizeStructuredFilterValue(ansi.Strip(a.genericCellValueAt(row, columnIndex)))
	if a.genericFilterValueCache == nil {
		a.genericFilterValueCache = make(map[string]map[int]string, 64)
	}
	rowCache := a.genericFilterValueCache[rowKey]
	if rowCache == nil {
		rowCache = make(map[int]string, 4)
		a.genericFilterValueCache[rowKey] = rowCache
	}
	rowCache[columnIndex] = value
	return value
}

func (a *App) genericRowMatchesScopeWithColumns(row cluster.GenericResourceRow, now time.Time, columns []components.Column) bool {
	if !a.contextMatches(row.Cluster) {
		return false
	}
	if a.activeResource.Namespaced && a.namespace != "" && row.Namespace != a.namespace {
		return false
	}
	query := strings.TrimSpace(a.resourceQuery2)
	if query != "" {
		if _, ok := scoreSearchCandidateLower(a.genericSearchText(row), query); !ok {
			return false
		}
	}
	if !a.matchesGenericColumnFiltersWithColumns(row, now, columns) {
		return false
	}
	return true
}

func (a *App) genericRowMatchesScope(row cluster.GenericResourceRow, now time.Time) bool {
	return a.genericRowMatchesScopeWithColumns(row, now, a.currentResourceColumns())
}

func (a *App) applyGenericResourceDelta(now time.Time) bool {
	currentVersion, changes, ok := a.genericBackend.GenericResourceDelta(a.activeResource.ID, a.lastManagerVersion)
	if !ok {
		return false
	}
	if currentVersion == a.lastManagerVersion {
		return true
	}
	if a.genericRowsByKey == nil {
		a.genericRowsByKey = buildGenericRowIndex(a.genericRows)
	}
	if a.genericNamespaceCounts == nil {
		a.genericNamespaceCounts = buildGenericNamespaceCounts(a.genericRows)
	}
	rebuildVisible := false
	namespaceSetChanged := false
	columns := a.currentResourceColumns()
	for _, change := range changes {
		key := change.Key.String()
		delete(a.genericSearchTextCache, key)
		delete(a.genericFilterValueCache, key)
		oldRow, hadOld := a.genericRowsByKey[key]
		oldVisible := hadOld && a.genericRowMatchesScopeWithColumns(oldRow, now, columns)
		if change.Deleted {
			delete(a.genericRowsByKey, key)
			if oldRow.Namespace != "" && a.genericNamespaceCounts[oldRow.Namespace] > 0 {
				a.genericNamespaceCounts[oldRow.Namespace]--
				if a.genericNamespaceCounts[oldRow.Namespace] == 0 {
					delete(a.genericNamespaceCounts, oldRow.Namespace)
					namespaceSetChanged = true
				}
			}
			if oldVisible {
				rebuildVisible = true
			}
			continue
		}
		newRow := change.Row
		a.genericRowsByKey[key] = newRow
		if hadOld {
			if oldRow.Namespace != "" && oldRow.Namespace != newRow.Namespace && a.genericNamespaceCounts[oldRow.Namespace] > 0 {
				a.genericNamespaceCounts[oldRow.Namespace]--
				if a.genericNamespaceCounts[oldRow.Namespace] == 0 {
					delete(a.genericNamespaceCounts, oldRow.Namespace)
					namespaceSetChanged = true
				}
			}
		}
		if newRow.Namespace != "" && (!hadOld || oldRow.Namespace != newRow.Namespace) {
			if a.genericNamespaceCounts[newRow.Namespace] == 0 {
				namespaceSetChanged = true
			}
			a.genericNamespaceCounts[newRow.Namespace]++
		}
		newVisible := a.genericRowMatchesScopeWithColumns(newRow, now, columns)
		if oldVisible || newVisible {
			rebuildVisible = true
		}
	}
	if namespaceSetChanged {
		a.genericNamespaces = namespacesFromCounts(a.genericNamespaceCounts)
	}
	a.lastGenericFetchAt = now
	a.lastManagerVersion = currentVersion
	if rebuildVisible {
		a.genericRows = a.genericRows[:0]
		for _, row := range a.genericRowsByKey {
			a.genericRows = append(a.genericRows, row)
		}
		cluster.SortGenericResourceRows(a.genericRows)
		a.genericPrinterValueCache = nil
		a.genericSearchTextCache = nil
		a.genericFilterValueCache = nil
		a.genericListCacheKey = ""
	}
	return true
}

func (a *App) ensureGenericCompiledColumns() {
	if a.genericCompiledColumnsVersion == a.activeResource.ID {
		return
	}
	a.genericCompiledColumns = cluster.CompilePrinterColumns(a.activeResource.PrinterColumns)
	a.genericCompiledColumnsVersion = a.activeResource.ID
}

func (a *App) genericListCacheState() string {
	return a.activeResource.ID + "|" + a.contextScope + "|" + a.namespace + "|" + strings.TrimSpace(a.resourceQuery2) + "|" + a.tableSortCacheKey() + "|" + a.tableFilterCacheKey()
}

func (a *App) applyGenericListRows(rows []cluster.GenericResourceRow, now time.Time, resourceID string) {
	a.genericRows = rows
	if len(rows) == 0 {
		a.genericRowsByKey = nil
		a.genericNamespaceCounts = nil
		a.genericNamespaces = nil
	} else {
		a.genericRowsByKey = buildGenericRowIndex(rows)
		a.genericNamespaceCounts = buildGenericNamespaceCounts(rows)
		a.genericNamespaces = namespacesFromCounts(a.genericNamespaceCounts)
	}
	a.genericRowsResourceID = resourceID
	a.lastGenericFetchAt = now
	a.genericPrinterValueCache = nil
	a.genericSearchTextCache = nil
	a.genericFilterValueCache = nil
	a.genericListCacheKey = ""
}

func (a *App) fetchGenericResourceListCmd(resource cluster.ResourceKind) tea.Cmd {
	if resource.Resource == "" {
		panic("app.fetchGenericResourceListCmd: empty resource")
	}
	token := a.nextAsyncTokenValue()
	a.genericListToken = token
	a.genericListLoading = true
	a.activity = "loading " + strings.ToLower(resource.Display)
	backend := a.genericBackend
	rootCtx := a.context
	resourceID := genericResourceIdentity(resource)
	kind := resource
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(rootCtx, genericRequestTimeout)
		defer cancel()
		rows, err := backend.ListGenericResource(ctx, kind)
		return genericListResultMsg{Token: token, ResourceID: resourceID, Rows: rows, Err: err}
	}
}

func (a *App) handleGenericListResult(msg genericListResultMsg) tea.Cmd {
	if msg.Token != a.genericListToken {
		return nil
	}
	a.genericListLoading = false
	if a.activity == "loading "+strings.ToLower(a.activeResource.Display) {
		a.activity = ""
	}
	if a.screen != screenResourceList || genericResourceIdentity(a.activeResource) != msg.ResourceID {
		return nil
	}
	now := time.Now()
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return nil
	}
	a.applyGenericListRows(msg.Rows, now, msg.ResourceID)
	a.lastManagerVersion = a.genericResourceVersion()
	return a.renderGenericResourceList(now)
}

func (a *App) renderGenericResourceList(now time.Time) tea.Cmd {
	var selectedKey string
	if a.genericSelectionKey.Name != "" {
		selectedKey = a.genericSelectionKey.String()
	} else if idx := a.resourceTable.SelectedIndex(); idx >= 0 {
		if row, ok := a.genericResourceRowAt(idx, now); ok {
			selectedKey = row.Key.String()
			a.genericSelectionKey = row.Key
		}
	}
	a.ensureGenericCompiledColumns()
	a.lastTick = now
	if a.activeResource.Namespaced {
		a.namespaces = a.genericNamespaces
	} else {
		a.namespaces = []string{""}
	}
	cacheKey := a.genericListCacheState()
	if cacheKey != a.genericListCacheKey {
		total, filtered := a.buildSortedGenericResources()
		a.visibleRows = filtered
		a.totalRows = total
		a.genericListCacheKey = cacheKey
	} else {
		a.visibleRows = len(a.sortedGenericRows)
		a.totalRows = len(a.genericRowsByKey)
	}
	a.resourceTable.SetColumns(a.currentResourceColumns())
	empty := a.emptyMessageFor(a.activeResource.Resource)
	if a.genericListLoading && len(a.sortedGenericRows) == 0 {
		empty = "Loading…"
	}
	a.resourceTable.SetEmptyMessage(empty)
	a.resourceTable.SetWindowProvider(a.visibleRows, func(start int, end int) [][]string {
		return a.genericTableRows(start, end-start, time.Now())
	})
	a.restoreGenericSelection(selectedKey, now)
	return nil
}

func (a *App) restoreGenericSelection(selectedKey string, now time.Time) {
	if selectedKey == "" {
		return
	}
	for i, row := range a.sortedGenericRows {
		if row.Key.String() == selectedKey {
			a.resourceTable.SetCursor(i)
			a.genericSelectionKey = row.Key
			return
		}
	}
	if a.genericSelectionKey.Name == "" {
		return
	}
	if _, ok := a.genericRowsByKey[a.genericSelectionKey.String()]; ok {
		a.genericSelectionKey = cluster.GenericResourceKey{}
		if row, ok := a.genericResourceRowAt(a.resourceTable.SelectedIndex(), now); ok {
			a.genericSelectionKey = row.Key
		}
		return
	}
	// Object truly vanished from cache: retain sticky identity for vanished messaging.
}

func (a *App) refreshGenericResourceList(now time.Time) tea.Cmd {
	var fetch tea.Cmd
	if a.genericNeedsFetch(now) {
		if !a.applyGenericResourceDelta(now) {
			fetch = a.fetchGenericResourceListCmd(a.activeResource)
		}
	} else {
		a.lastManagerVersion = a.genericResourceVersion()
	}
	a.renderGenericResourceList(now)
	return fetch
}

func (a *App) fetchGenericResourceDetailsCmd(resource cluster.ResourceKind, key cluster.GenericResourceKey, open bool) tea.Cmd {
	if resource.Resource == "" {
		panic("app.fetchGenericResourceDetailsCmd: empty resource")
	}
	if key.Name == "" {
		panic("app.fetchGenericResourceDetailsCmd: empty key")
	}
	token := a.nextAsyncTokenValue()
	a.genericDetailToken = token
	a.genericDetailLoading = true
	a.activity = "loading details"
	backend := a.genericBackend
	rootCtx := a.context
	resourceID := genericResourceIdentity(resource)
	kind := resource
	capturedKey := key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(rootCtx, genericRequestTimeout)
		defer cancel()
		details, err := backend.GenericResourceDetails(ctx, kind, capturedKey, time.Now())
		return genericDetailResultMsg{Token: token, ResourceID: resourceID, Key: capturedKey, Details: details, Err: err, Open: open}
	}
}

func (a *App) handleGenericDetailResult(msg genericDetailResultMsg) tea.Cmd {
	if msg.Token != a.genericDetailToken {
		return nil
	}
	a.genericDetailLoading = false
	if a.activity == "loading details" {
		a.activity = ""
	}
	if genericResourceIdentity(a.activeResource) != msg.ResourceID {
		return nil
	}
	now := time.Now()
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return nil
	}
	if msg.Open {
		if a.screen != screenResourceList && a.screen != screenResourceDetails {
			return nil
		}
		a.activeGenericDetails = msg.Details
		a.genericDetailFetchedAt = now
		a.lastManagerVersion = a.genericResourceVersion()
		a.lastTick = now
		a.screen = screenResourceDetails
		a.resetTextViewport()
		return nil
	}
	if a.screen != screenResourceDetails || a.activeGenericDetails.Row.Key != msg.Key {
		return nil
	}
	a.activeGenericDetails = msg.Details
	a.genericDetailFetchedAt = now
	a.lastManagerVersion = a.genericResourceVersion()
	a.lastTick = now
	return nil
}

func (a *App) refreshGenericResourceDetails(now time.Time) tea.Cmd {
	if a.activeGenericDetails.Row.Name == "" {
		return nil
	}
	currentVersion := a.genericResourceVersion()
	if currentVersion == a.lastManagerVersion {
		a.activeGenericDetails.Row = a.activeGenericDetails.Row.WithAge(now)
		a.lastTick = now
		return nil
	}
	currentObjectVersion, ok := a.genericBackend.GenericResourceObjectVersion(a.activeResource, a.activeGenericDetails.Row.Key)
	if ok && currentObjectVersion == a.activeGenericDetails.Row.ResourceVersion {
		a.activeGenericDetails.Row = a.activeGenericDetails.Row.WithAge(now)
		a.lastManagerVersion = currentVersion
		a.lastTick = now
		return nil
	}
	return a.fetchGenericResourceDetailsCmd(a.activeResource, a.activeGenericDetails.Row.Key, false)
}

func (a *App) syncGenericSelectionFromCursor(now time.Time) {
	if row, ok := a.genericResourceRowAt(a.resourceTable.SelectedIndex(), now); ok {
		a.genericSelectionKey = row.Key
	}
}

func (a *App) resolveGenericSelectionKey(index int, now time.Time) (cluster.GenericResourceKey, bool) {
	if a.genericSelectionKey.Name != "" {
		want := a.genericSelectionKey.String()
		for _, row := range a.sortedGenericRows {
			if row.Key.String() == want {
				return a.genericSelectionKey, true
			}
		}
		if _, ok := a.genericRowsByKey[want]; !ok {
			return cluster.GenericResourceKey{}, false
		}
		// Sticky key left the visible snapshot via filter/scope; use highlighted row.
	}
	row, ok := a.genericResourceRowAt(index, now)
	if !ok {
		return cluster.GenericResourceKey{}, false
	}
	a.genericSelectionKey = row.Key
	return row.Key, true
}

func (a *App) genericVisibleOrCachedRow(key cluster.GenericResourceKey) (cluster.GenericResourceRow, bool) {
	want := key.String()
	for _, row := range a.sortedGenericRows {
		if row.Key.String() == want {
			return row, true
		}
	}
	row, ok := a.genericRowsByKey[want]
	return row, ok
}

func (a *App) openCurrentGenericResourceSelection(index int, now time.Time) tea.Cmd {
	key, ok := a.resolveGenericSelectionKey(index, now)
	if !ok {
		a.statusMessage = "resource vanished during refresh"
		return nil
	}
	a.genericDetailLoading = true
	a.statusMessage = ""
	return a.fetchGenericResourceDetailsCmd(a.activeResource, key, true)
}

func (a *App) fetchGenericActionRevalidateCmd(resource cluster.ResourceKind, key cluster.GenericResourceKey, kind genericActionKind, returnScreen screen) tea.Cmd {
	if resource.Resource == "" {
		panic("app.fetchGenericActionRevalidateCmd: empty resource")
	}
	if key.Name == "" {
		panic("app.fetchGenericActionRevalidateCmd: empty key")
	}
	token := a.nextAsyncTokenValue()
	a.genericActionToken = token
	a.activity = "validating resource"
	backend := a.genericBackend
	rootCtx := a.context
	resourceID := genericResourceIdentity(resource)
	captured := resource
	capturedKey := key
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(rootCtx, genericRequestTimeout)
		defer cancel()
		details, err := backend.GenericResourceDetails(ctx, captured, capturedKey, time.Now())
		return genericActionResultMsg{
			Token:      token,
			ResourceID: resourceID,
			Key:        capturedKey,
			Kind:       kind,
			Details:    details,
			Err:        err,
			ReturnTo:   returnScreen,
		}
	}
}

func (a *App) handleGenericActionResult(msg genericActionResultMsg) tea.Cmd {
	if msg.Token != a.genericActionToken {
		return nil
	}
	if a.activity == "validating resource" {
		a.activity = ""
	}
	if genericResourceIdentity(a.activeResource) != msg.ResourceID {
		return nil
	}
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return a.refreshCurrentScreen(time.Now())
	}
	a.activeGenericDetails = msg.Details
	switch msg.Kind {
	case genericActionEdit:
		cmd, description, err := a.executor.EditGenericResource(a.activeResource, msg.Details)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		return runProcessCommand(cmd, description)
	case genericActionDelete:
		cmd, description, err := a.executor.DeleteGenericResource(a.activeResource, msg.Details)
		if err != nil {
			a.statusMessage = err.Error()
			return nil
		}
		if msg.ReturnTo == screenResourceDetails {
			a.screen = screenResourceList
		}
		return runProcessCommand(cmd, description)
	default:
		a.statusMessage = "unsupported generic action"
		return nil
	}
}

func (a *App) handleResourceJumpResult(msg resourceJumpResultMsg) tea.Cmd {
	if msg.Token != a.genericJumpToken || msg.Fingerprint != a.genericJumpFingerprint {
		return nil
	}
	a.genericJumpLoading = false
	if a.activity == "loading jump target" || a.activity == "resolving jump targets" {
		a.activity = ""
	}
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return nil
	}
	now := time.Now()
	a.activeResource = msg.Resource
	a.activeGenericDetails = msg.Details
	a.genericDetailFetchedAt = now
	a.lastManagerVersion = a.genericResourceVersion()
	a.lastTick = now
	a.screen = screenResourceDetails
	a.detailFocus = detailFocusContent
	a.resetTextViewport()
	return a.maybeRefreshResourceUsageCmd(now)
}

func (a *App) handleResourceJumpDiscoveryResult(msg resourceJumpDiscoveryResultMsg) tea.Cmd {
	if msg.Token != a.genericJumpToken || msg.Fingerprint != a.genericJumpFingerprint {
		return nil
	}
	a.genericJumpLoading = false
	if a.activity == "resolving jump targets" {
		a.activity = ""
	}
	if msg.Err != nil {
		a.statusMessage = msg.Err.Error()
		return nil
	}
	if len(msg.Targets) == 0 {
		switch msg.Kind {
		case jumpDiscoverOwners:
			a.statusMessage = "no owner targets"
		case jumpDiscoverChildren:
			a.statusMessage = "no dependent targets"
		default:
			a.statusMessage = "no jump targets"
		}
		return nil
	}
	title := "select owner"
	if msg.Kind == jumpDiscoverChildren {
		title = "select dependent"
	}
	return a.openJumpTargets(title, msg.Targets, time.Now())
}

func (a *App) renderGenericResourceDetails() string {
	if rendered, ok := a.renderTypedGenericResourceDetails(); ok {
		return rendered
	}
	details := a.activeGenericDetails
	if details.Object == nil {
		return "resource disappeared"
	}
	if a.activeResource.Custom {
		return renderCustomResourceDetails(max(20, a.width-2), a.activeResource, details)
	}
	sections := []string{
		renderDetailFieldSection("Resource", []detailField{
			{Label: "Name", Value: details.Row.Name},
			{Label: "Namespace", Value: defaultString(details.Row.Namespace, "cluster")},
			{Label: "Cluster", Value: details.Row.Cluster},
			{Label: "Kind", Value: a.activeResource.Kind},
			{Label: "API group", Value: defaultString(a.activeResource.APIGroup, "core")},
			{Label: "API version", Value: defaultString(a.activeResource.Version, "server-default")},
			{Label: "Resource", Value: a.activeResource.Resource},
			{Label: "Ready", Value: defaultString(details.Row.Ready, "unknown")},
			{Label: "Status", Value: defaultString(details.Row.Status, "unknown")},
			{Label: "Age", Value: details.Row.Age},
		}),
		renderDetailSection("YAML", []string{details.YAML}),
	}
	return joinDetailSections(sections...)
}

func genericPrinterCacheKey(row cluster.GenericResourceRow) string {
	return row.Cluster + "\x00" + row.Namespace + "\x00" + row.Name + "\x00" + row.ResourceVersion
}

func (a *App) genericPrinterValues(row cluster.GenericResourceRow) []string {
	if len(a.activeResource.PrinterColumns) == 0 {
		return nil
	}
	if len(row.PrinterValues) != 0 {
		return row.PrinterValues
	}
	if row.Object == nil {
		return nil
	}
	if values, ok := a.genericPrinterValueCache[genericPrinterCacheKey(row)]; ok {
		return values
	}
	a.ensureGenericCompiledColumns()
	values := cluster.EvaluateCompiledPrinterColumns(row.Object, a.genericCompiledColumns)
	if a.genericPrinterValueCache == nil {
		a.genericPrinterValueCache = make(map[string][]string, 64)
	}
	a.genericPrinterValueCache[genericPrinterCacheKey(row)] = values
	return values
}

func (a *App) fillGenericCells(dst []string, row cluster.GenericResourceRow, now time.Time) {
	if len(dst) < len(a.currentResourceColumns()) {
		panic("app.fillGenericCells: short dst")
	}
	row = row.WithAge(now)
	idx := 0
	dst[idx] = row.Cluster
	idx++
	if a.activeResource.Namespaced {
		dst[idx] = row.Namespace
		idx++
	}
	dst[idx] = row.Name
	idx++
	if len(a.activeResource.PrinterColumns) != 0 {
		printerValues := a.genericPrinterValues(row)
		for _, value := range printerValues {
			dst[idx] = value
			idx++
		}
	} else {
		dst[idx] = row.Ready
		idx++
		dst[idx] = row.Status
		idx++
	}
	dst[idx] = row.Age
}

func (a *App) genericTableRows(start int, limit int, now time.Time) [][]string {
	if limit <= 0 || start >= len(a.sortedGenericRows) {
		return nil
	}
	end := start + limit
	if end > len(a.sortedGenericRows) {
		end = len(a.sortedGenericRows)
	}
	columnCount := len(a.currentResourceColumns())
	result := ensureRowCellBuffer(&a.genericTableRowBuf, end-start, columnCount)
	for i, row := range a.sortedGenericRows[start:end] {
		a.fillGenericCells(result[i], row, now)
	}
	return result
}

func (a *App) genericResourceRowAt(index int, now time.Time) (cluster.GenericResourceRow, bool) {
	if index < 0 || index >= len(a.sortedGenericRows) {
		return cluster.GenericResourceRow{}, false
	}
	row := a.sortedGenericRows[index].WithAge(now)
	if len(a.activeResource.PrinterColumns) != 0 {
		row.PrinterValues = a.genericPrinterValues(row)
	}
	return row, true
}

func clusterNamespaces(rows []cluster.GenericResourceRow) []string {
	seen := make(map[string]struct{}, 16)
	namespaces := make([]string, 0, 16)
	for _, row := range rows {
		if row.Namespace == "" {
			continue
		}
		if _, ok := seen[row.Namespace]; ok {
			continue
		}
		seen[row.Namespace] = struct{}{}
		namespaces = append(namespaces, row.Namespace)
	}
	if len(namespaces) == 0 {
		return []string{""}
	}
	sort.Strings(namespaces)
	return namespaces
}
