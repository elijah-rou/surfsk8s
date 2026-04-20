package app

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

type tableColumnFilter struct {
	ID          int
	ColumnIndex int
	ColumnTitle string
	Query       string
	Enabled     bool
}

type tableFilterColumnOption struct {
	ColumnIndex int
	ColumnTitle string
	Count       int
}

func fuzzyTableFilterColumnOptions(options []tableFilterColumnOption, query string) []tableFilterColumnOption {
	if strings.TrimSpace(query) == "" {
		return append([]tableFilterColumnOption(nil), options...)
	}
	type scoredOption struct {
		option tableFilterColumnOption
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
	result := make([]tableFilterColumnOption, 0, len(matched))
	for _, item := range matched {
		result = append(result, item.option)
	}
	return result
}

func (a *App) tableFilterBaseScreen() screen {
	switch a.screen {
	case screenTableFilterColumnPicker, screenTableFilterManager, screenTableSortColumnPicker, screenTableSortDirectionPicker, screenTableSortManager:
		return a.prevScreen
	default:
		return a.screen
	}
}

func (a *App) screenSupportsColumnFilters() bool {
	base := a.tableFilterBaseScreen()
	return base == screenPods || base == screenResourceList
}

func (a *App) currentTableFilterKey() string {
	switch a.tableFilterBaseScreen() {
	case screenPods:
		return "pods"
	case screenResourceList:
		return a.activeResource.ID
	default:
		return ""
	}
}

func (a *App) currentTableColumns() []components.Column {
	switch a.tableFilterBaseScreen() {
	case screenPods:
		return a.podsView.Columns()
	case screenResourceList:
		return a.currentResourceColumns()
	default:
		return nil
	}
}

func (a *App) currentTableFilters() []tableColumnFilter {
	switch a.tableFilterBaseScreen() {
	case screenPods:
		return a.podColumnFilters
	case screenResourceList:
		if a.resourceColumnFilters == nil {
			return nil
		}
		return a.resourceColumnFilters[a.activeResource.ID]
	default:
		return nil
	}
}

func (a *App) setCurrentTableFilters(filters []tableColumnFilter) {
	switch a.tableFilterBaseScreen() {
	case screenPods:
		a.podColumnFilters = filters
	case screenResourceList:
		if a.resourceColumnFilters == nil {
			a.resourceColumnFilters = make(map[string][]tableColumnFilter, 8)
		}
		a.resourceColumnFilters[a.activeResource.ID] = filters
	}
}

func (a *App) tableFilterStats() (int, int) {
	filters := a.currentTableFilters()
	enabled := 0
	for _, filter := range filters {
		if filter.Enabled {
			enabled++
		}
	}
	return enabled, len(filters)
}

func (a *App) tableFilterFooter() string {
	enabled, total := a.tableFilterStats()
	if total == 0 {
		return "filters:0"
	}
	var b strings.Builder
	b.Grow(24)
	b.WriteString("filters:")
	b.WriteString(strconv.Itoa(enabled))
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(total))
	return b.String()
}

func (a *App) tableFilterCacheKey() string {
	filters := a.currentTableFilters()
	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		parts = append(parts, fmt.Sprintf("%d:%s:%t", filter.ColumnIndex, filter.Query, filter.Enabled))
	}
	return strings.Join(parts, "|")
}

func (a *App) openTableFilterColumnPicker() tea.Cmd {
	if !a.screenSupportsColumnFilters() {
		a.statusMessage = "column filters unavailable here"
		return nil
	}
	a.prevScreen = a.screen
	a.screen = screenTableFilterColumnPicker
	a.inputMode = inputModeSearch
	a.tableFilterColumnQuery = ""
	a.filter.SetPrompt("f> ")
	a.filter.SetPlaceholder("column")
	a.filter.SetValue("")
	a.filter.Activate()
	a.refreshTableFilterColumnPicker()
	return nil
}

func (a *App) openTableFilterManager() tea.Cmd {
	if !a.screenSupportsColumnFilters() {
		a.statusMessage = "column filters unavailable here"
		return nil
	}
	a.prevScreen = a.screen
	a.screen = screenTableFilterManager
	a.refreshTableFilterManager()
	return nil
}

func (a *App) refreshTableFilterColumnPicker() {
	columns := a.currentTableColumns()
	filters := a.currentTableFilters()
	counts := make(map[int]int, len(columns))
	for _, filter := range filters {
		counts[filter.ColumnIndex]++
	}
	options := make([]tableFilterColumnOption, 0, len(columns))
	for idx, column := range columns {
		options = append(options, tableFilterColumnOption{ColumnIndex: idx, ColumnTitle: column.Title, Count: counts[idx]})
	}
	options = fuzzyTableFilterColumnOptions(options, a.tableFilterColumnQuery)
	rows := make([][]string, 0, len(options))
	for _, option := range options {
		rows = append(rows, []string{fmt.Sprintf("%s  (%d filters)", option.ColumnTitle, option.Count)})
	}
	a.visibleTableFilterColumns = options
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable("FILTER COLUMNS", rows)
	a.navTable.MoveTop()
}

