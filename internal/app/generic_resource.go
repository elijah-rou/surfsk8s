package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/elijahrou/surfsk8s/internal/cluster"
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

func (a *App) genericNeedsFetch(_ time.Time) bool {
	if a.genericRowsResourceID != a.activeResource.ID {
		return true
	}
	if a.lastGenericFetchAt.IsZero() {
		return true
	}
	return a.manager.Version() != a.lastManagerVersion
}

func (a *App) loadGenericResourceRows(now time.Time) {
	rows, err := a.manager.ListGenericResource(context.Background(), a.activeResource)
	if err != nil {
		a.statusMessage = err.Error()
	}
	if len(rows) != 0 {
		a.genericRows = rows
		a.genericNamespaces = clusterNamespaces(rows)
	} else if a.genericRowsResourceID != a.activeResource.ID {
		a.genericRows = nil
		a.genericNamespaces = nil
	}
	a.genericRowsResourceID = a.activeResource.ID
	a.lastGenericFetchAt = now
	a.genericPrinterValueCache = nil
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
	return a.activeResource.ID + "|" + a.contextScope + "|" + a.namespace + "|" + strings.TrimSpace(a.resourceQuery2) + "|" + a.tableSortCacheKey() + "|" + a.tableFilterCacheKey() + "|" + fmt.Sprintf("%d", a.manager.Version())
}

func (a *App) refreshGenericResourceList(now time.Time) {
	if a.genericNeedsFetch(now) {
		a.loadGenericResourceRows(now)
	}
	a.ensureGenericCompiledColumns()
	a.lastManagerVersion = a.manager.Version()
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
		a.totalRows = len(a.genericRows)
	}
	a.resourceTable.SetColumns(a.genericView.Columns(a.activeResource))
	a.resourceTable.SetEmptyMessage(a.emptyMessageFor(a.activeResource.Resource))
	a.resourceTable.SetWindowProvider(a.visibleRows, func(start int, end int) [][]string {
		return a.genericTableRows(start, end-start, time.Now())
	})
}

func (a *App) refreshGenericResourceDetails(now time.Time) {
	if a.activeGenericDetails.Row.Name == "" {
		return
	}
	if a.manager.Version() == a.lastManagerVersion {
		a.activeGenericDetails.Row = a.activeGenericDetails.Row.WithAge(now)
		a.lastTick = now
		return
	}
	details, err := a.manager.GenericResourceDetails(context.Background(), a.activeResource, a.activeGenericDetails.Row.Key, now)
	if err != nil {
		a.statusMessage = err.Error()
		return
	}
	a.activeGenericDetails = details
	a.lastManagerVersion = a.manager.Version()
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
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = now
	a.screen = screenResourceDetails
	a.resetTextViewport()
	return true
}

func (a *App) renderGenericResourceDetails() string {
	details := a.activeGenericDetails
	if details.Object == nil {
		return "resource disappeared"
	}
	sections := []string{
		fmt.Sprintf("Name:        %s", details.Row.Name),
		fmt.Sprintf("Namespace:   %s", defaultString(details.Row.Namespace, "cluster")),
		fmt.Sprintf("Cluster:     %s", details.Row.Cluster),
		fmt.Sprintf("Kind:        %s", a.activeResource.Kind),
		fmt.Sprintf("API group:   %s", defaultString(a.activeResource.APIGroup, "core")),
		fmt.Sprintf("API version: %s", defaultString(a.activeResource.Version, "server-default")),
		fmt.Sprintf("Resource:    %s", a.activeResource.Resource),
		fmt.Sprintf("Ready:       %s", defaultString(details.Row.Ready, "unknown")),
		fmt.Sprintf("Status:      %s", defaultString(details.Row.Status, "unknown")),
		fmt.Sprintf("Age:         %s", details.Row.Age),
		"",
		"YAML:",
		details.YAML,
	}
	return strings.Join(sections, "\n")
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
