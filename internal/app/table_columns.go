package app

import (
	"fmt"
	"time"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

const (
	minTableColumnWidth       = 4
	maxTableColumnWidth       = 80
	collapsedTableColumnWidth = 4
)

func loadTableColumnPreferences() map[string]map[string]tableColumnPreference {
	prefs, err := loadPreferences()
	if err != nil || len(prefs.TableColumns) == 0 {
		return nil
	}
	copied := make(map[string]map[string]tableColumnPreference, len(prefs.TableColumns))
	for tableKey, columns := range prefs.TableColumns {
		if len(columns) == 0 {
			continue
		}
		copied[tableKey] = make(map[string]tableColumnPreference, len(columns))
		for title, pref := range columns {
			copied[tableKey][title] = pref
		}
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}

func copyTableColumnPreferences(src map[string]map[string]tableColumnPreference) map[string]map[string]tableColumnPreference {
	if len(src) == 0 {
		return nil
	}
	copied := make(map[string]map[string]tableColumnPreference, len(src))
	for tableKey, columns := range src {
		if len(columns) == 0 {
			continue
		}
		copied[tableKey] = make(map[string]tableColumnPreference, len(columns))
		for title, pref := range columns {
			copied[tableKey][title] = pref
		}
	}
	if len(copied) == 0 {
		return nil
	}
	return copied
}

func cloneColumns(columns []components.Column) []components.Column {
	return append([]components.Column(nil), columns...)
}

func tableColumnKeyForResource(resource cluster.ResourceKind) string {
	if resource.Resource == "pods" && resource.APIGroup == "" {
		return "pods"
	}
	if resource.ID != "" {
		return resource.ID
	}
	return resource.APIGroup + "/" + resource.Resource
}

func (a *App) currentTableColumnKey() string {
	switch a.screen {
	case screenPods:
		return "pods"
	case screenResourceList:
		return tableColumnKeyForResource(a.activeResource)
	default:
		return ""
	}
}

func (a *App) currentPodColumns() []components.Column {
	return a.applyTableColumnPreferences("pods", a.podsView.Columns())
}

func (a *App) baseResourceColumns() []components.Column {
	switch {
	case a.activeResource.Resource == "deployments" && a.activeResource.APIGroup == "apps":
		return cloneColumns(a.deploymentsView.Columns())
	case a.activeResource.Resource == "services" && a.activeResource.APIGroup == "":
		return cloneColumns(a.servicesView.Columns())
	case a.activeResource.Resource == "nodes" && a.activeResource.APIGroup == "":
		return cloneColumns(a.nodesView.Columns())
	default:
		return cloneColumns(a.genericView.Columns(a.activeResource))
	}
}

func (a *App) applyTableColumnPreferences(tableKey string, base []components.Column) []components.Column {
	columns := cloneColumns(base)
	if tableKey == "" || len(columns) == 0 || len(a.tableColumnPreferences) == 0 {
		return columns
	}
	preferences := a.tableColumnPreferences[tableKey]
	if len(preferences) == 0 {
		return columns
	}
	for idx := range columns {
		pref, ok := preferences[columns[idx].Title]
		if !ok {
			continue
		}
		if pref.Collapsed {
			columns[idx].Width = collapsedTableColumnWidth
			continue
		}
		if pref.Width > 0 {
			columns[idx].Width = pref.Width
		}
		if columns[idx].Width < minTableColumnWidth {
			columns[idx].Width = minTableColumnWidth
		}
	}
	return columns
}

func (a *App) selectedTableColumnTarget() (string, string, int, int, bool) {
	switch a.screen {
	case screenPods:
		index := a.podTable.SelectedColumnIndex()
		columns := a.currentPodColumns()
		defaults := a.podsView.Columns()
		if index < 0 || index >= len(columns) || index >= len(defaults) {
			return "", "", 0, 0, false
		}
		return "pods", columns[index].Title, columns[index].Width, defaults[index].Width, true
	case screenResourceList:
		index := a.resourceTable.SelectedColumnIndex()
		columns := a.currentResourceColumns()
		defaults := a.baseResourceColumns()
		if index < 0 || index >= len(columns) || index >= len(defaults) {
			return "", "", 0, 0, false
		}
		return tableColumnKeyForResource(a.activeResource), columns[index].Title, columns[index].Width, defaults[index].Width, true
	default:
		return "", "", 0, 0, false
	}
}

func (a *App) saveTableColumnPreferences() {
	if err := updatePreferences(func(prefs *preferences) {
		prefs.TableColumns = copyTableColumnPreferences(a.tableColumnPreferences)
	}); err != nil {
		a.statusMessage = err.Error()
	}
}

func (a *App) setTableColumnPreference(tableKey string, title string, defaultWidth int, pref tableColumnPreference) {
	if tableKey == "" || title == "" {
		panic("app.setTableColumnPreference: empty tableKey/title")
	}
	if !pref.Collapsed && pref.Width == defaultWidth {
		pref.Width = 0
	}
	if !pref.Collapsed {
		pref.ExpandedWidth = 0
	}
	if a.tableColumnPreferences == nil {
		a.tableColumnPreferences = make(map[string]map[string]tableColumnPreference, 8)
	}
	entries := a.tableColumnPreferences[tableKey]
	if entries == nil {
		entries = make(map[string]tableColumnPreference, 8)
		a.tableColumnPreferences[tableKey] = entries
	}
	if !pref.Collapsed && pref.Width == 0 && pref.ExpandedWidth == 0 {
		delete(entries, title)
		if len(entries) == 0 {
			delete(a.tableColumnPreferences, tableKey)
		}
		a.saveTableColumnPreferences()
		return
	}
	entries[title] = pref
	a.saveTableColumnPreferences()
}

func clampTableColumnWidth(width int) int {
	if width < minTableColumnWidth {
		return minTableColumnWidth
	}
	if width > maxTableColumnWidth {
		return maxTableColumnWidth
	}
	return width
}

func (a *App) adjustSelectedTableColumnWidth(delta int) {
	if delta == 0 {
		panic("app.adjustSelectedTableColumnWidth: zero delta")
	}
	tableKey, title, currentWidth, defaultWidth, ok := a.selectedTableColumnTarget()
	if !ok {
		a.statusMessage = "no column selected"
		return
	}
	pref := a.tableColumnPreferences[tableKey][title]
	if pref.Collapsed {
		pref.Collapsed = false
		if pref.ExpandedWidth > 0 {
			currentWidth = pref.ExpandedWidth
		} else {
			currentWidth = defaultWidth
		}
	}
	newWidth := clampTableColumnWidth(currentWidth + delta)
	pref.Width = newWidth
	a.setTableColumnPreference(tableKey, title, defaultWidth, pref)
	a.statusMessage = fmt.Sprintf("%s width %d", title, newWidth)
	a.refreshCurrentScreen(time.Now())
}

func (a *App) toggleSelectedTableColumnCollapse() {
	tableKey, title, currentWidth, defaultWidth, ok := a.selectedTableColumnTarget()
	if !ok {
		a.statusMessage = "no column selected"
		return
	}
	pref := a.tableColumnPreferences[tableKey][title]
	if pref.Collapsed || currentWidth <= collapsedTableColumnWidth {
		pref.Collapsed = false
		if pref.ExpandedWidth > 0 {
			pref.Width = clampTableColumnWidth(pref.ExpandedWidth)
		} else {
			pref.Width = defaultWidth
		}
		a.setTableColumnPreference(tableKey, title, defaultWidth, pref)
		a.statusMessage = fmt.Sprintf("expanded %s", title)
		a.refreshCurrentScreen(time.Now())
		return
	}
	pref.Collapsed = true
	pref.ExpandedWidth = currentWidth
	pref.Width = collapsedTableColumnWidth
	a.setTableColumnPreference(tableKey, title, defaultWidth, pref)
	a.statusMessage = fmt.Sprintf("collapsed %s", title)
	a.refreshCurrentScreen(time.Now())
}
