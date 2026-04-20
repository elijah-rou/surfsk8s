package app

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/cluster"
)

type resourceFinderItem struct {
	resource cluster.ResourceKind
	label    string
	search   string
}

func (a *App) openResourceFinder() tea.Cmd {
	a.prevScreen = a.screen
	a.screen = screenResourceFinder
	a.inputMode = inputModeResourceFinder
	a.resourceFinderQuery = ""
	a.filter.SetPrompt("r> ")
	a.filter.SetPlaceholder("resource")
	a.filter.SetValue("")
	a.filter.Activate()
	a.refreshResourceFinder()
	return nil
}

func (a *App) refreshResourceFinder() {
	a.lastManagerVersion = a.manager.Version()
	a.lastTick = time.Now()
	a.resourceFinderItems = buildResourceFinderItems(a.manager.Catalog())
	a.visibleResourceItems = fuzzyResourceFinderItems(a.resourceFinderItems, a.resourceFinderQuery)
	a.visibleRows = len(a.visibleResourceItems)
	a.totalRows = len(a.resourceFinderItems)
	a.setNavTable("RESOURCES", renderResourceFinderRows(a.visibleResourceItems))
}

func buildResourceFinderItems(groups []cluster.ResourceGroup) []resourceFinderItem {
	seen := make(map[string]struct{}, 64)
	items := make([]resourceFinderItem, 0, 64)
	for _, group := range groups {
		for _, resource := range group.Resources {
			if _, ok := seen[resource.ID]; ok {
				continue
			}
			seen[resource.ID] = struct{}{}
			label := resource.Display + "  (" + defaultString(resource.APIGroup, "core") + " · " + scopeAbbrev(resource.Namespaced) + ")"
			search := strings.Join([]string{resource.Display, resource.Kind, resource.Resource, resource.APIGroup, group.Name}, " ")
			items = append(items, resourceFinderItem{resource: resource, label: label, search: search})
		}
	}
	sort.SliceStable(items, func(i int, j int) bool {
		left := strings.ToLower(items[i].resource.Display + " " + items[i].resource.APIGroup)
		right := strings.ToLower(items[j].resource.Display + " " + items[j].resource.APIGroup)
		return left < right
	})
	return items
}

func fuzzyResourceFinderItems(items []resourceFinderItem, query string) []resourceFinderItem {
	if strings.TrimSpace(query) == "" {
		return append([]resourceFinderItem(nil), items...)
	}
	type scoredItem struct {
		item  resourceFinderItem
		score int
	}
	matched := make([]scoredItem, 0, len(items))
	for _, item := range items {
		score, ok := scoreSearchCandidate(item.search, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredItem{item: item, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]resourceFinderItem, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.item)
	}
	return result
}

func renderResourceFinderRows(items []resourceFinderItem) [][]string {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{item.label})
	}
	return rows
}

func scopeAbbrev(namespaced bool) string {
	if namespaced {
		return "ns"
	}
	return "cluster"
}

func (a *App) updateResourceFinderPrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			if strings.TrimSpace(a.resourceFinderQuery) != "" {
				a.resourceFinderQuery = ""
				a.filter.Clear()
				a.refreshResourceFinder()
				return nil
			}
			a.inputMode = inputModeSearch
			a.filter.Deactivate()
			a.screen = a.prevScreen
			a.refreshCurrentScreen(time.Now())
			return nil
		case "j", "down":
			a.navTable.MoveDown(1)
			return nil
		case "k", "up":
			a.navTable.MoveUp(1)
			return nil
		case "g", "home":
			a.navTable.MoveTop()
			return nil
		case "G", "end":
			a.navTable.MoveBottom()
			return nil
		case "enter":
			return a.openSelectedResourceFinderItem()
		}
	}
	cmd := a.filter.Update(msg)
	a.resourceFinderQuery = a.filter.Value()
	a.refreshResourceFinder()
	return cmd
}

func (a *App) updateResourceFinderKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case "esc", "backspace":
		if a.resourceFinderQuery != "" {
			a.resourceFinderQuery = ""
			a.refreshResourceFinder()
			return nil
		}
		a.inputMode = inputModeSearch
		a.filter.Deactivate()
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	case "enter":
		return a.openSelectedResourceFinderItem()
	}
	return nil
}

func (a *App) openSelectedResourceFinderItem() tea.Cmd {
	index := a.navTable.SelectedIndex()
	if index < 0 || index >= len(a.visibleResourceItems) {
		a.statusMessage = "resource selection disappeared"
		return nil
	}
	a.inputMode = inputModeSearch
	a.filter.Deactivate()
	resource := a.visibleResourceItems[index].resource
	a.openResourceList(resource)
	return nil
}