func (a *App) refreshTableFilterManager() {
	filters := a.currentTableFilters()
	rows := make([][]string, 0, len(filters))
	for _, filter := range filters {
		state := "on"
		if !filter.Enabled {
			state = "off"
		}
		rows = append(rows, []string{fmt.Sprintf("[%s] %s = %s", state, filter.ColumnTitle, filter.Query)})
	}
	a.visibleTableFilters = append(a.visibleTableFilters[:0], filters...)
	a.visibleRows = len(rows)
	a.totalRows = len(rows)
	a.setNavTable("FILTERS", rows)
}

func (a *App) openTableFilterValuePrompt(columnIndex int, columnTitle string) tea.Cmd {
	a.pendingFilterColumnIndex = columnIndex
	a.pendingFilterColumnTitle = columnTitle
	a.screen = screenTableFilterColumnPicker
	a.inputMode = inputModeTableFilterValue
	a.filter.SetPrompt("f:" + strings.ToLower(columnTitle) + "> ")
	a.filter.SetPlaceholder("contains, !contains, =exact, !=exact, *wildcard*, !*wildcard*")
	a.filter.SetValue("")
	a.filter.Activate()
	return nil
}

func (a *App) updateTableFilterColumnPickerKeys(msg tea.KeyMsg) tea.Cmd {
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
		if index < 0 || index >= len(a.visibleTableFilterColumns) {
			a.statusMessage = "no column selected"
			return nil
		}
		option := a.visibleTableFilterColumns[index]
		return a.openTableFilterValuePrompt(option.ColumnIndex, option.ColumnTitle)
	case "esc", "backspace":
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	}
	return nil
}

func (a *App) updateTableFilterManagerKeys(msg tea.KeyMsg) tea.Cmd {
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
		filters := append([]tableColumnFilter(nil), a.currentTableFilters()...)
		if index < 0 || index >= len(filters) {
			a.statusMessage = "no filter selected"
			return nil
		}
		filters[index].Enabled = !filters[index].Enabled
		a.setCurrentTableFilters(filters)
		a.refreshTableFilterManager()
	case "x", "backspace":
		index := a.navTable.SelectedIndex()
		filters := append([]tableColumnFilter(nil), a.currentTableFilters()...)
		if index < 0 || index >= len(filters) {
			a.statusMessage = "no filter selected"
			return nil
		}
		filters = append(filters[:index], filters[index+1:]...)
		a.setCurrentTableFilters(filters)
		a.refreshTableFilterManager()
	case "esc":
		a.screen = a.prevScreen
		a.refreshCurrentScreen(time.Now())
	}
	return nil
}

func (a *App) updateTableFilterValuePrompt(msg tea.Msg) tea.Cmd {
	switch typed := msg.(type) {
	case tea.KeyMsg:
		switch typed.String() {
		case "esc":
			a.filter.Clear()
			a.filter.Deactivate()
			a.inputMode = inputModeSearch
			a.refreshTableFilterColumnPicker()
			return nil
		case "enter":
			query := strings.TrimSpace(a.filter.Value())
			if query == "" {
				a.statusMessage = "filter cannot be empty"
				return nil
			}
			filters := append([]tableColumnFilter(nil), a.currentTableFilters()...)
			a.nextTableFilterID++
			filters = append(filters, tableColumnFilter{
				ID:          a.nextTableFilterID,
				ColumnIndex: a.pendingFilterColumnIndex,
				ColumnTitle: a.pendingFilterColumnTitle,
				Query:       query,
				Enabled:     true,
			})
			a.setCurrentTableFilters(filters)
			a.filter.Clear()
			a.filter.Deactivate()
			a.inputMode = inputModeSearch
			a.screen = a.prevScreen
			a.refreshCurrentScreen(time.Now())
			return nil
		}
	}
	return a.filter.Update(msg)
}

func matchesStructuredFilter(candidate string, query string) bool {
	candidate = strings.TrimSpace(strings.ToLower(candidate))
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	if strings.HasPrefix(query, "!=") {
		return candidate != strings.TrimSpace(strings.TrimPrefix(query, "!="))
	}
	if strings.HasPrefix(query, "!") {
		return !matchesStructuredFilter(candidate, strings.TrimSpace(strings.TrimPrefix(query, "!")))
	}
	if strings.HasPrefix(query, "=") {
		return candidate == strings.TrimSpace(strings.TrimPrefix(query, "="))
	}
	if strings.ContainsAny(query, "*?") {
		matched, err := path.Match(query, candidate)
		if err == nil && matched {
			return true
		}
		if !strings.HasPrefix(query, "*") {
			matched, err = path.Match("*"+query, candidate)
			if err == nil && matched {
				return true
			}
		}
		if !strings.HasSuffix(query, "*") {
			matched, err = path.Match(query+"*", candidate)
			if err == nil && matched {
				return true
			}
		}
		matched, err = path.Match("*"+query+"*", candidate)
		return err == nil && matched
	}
	return strings.Contains(candidate, query)
}

