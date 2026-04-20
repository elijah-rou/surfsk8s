package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type tableSortCriterion struct {
	ID          int
	ColumnIndex int
	ColumnTitle string
	Desc        bool
	Enabled     bool
}

type tableSortColumnOption struct {
	ColumnIndex int
	ColumnTitle string
	Count       int
}

type tableSortDirectionOption struct {
	Label string
	Desc  bool
}

func fuzzyTableSortColumnOptions(options []tableSortColumnOption, query string) []tableSortColumnOption {
	if strings.TrimSpace(query) == "" {
		return append([]tableSortColumnOption(nil), options...)
	}
	type scoredOption struct {
		option tableSortColumnOption
		score  int
	}
	matched := make([]scoredOption, 0, len(options))
	for _, option := range options {
		score, ok := scoreSearchCandidate(option.ColumnTitle, query)
		if !ok {
			continue
		}
		matched = append(matched, scoredOption{option: option, score: score})
	}
	sort.SliceStable(matched, func(i int, j int) bool { return matched[i].score > matched[j].score })
	result := make([]tableSortColumnOption, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.option)
	}
	return result
}

func (a *App) tableSortBaseScreen() screen {
	switch a.screen {
	case screenTableSortColumnPicker, screenTableSortDirectionPicker, screenTableSortManager:
		return a.prevScreen
	default:
		return a.screen
	}
}

func (a *App) screenSupportsTableSorts() bool {
	base := a.tableSortBaseScreen()
	return base == screenPods || base == screenResourceList
}

func (a *App) currentTableSorts() []tableSortCriterion {
	switch a.tableSortBaseScreen() {
	case screenPods:
		return a.podTableSorts
	case screenResourceList:
		if a.resourceTableSorts == nil {
			return nil
		}
		return a.resourceTableSorts[a.activeResource.ID]
	default:
		return nil
	}
}

func (a *App) setCurrentTableSorts(criteria []tableSortCriterion) {
	switch a.tableSortBaseScreen() {
	case screenPods:
		a.podTableSorts = criteria
	case screenResourceList:
		if a.resourceTableSorts == nil {
			a.resourceTableSorts = make(map[string][]tableSortCriterion, 8)
		}
		a.resourceTableSorts[a.activeResource.ID] = criteria
	}
}

func (a *App) enabledTableSorts() []tableSortCriterion {
	criteria := a.currentTableSorts()
	result := make([]tableSortCriterion, 0, len(criteria))
	for _, criterion := range criteria {
		if criterion.Enabled {
			result = append(result, criterion)
		}
	}
	return result
}

func (a *App) tableSortLabel() string {
	criteria := a.currentTableSorts()
	var first string
	var firstDesc bool
	enabledCount := 0
	for _, c := range criteria {
		if !c.Enabled {
			continue
		}
		if enabledCount == 0 {
			first = strings.ToLower(c.ColumnTitle)
			firstDesc = c.Desc
		}
		enabledCount++
	}
	if enabledCount == 0 {
		return "sort:default"
	}
	if firstDesc {
		first += "↓"
	} else {
		first += "↑"
	}
	if enabledCount == 1 {
		return "sort:" + first
	}
	return fmt.Sprintf("sort:%s+%d", first, enabledCount-1)
}

func (a *App) tableSortCacheKey() string {
	criteria := a.currentTableSorts()
	parts := make([]string, 0, len(criteria))
	for _, criterion := range criteria {
		parts = append(parts, fmt.Sprintf("%d:%t:%t", criterion.ColumnIndex, criterion.Desc, criterion.Enabled))
	}
	return strings.Join(parts, "|")
}

func (a *App) hasAnyEnabledTableSorts() bool {
	for _, criterion := range a.currentTableSorts() {
		if criterion.Enabled {
			return true
		}
	}
	return false
}

func (a *App) tableSortNeedsMaterializedSort() bool {
	return a.hasAnyEnabledTableSorts()
}

func (a *App) openTableSortColumnPicker() tea.Cmd {
	if !a.screenSupportsTableSorts() {
		a.statusMessage = "column sorts unavailable here"
		return nil
	}
	a.prevScreen = a.screen
	a.screen = screenTableSortColumnPicker
	a.inputMode = inputModeSearch
	a.tableSortColumnQuery = ""
	a.filter.SetPrompt("o> ")
	a.filter.SetPlaceholder("column")
	a.filter.SetValue("")
	a.filter.Activate()
	a.refreshTableSortColumnPicker()
	return nil
}

func (a *App) openTableSortManager() tea.Cmd {
	if !a.screenSupportsTableSorts() {
		a.statusMessage = "column sorts unavailable here"
		return nil
	}
	a.prevScreen = a.screen
	a.screen = screenTableSortManager
	a.refreshTableSortManager()
	return nil
}

