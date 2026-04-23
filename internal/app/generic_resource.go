package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

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
	return a.manager.GenericResourceVersion(a.activeResource.ID)
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
	currentVersion, changes, ok := a.manager.GenericResourceDelta(a.activeResource.ID, a.lastManagerVersion)
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
		if newRow.Namespace != "" {
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

func (a *App) loadGenericResourceRows(now time.Time) {
	rows, err := a.manager.ListGenericResource(context.Background(), a.activeResource)
	if err != nil {
		a.statusMessage = err.Error()
	}
	if len(rows) != 0 {
		a.genericRows = rows
		a.genericRowsByKey = buildGenericRowIndex(rows)
		a.genericNamespaceCounts = buildGenericNamespaceCounts(rows)
		a.genericNamespaces = namespacesFromCounts(a.genericNamespaceCounts)
	} else if a.genericRowsResourceID != a.activeResource.ID {
		a.genericRows = nil
		a.genericRowsByKey = nil
		a.genericNamespaceCounts = nil
		a.genericNamespaces = nil
	}
	a.genericRowsResourceID = a.activeResource.ID
	a.lastGenericFetchAt = now
	a.genericPrinterValueCache = nil
	a.genericSearchTextCache = nil
	a.genericFilterValueCache = nil
	a.genericListCacheKey = ""
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

func (a *App) refreshGenericResourceList(now time.Time) {
	if a.genericNeedsFetch(now) {
		if !a.applyGenericResourceDelta(now) {
			a.loadGenericResourceRows(now)
		}
	}
	a.ensureGenericCompiledColumns()
	a.lastManagerVersion = a.genericResourceVersion()
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
	a.resourceTable.SetEmptyMessage(a.emptyMessageFor(a.activeResource.Resource))
	a.resourceTable.SetWindowProvider(a.visibleRows, func(start int, end int) [][]string {
		return a.genericTableRows(start, end-start, time.Now())
	})
}

func (a *App) refreshGenericResourceDetails(now time.Time) {
	if a.activeGenericDetails.Row.Name == "" {
		return
	}
	currentVersion := a.genericResourceVersion()
	if currentVersion == a.lastManagerVersion {
		a.activeGenericDetails.Row = a.activeGenericDetails.Row.WithAge(now)
		a.lastTick = now
		return
	}
	currentObjectVersion, ok := a.manager.GenericResourceObjectVersion(a.activeResource, a.activeGenericDetails.Row.Key)
	if ok && currentObjectVersion == a.activeGenericDetails.Row.ResourceVersion {
		a.activeGenericDetails.Row = a.activeGenericDetails.Row.WithAge(now)
		a.lastManagerVersion = currentVersion
		a.lastTick = now
		return
	}
	details, err := a.manager.GenericResourceDetails(context.Background(), a.activeResource, a.activeGenericDetails.Row.Key, now)
	if err != nil {
		a.statusMessage = err.Error()
		return
	}
	a.activeGenericDetails = details
	a.lastManagerVersion = currentVersion
	a.lastTick = now
	a.genericDetailFetchedAt = now
}

func (a *App) openCurrentGenericResourceSelection(index int, now time.Time) bool {
	row, ok := a.genericResourceRowAt(index, now)
	if !ok {
		return false
	}
	details, err := a.manager.GenericResourceDetails(context.Background(), a.activeResource, row.Key, now)
	if err != nil {
		a.statusMessage = err.Error()
		return false
	}
	a.activeGenericDetails = details
	a.genericDetailFetchedAt = now
	a.lastManagerVersion = a.genericResourceVersion()
	a.lastTick = now
	a.screen = screenResourceDetails
	a.resetTextViewport()
	return true
}

func (a *App) renderGenericResourceDetails() string {
	if rendered, ok := a.renderTypedGenericResourceDetails(); ok {
		return rendered
	}
	details := a.activeGenericDetails
	if details.Object == nil {
		return "resource disappeared"
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