func hasAnyEnabledColumnFilters(filters []tableColumnFilter) bool {
	for _, filter := range filters {
		if filter.Enabled {
			return true
		}
	}
	return false
}

func columnFilterTouchesAge(columns []components.Column, filters []tableColumnFilter) bool {
	for _, filter := range filters {
		if !filter.Enabled {
			continue
		}
		if filter.ColumnIndex < 0 || filter.ColumnIndex >= len(columns) {
			continue
		}
		if normalizeSortTitle(columns[filter.ColumnIndex].Title) == "AGE" {
			return true
		}
	}
	return false
}

func matchesColumnFilters(cells []string, filters []tableColumnFilter) bool {
	for _, filter := range filters {
		if !filter.Enabled {
			continue
		}
		if filter.ColumnIndex < 0 || filter.ColumnIndex >= len(cells) {
			return false
		}
		if !matchesStructuredFilter(ansi.Strip(cells[filter.ColumnIndex]), filter.Query) {
			return false
		}
	}
	return true
}

func deploymentCells(row state.DeploymentRow) []string {
	return []string{row.Cluster, row.Namespace, row.Name, row.Ready, strconv.Itoa(int(row.UpToDate)), strconv.Itoa(int(row.Available)), row.Age}
}

func serviceCells(row state.ServiceRow) []string {
	return []string{row.Cluster, row.Namespace, row.Name, row.Type, row.ClusterIP, row.Ports, row.Age}
}

func (a *App) matchesPodColumnFilters(row state.PodRow, now time.Time) bool {
	if !hasAnyEnabledColumnFilters(a.podColumnFilters) {
		return true
	}
	columns := a.podsView.Columns()
	if columnFilterTouchesAge(columns, a.podColumnFilters) {
		return matchesColumnFilters(a.podCells(row.WithAge(now)), a.podColumnFilters)
	}
	return matchesColumnFilters(a.podCells(row), a.podColumnFilters)
}

func (a *App) matchesDeploymentColumnFilters(row state.DeploymentRow, now time.Time) bool {
	filters := a.currentTableFilters()
	if !hasAnyEnabledColumnFilters(filters) {
		return true
	}
	columns := a.deploymentsView.Columns()
	if columnFilterTouchesAge(columns, filters) {
		return matchesColumnFilters(deploymentCells(row.WithAge(now)), filters)
	}
	return matchesColumnFilters(deploymentCells(row), filters)
}

func (a *App) matchesServiceColumnFilters(row state.ServiceRow, now time.Time) bool {
	filters := a.currentTableFilters()
	if !hasAnyEnabledColumnFilters(filters) {
		return true
	}
	columns := a.servicesView.Columns()
	if columnFilterTouchesAge(columns, filters) {
		return matchesColumnFilters(serviceCells(row.WithAge(now)), filters)
	}
	return matchesColumnFilters(serviceCells(row), filters)
}

func (a *App) matchesNodeColumnFilters(row state.NodeRow, now time.Time) bool {
	filters := a.currentTableFilters()
	if !hasAnyEnabledColumnFilters(filters) {
		return true
	}
	columns := a.nodesView.Columns()
	if columnFilterTouchesAge(columns, filters) {
		return matchesColumnFilters(a.nodeCells(row.WithAge(now)), filters)
	}
	return matchesColumnFilters(a.nodeCells(row), filters)
}

func (a *App) matchesGenericColumnFilters(row cluster.GenericResourceRow, now time.Time) bool {
	filters := a.currentTableFilters()
	if !hasAnyEnabledColumnFilters(filters) {
		return true
	}
	columns := a.currentResourceColumns()
	for _, filter := range filters {
		if !filter.Enabled {
			continue
		}
		if filter.ColumnIndex < 0 || filter.ColumnIndex >= len(columns) {
			return false
		}
		value := a.genericCellValueAtNow(row, filter.ColumnIndex, now)
		if !matchesStructuredFilter(ansi.Strip(value), filter.Query) {
			return false
		}
	}
	return true
}

func (a *App) genericCells(row cluster.GenericResourceRow, now time.Time) []string {
	row = row.WithAge(now)
	values := make([]string, len(a.currentResourceColumns()))
	a.fillGenericCells(values, row, now)
	return values
}