func (a *App) refreshTableSortColumnPicker() {
	columns := a.currentTableColumns()
	criteria := a.currentTableSorts()
	counts := make(map[int]int, len(columns))
	for _, criterion := range criteria {
		counts[criterion.ColumnIndex]++
	}
	options := make([]tableSortColumnOption, 0, len(columns))
	for idx, column := range columns {
		options = append(options, tableSortColumnOption{ColumnIndex: idx, ColumnTitle: column.Title, Count: counts[idx]})
	}
	options = fuzzyTableSortColumnOptions(options, a.tableSortColumnQuery)
	rows := make([][]string, 0, len(options))
	for _, option := range options {
		rows = append(rows, []string{fmt.Sprintf("%s  (%d sorts)", option.ColumnTitle, option.Count)})
	}
	a.visibleTableSortColumns = options
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable("SORT COLUMNS", rows)
	a.navTable.MoveTop()
}

func (a *App) refreshTableSortDirectionPicker() {
	options := []tableSortDirectionOption{{Label: "ascending  (A→Z, 0→9)", Desc: false}, {Label: "descending  (Z→A, 9→0)", Desc: true}}
	rows := make([][]string, 0, len(options))
	for _, option := range options {
		rows = append(rows, []string{option.Label})
	}
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable("SORT DIRECTION", rows)
}

func (a *App) refreshTableSortManager() {
	criteria := a.currentTableSorts()
	rows := make([][]string, 0, len(criteria))
	for idx, criterion := range criteria {
		state := "on"
		if !criterion.Enabled {
			state = "off"
		}
		direction := "↑"
		if criterion.Desc {
			direction = "↓"
		}
		rows = append(rows, []string{fmt.Sprintf("[%s] %d. %s %s", state, idx+1, criterion.ColumnTitle, direction)})
	}
	a.visibleTableSorts = append(a.visibleTableSorts[:0], criteria...)
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable("SORTS", rows)
}

func (a *App) updateTableSortColumnPickerKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case "enter":
		index := a.navTable.SelectedIndex()
		if index < 0 || index >= len(a.visibleTableSortColumns) {
			a.statusMessage = "no column selected"
			return nil
		}
		option := a.visibleTableSortColumns[index]
		a.pendingSortColumnIndex = option.ColumnIndex
		a.pendingSortColumnTitle = option.ColumnTitle
		a.screen = screenTableSortDirectionPicker
		a.refreshTableSortDirectionPicker()
	case "esc", "backspace":
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	}
	return nil
}

func (a *App) updateTableSortDirectionPickerKeys(msg tea.KeyMsg) tea.Cmd {
	applyDirection := func(desc bool) tea.Cmd {
		criteria := append([]tableSortCriterion(nil), a.currentTableSorts()...)
		a.nextTableSortID++
		criteria = append(criteria, tableSortCriterion{ID: a.nextTableSortID, ColumnIndex: a.pendingSortColumnIndex, ColumnTitle: a.pendingSortColumnTitle, Desc: desc, Enabled: true})
		a.setCurrentTableSorts(criteria)
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
		return nil
	}

	switch msg.String() {
	case "a":
		return applyDirection(false)
	case "d":
		return applyDirection(true)
	case "enter":
		index := a.navTable.SelectedIndex()
		if index <= 0 {
			return applyDirection(false)
		}
		return applyDirection(true)
	case "esc", "backspace":
		a.screen = screenTableSortColumnPicker
		a.refreshTableSortColumnPicker()
	}
	return nil
}

func (a *App) updateTableSortManagerKeys(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		a.navTable.MoveDown(1)
	case "k", "up":
		a.navTable.MoveUp(1)
	case "g", "home":
		a.navTable.MoveTop()
	case "G", "end":
		a.navTable.MoveBottom()
	case " ", "enter":
		index := a.navTable.SelectedIndex()
		criteria := append([]tableSortCriterion(nil), a.currentTableSorts()...)
		if index < 0 || index >= len(criteria) {
			a.statusMessage = "no sort selected"
			return nil
		}
		criteria[index].Enabled = !criteria[index].Enabled
		a.setCurrentTableSorts(criteria)
		a.refreshTableSortManager()
	case "x", "backspace":
		index := a.navTable.SelectedIndex()
		criteria := append([]tableSortCriterion(nil), a.currentTableSorts()...)
		if index < 0 || index >= len(criteria) {
			a.statusMessage = "no sort selected"
			return nil
		}
		criteria = append(criteria[:index], criteria[index+1:]...)
		a.setCurrentTableSorts(criteria)
		a.refreshTableSortManager()
	case "K":
		index := a.navTable.SelectedIndex()
		criteria := append([]tableSortCriterion(nil), a.currentTableSorts()...)
		if index <= 0 || index >= len(criteria) {
			return nil
		}
		criteria[index-1], criteria[index] = criteria[index], criteria[index-1]
		a.setCurrentTableSorts(criteria)
		a.refreshTableSortManager()
		a.navTable.MoveUp(1)
	case "J":
		index := a.navTable.SelectedIndex()
		criteria := append([]tableSortCriterion(nil), a.currentTableSorts()...)
		if index < 0 || index >= len(criteria)-1 {
			return nil
		}
		criteria[index], criteria[index+1] = criteria[index+1], criteria[index]
		a.setCurrentTableSorts(criteria)
		a.refreshTableSortManager()
		a.navTable.MoveDown(1)
	case "esc":
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	}
	return nil
}
