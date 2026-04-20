package components

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

type Column struct {
	Title      string
	Width      int
	AlignRight bool
}

const (
	tableColumnSeparator = "│"
	tableHeaderJoin      = "┼"
	tableTopJoin         = "┬"
	tableDividerRune     = "─"
)

type TableVariant uint8

type TableSelectionMode uint8

const (
	TableVariantSimple TableVariant = iota
	TableVariantRich
)

const (
	TableSelectionCell TableSelectionMode = iota
	TableSelectionRow
)

// Table renders a virtual-scrolling table.
// Only visible rows are rendered. Handles 10k+ items without lag.
type Table struct {
	columns          []Column
	rows             [][]string
	windowProvider   func(start int, end int) [][]string
	emptyMessage     string
	width            int
	height           int
	cursor           int
	columnCursor     int
	columnOffset     int
	offset           int
	rowCount         int
	variant          TableVariant
	selectionMode    TableSelectionMode
	selectionActive  bool
	selectionRowFrom int
	selectionColFrom int
}

func NewTable(columns []Column) Table {
	if len(columns) == 0 {
		panic("components.NewTable: empty columns")
	}
	return Table{columns: columns, emptyMessage: "No items", height: 1, variant: TableVariantSimple, selectionMode: TableSelectionCell}
}

func (t *Table) SetVariant(variant TableVariant) {
	t.variant = variant
}

func (t *Table) SetSelectionMode(mode TableSelectionMode) {
	t.selectionMode = mode
}

func (t *Table) SelectionMode() TableSelectionMode {
	return t.selectionMode
}

func (t *Table) SelectionActive() bool {
	return t.selectionActive
}

func (t *Table) BeginCellSelection() {
	t.selectionMode = TableSelectionCell
	t.selectionActive = true
	t.selectionRowFrom = t.cursor
	t.selectionColFrom = t.columnCursor
}

func (t *Table) BeginRowSelection() {
	t.selectionMode = TableSelectionRow
	t.selectionActive = true
	t.selectionRowFrom = t.cursor
	t.selectionColFrom = 0
}

func (t *Table) ClearSelection() {
	t.selectionActive = false
	t.selectionMode = TableSelectionCell
}

func (t *Table) SetColumns(columns []Column) {
	if len(columns) == 0 {
		panic("components.Table.SetColumns: empty columns")
	}
	t.columns = columns
	t.clampColumnCursor()
	t.clampHorizontalViewport()
}

func (t *Table) SetEmptyMessage(message string) {
	if message == "" {
		panic("components.Table.SetEmptyMessage: empty message")
	}
	t.emptyMessage = message
}

func (t *Table) SetRows(rows [][]string) {
	t.windowProvider = nil
	t.rows = rows
	t.rowCount = len(rows)
	t.clampViewport()
}

func (t *Table) SetWindowProvider(rowCount int, provider func(start int, end int) [][]string) {
	if rowCount < 0 {
		panic("components.Table.SetWindowProvider: negative rowCount")
	}
	if provider == nil && rowCount != 0 {
		panic("components.Table.SetWindowProvider: nil provider")
	}
	t.rows = nil
	t.windowProvider = provider
	t.rowCount = rowCount
	t.clampViewport()
}

func (t *Table) SetSize(width int, height int) {
	if width < 0 {
		panic("components.Table.SetSize: negative width")
	}
	if height < 1 {
		height = 1
	}
	t.width = width
	t.height = height
	t.clampViewport()
	t.clampHorizontalViewport()
}

func (t *Table) MoveUp(steps int) {
	if steps < 0 {
		panic("components.Table.MoveUp: negative steps")
	}
	if t.rowCount == 0 {
		return
	}
	t.cursor -= steps
	if t.cursor < 0 {
		t.cursor = 0
	}
	t.clampViewport()
}

func (t *Table) MoveDown(steps int) {
	if steps < 0 {
		panic("components.Table.MoveDown: negative steps")
	}
	if t.rowCount == 0 {
		return
	}
	t.cursor += steps
	if t.cursor >= t.rowCount {
		t.cursor = t.rowCount - 1
	}
	t.clampViewport()
}

func (t *Table) MoveLeft(steps int) {
	if steps < 0 {
		panic("components.Table.MoveLeft: negative steps")
	}
	t.columnCursor -= steps
	if t.columnCursor < 0 {
		t.columnCursor = 0
	}
	t.clampHorizontalViewport()
}

func (t *Table) MoveRight(steps int) {
	if steps < 0 {
		panic("components.Table.MoveRight: negative steps")
	}
	t.columnCursor += steps
	t.clampColumnCursor()
	t.clampHorizontalViewport()
}

func (t *Table) MoveColumnStart() {
	if len(t.columns) == 0 {
		t.columnCursor = 0
		return
	}
	t.columnCursor = 0
	t.clampHorizontalViewport()
}

func (t *Table) MoveColumnEnd() {
	if len(t.columns) == 0 {
		t.columnCursor = 0
		return
	}
	t.columnCursor = len(t.columns) - 1
	t.clampHorizontalViewport()
}

func (t *Table) MoveTop() {
	if t.rowCount == 0 {
		return
	}
	t.cursor = 0
	t.clampViewport()
}

func (t *Table) MoveBottom() {
	if t.rowCount == 0 {
		return
	}
	t.cursor = t.rowCount - 1
	t.clampViewport()
}

func (t *Table) SelectedIndex() int {
	if t.rowCount == 0 {
		return -1
	}
	return t.cursor
}

func (t *Table) RowCount() int {
	return t.rowCount
}

func (t *Table) SelectedColumnIndex() int {
	if len(t.columns) == 0 {
		return -1
	}
	return t.columnCursor
}

func (t *Table) View() string {
	if len(t.columns) == 0 {
		return ""
	}

	header := t.renderHeader()
	lines := make([]string, 0, len(header)+t.visibleHeight())
	lines = append(lines, header...)

	if t.rowCount == 0 {
		lines = append(lines, theme.Muted.Render(t.emptyMessage))
		return strings.Join(lines, "\n")
	}

	start, end := t.visibleRange()
	windowRows := t.windowRows(start, end)
	for idx := range windowRows {
		absoluteIndex := start + idx
		lines = append(lines, t.renderDataRow(windowRows[idx], absoluteIndex == t.cursor))
	}

	return strings.Join(lines, "\n")
}

func (t *Table) renderHeader() []string {
	if t.variant == TableVariantSimple {
		return []string{headerStyle().Render(t.renderSimpleRow(t.headerCells(), false))}
	}
	start, end := t.visibleColumnRange()
	height := t.headerHeight()
	lines := make([]string, 0, height+1)
	for line := 0; line < height; line++ {
		cells := make([]string, 0, end-start)
		for _, column := range t.columns[start:end] {
			wrapped := wrapHeaderTitle(column.Title, column.Width)
			if line < len(wrapped) {
				cells = append(cells, wrapped[line])
				continue
			}
			cells = append(cells, "")
		}
		lines = append(lines, t.renderHeaderLine(cells, start))
	}
	lines = append(lines, t.renderDivider(start, end))
	return lines
}

func (t *Table) renderHeaderLine(cells []string, start int) string {
	end := start + len(cells)
	parts := make([]string, 0, len(cells)*2-1)
	for idx, column := range t.columns[start:end] {
		cell := cells[idx]
		style := headerStyle().Width(column.Width).MaxWidth(column.Width).Align(lipgloss.Center)
		parts = append(parts, style.Render(cell))
		if idx < len(cells)-1 {
			parts = append(parts, theme.TableDivider.Render(tableColumnSeparator))
		}
	}
	return strings.Join(parts, "")
}

func (t *Table) TopDivider() string {
	if len(t.columns) == 0 {
		return ""
	}
	start, end := t.visibleColumnRange()
	join := tableTopJoin
	if t.variant == TableVariantSimple {
		join = tableDividerRune
	}
	return t.renderColumnDivider(start, end, join)
}

func (t *Table) renderDivider(start int, end int) string {
	return t.renderColumnDivider(start, end, tableHeaderJoin)
}

func (t *Table) renderColumnDivider(start int, end int, join string) string {
	parts := make([]string, 0, (end-start)*2-1)
	for idx, column := range t.columns[start:end] {
		parts = append(parts, theme.TableDivider.Render(strings.Repeat(tableDividerRune, max(1, column.Width))))
		if idx < end-start-1 {
			parts = append(parts, theme.TableDivider.Render(join))
		}
	}
	return strings.Join(parts, "")
}

func (t *Table) renderDataRow(cells []string, selected bool) string {
	if t.variant == TableVariantSimple {
		return t.renderSimpleRow(cells, selected)
	}
	start, end := t.visibleColumnRange()
	parts := make([]string, 0, (end-start)*2-1)
	for idx, column := range t.columns[start:end] {
		absoluteColumn := start + idx
		cell := ""
		if absoluteColumn < len(cells) {
			cell = truncateCell(cells[absoluteColumn], column.Width)
		}

		style := lipgloss.NewStyle().Width(column.Width).MaxWidth(column.Width).Align(lipgloss.Left)
		if selected {
			style = style.Inherit(theme.SelectedRow.Copy())
		}
		styled := dataStyleForColumn(column.Title, cell).Inherit(style)
		parts = append(parts, styled.Render(cell))
		if idx < end-start-1 {
			divider := theme.TableDivider.Copy()
			if selected {
				divider = divider.Background(lipgloss.Color("236"))
			}
			parts = append(parts, divider.Render(tableColumnSeparator))
		}
	}
	return strings.Join(parts, "")
}

func (t *Table) headerCells() []string {
	cells := make([]string, 0, len(t.columns))
	for _, column := range t.columns {
		cells = append(cells, column.Title)
	}
	return cells
}

func (t *Table) renderSimpleRow(cells []string, selected bool) string {
	start, end := t.visibleColumnRange()
	parts := make([]string, 0, end-start)
	for idx, column := range t.columns[start:end] {
		absoluteColumn := start + idx
		cell := ""
		if absoluteColumn < len(cells) {
			cell = truncateCell(cells[absoluteColumn], column.Width)
		}
		style := lipgloss.NewStyle().Width(column.Width).MaxWidth(column.Width).Align(lipgloss.Left)
		if selected {
			style = style.Inherit(theme.SelectedRow.Copy())
		}
		parts = append(parts, style.Render(cell))
	}
	return strings.Join(parts, " ")
}

func truncateCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(value, width, "…")
}

func wrapHeaderTitle(title string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return []string{""}
	}
	tokens := strings.FieldsFunc(title, func(r rune) bool {
		return r == ' ' || r == '-' || r == '_'
	})
	if len(tokens) > 1 {
		lines := make([]string, 0, len(tokens))
		for _, token := range tokens {
			lines = append(lines, chunkHeaderToken(token, width)...)
		}
		return lines
	}
	return chunkHeaderToken(title, width)
}

func chunkHeaderToken(token string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if utf8.RuneCountInString(token) <= width {
		return []string{token}
	}
	runes := []rune(token)
	lines := make([]string, 0, (len(runes)+width-1)/width)
	for start := 0; start < len(runes); start += width {
		end := start + width
		if end > len(runes) {
			end = len(runes)
		}
		lines = append(lines, string(runes[start:end]))
	}
	return lines
}

func (t *Table) headerHeight() int {
	if t.variant == TableVariantSimple {
		return 1
	}
	start, end := t.visibleColumnRange()
	height := 1
	for _, column := range t.columns[start:end] {
		wrapped := wrapHeaderTitle(column.Title, column.Width)
		if len(wrapped) > height {
			height = len(wrapped)
		}
	}
	return height
}

func headerStyle() lipgloss.Style {
	return theme.HeaderStyle.Copy().Bold(true).Foreground(lipgloss.Color("15"))
}

func dataStyleForColumn(title string, value string) lipgloss.Style {
	normalizedTitle := normalizeColumnTitle(title)
	switch {
	case strings.Contains(normalizedTitle, "READY"):
		return semanticStatusStyle(value)
	case strings.Contains(normalizedTitle, "STATUS"):
		return semanticStatusStyle(value)
	case strings.Contains(normalizedTitle, "RESTART"):
		return restartStyle(value)
	case strings.Contains(normalizedTitle, "AGE"):
		return ageStyle(value)
	default:
		return lipgloss.NewStyle()
	}
}

func semanticStatusStyle(value string) lipgloss.Style {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if strings.Count(normalized, "/") == 1 {
		parts := strings.SplitN(normalized, "/", 2)
		left, leftErr := strconv.Atoi(parts[0])
		right, rightErr := strconv.Atoi(parts[1])
		if leftErr == nil && rightErr == nil {
			switch {
			case right == 0:
				return theme.Muted.Copy()
			case left >= right:
				return theme.StatusOK.Copy()
			case left > 0:
				return theme.StatusWarn.Copy()
			default:
				return theme.StatusError.Copy()
			}
		}
	}
	switch {
	case normalized == "", normalized == "unknown", normalized == "<none>":
		return theme.Muted.Copy()
	case normalized == "ready", normalized == "running", normalized == "succeeded", normalized == "completed", normalized == "true":
		return theme.StatusOK.Copy()
	case strings.Contains(normalized, "ready"), strings.Contains(normalized, "running"), strings.Contains(normalized, "healthy"), strings.Contains(normalized, "active"):
		return theme.StatusOK.Copy()
	case normalized == "pending", normalized == "terminating", normalized == "false":
		return theme.StatusWarn.Copy()
	case strings.Contains(normalized, "pending"), strings.Contains(normalized, "terminating"), strings.Contains(normalized, "creating"), strings.Contains(normalized, "unknown"):
		return theme.StatusWarn.Copy()
	case strings.Contains(normalized, "error"), strings.Contains(normalized, "fail"), strings.Contains(normalized, "backoff"), strings.Contains(normalized, "crash"), strings.Contains(normalized, "evicted"):
		return theme.StatusError.Copy()
	default:
		return lipgloss.NewStyle()
	}
}

func restartStyle(value string) lipgloss.Style {
	restarts, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		if strings.TrimSpace(value) == "" {
			return theme.Muted.Copy()
		}
		return lipgloss.NewStyle()
	}
	switch {
	case restarts <= 0:
		return theme.StatusOK.Copy()
	case restarts <= 3:
		return theme.StatusWarn.Copy()
	default:
		return theme.StatusError.Copy()
	}
}

func ageStyle(value string) lipgloss.Style {
	age, ok := parseAgeValue(value)
	if !ok {
		if strings.TrimSpace(value) == "" {
			return theme.Muted.Copy()
		}
		return lipgloss.NewStyle()
	}
	switch {
	case age < time.Hour:
		return theme.StatusOK.Copy()
	case age < 24*time.Hour:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	case age < 7*24*time.Hour:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	default:
		return theme.StatusError.Copy()
	}
}

func parseAgeValue(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	unit := value[len(value)-1]
	amount, err := strconv.Atoi(value[:len(value)-1])
	if err != nil {
		return 0, false
	}
	switch unit {
	case 's':
		return time.Duration(amount) * time.Second, true
	case 'm':
		return time.Duration(amount) * time.Minute, true
	case 'h':
		return time.Duration(amount) * time.Hour, true
	case 'd':
		return time.Duration(amount) * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func normalizeColumnTitle(title string) string {
	normalized := strings.TrimSpace(title)
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	return strings.ToUpper(normalized)
}

func (t *Table) rowWidth() int {
	start, end := t.visibleColumnRange()
	width := 0
	separatorWidth := utf8.RuneCountInString(tableColumnSeparator)
	if t.variant == TableVariantSimple {
		separatorWidth = 1
	}
	for idx, column := range t.columns[start:end] {
		width += max(1, column.Width)
		if idx < end-start-1 {
			width += separatorWidth
		}
	}
	return width
}

func (t *Table) clampColumnCursor() {
	if len(t.columns) == 0 {
		t.columnCursor = 0
		return
	}
	if t.columnCursor < 0 {
		t.columnCursor = 0
		return
	}
	if t.columnCursor >= len(t.columns) {
		t.columnCursor = len(t.columns) - 1
	}
}

func (t *Table) availableWidth() int {
	if t.width <= 0 {
		return 1 << 30
	}
	return t.width
}

func (t *Table) separatorWidth() int {
	if t.variant == TableVariantSimple {
		return 1
	}
	return utf8.RuneCountInString(tableColumnSeparator)
}

func (t *Table) columnSpanWidth(start int, end int) int {
	if end <= start {
		return 0
	}
	width := 0
	for idx := start; idx < end; idx++ {
		width += max(1, t.columns[idx].Width)
		if idx < end-1 {
			width += t.separatorWidth()
		}
	}
	return width
}

func (t *Table) visibleColumnRange() (int, int) {
	if len(t.columns) == 0 {
		return 0, 0
	}
	start := t.columnOffset
	if start < 0 {
		start = 0
	}
	if start >= len(t.columns) {
		start = len(t.columns) - 1
	}
	width := 0
	end := start
	for end < len(t.columns) {
		addition := max(1, t.columns[end].Width)
		if end > start {
			addition += t.separatorWidth()
		}
		if width+addition > t.availableWidth() && end > start {
			break
		}
		width += addition
		end++
	}
	if end <= start {
		end = start + 1
	}
	return start, end
}

func (t *Table) clampHorizontalViewport() {
	t.clampColumnCursor()
	if len(t.columns) == 0 {
		t.columnOffset = 0
		return
	}
	if t.columnOffset < 0 {
		t.columnOffset = 0
	}
	if t.columnOffset >= len(t.columns) {
		t.columnOffset = len(t.columns) - 1
	}
	start, end := t.visibleColumnRange()
	if t.columnCursor < start || t.columnCursor >= end {
		t.columnOffset = t.columnCursor
	}
	for t.columnOffset > 0 && t.columnSpanWidth(t.columnOffset-1, t.columnCursor+1) <= t.availableWidth() {
		t.columnOffset--
	}
}

func (t *Table) selectedRowRange() (int, int) {
	if !t.selectionActive {
		return t.cursor, t.cursor + 1
	}
	start := min(t.selectionRowFrom, t.cursor)
	end := max(t.selectionRowFrom, t.cursor) + 1
	return start, end
}

func (t *Table) selectedColumnRange() (int, int) {
	if !t.selectionActive {
		return t.columnCursor, t.columnCursor + 1
	}
	if t.selectionMode == TableSelectionRow {
		return 0, len(t.columns)
	}
	start := min(t.selectionColFrom, t.columnCursor)
	end := max(t.selectionColFrom, t.columnCursor) + 1
	return start, end
}

func (t *Table) SelectedRowRange() (int, int) {
	return t.selectedRowRange()
}

func (t *Table) SelectedColumnRange() (int, int) {
	return t.selectedColumnRange()
}

func (t *Table) rowSelected(rowIndex int) bool {
	if !t.selectionActive || t.selectionMode != TableSelectionRow {
		return false
	}
	start, end := t.selectedRowRange()
	return rowIndex >= start && rowIndex < end
}

func (t *Table) cellSelected(rowIndex int, columnIndex int) bool {
	if !t.selectionActive {
		return rowIndex == t.cursor && columnIndex == t.columnCursor
	}
	if t.selectionMode == TableSelectionRow {
		return false
	}
	rowStart := min(t.selectionRowFrom, t.cursor)
	rowEnd := max(t.selectionRowFrom, t.cursor)
	colStart := min(t.selectionColFrom, t.columnCursor)
	colEnd := max(t.selectionColFrom, t.columnCursor)
	return rowIndex >= rowStart && rowIndex <= rowEnd && columnIndex >= colStart && columnIndex <= colEnd
}

func (t *Table) clampViewport() {
	t.clampColumnCursor()
	if t.rowCount == 0 {
		t.cursor = 0
		t.offset = 0
		t.clampHorizontalViewport()
		return
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= t.rowCount {
		t.cursor = t.rowCount - 1
	}

	visible := t.visibleHeight()
	if visible <= 0 {
		visible = 1
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}

	maxOffset := t.rowCount - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
	t.clampHorizontalViewport()
}

func (t *Table) visibleHeight() int {
	visible := t.height - t.headerHeight()
	if t.variant == TableVariantRich {
		visible--
	}
	if visible < 1 {
		return 1
	}
	return visible
}

func (t *Table) visibleRange() (int, int) {
	start := t.offset
	end := start + t.visibleHeight()
	if end > t.rowCount {
		end = t.rowCount
	}
	return start, end
}

func (t *Table) windowRows(start int, end int) [][]string {
	if start < 0 || end < start {
		panic("components.Table.windowRows: invalid range")
	}
	if t.windowProvider != nil {
		return t.windowProvider(start, end)
	}
	return t.rows[start:end]
}

func (t *Table) Footer() string {
	if t.rowCount == 0 {
		return "0 rows"
	}
	start, end := t.visibleRange()
	return fmt.Sprintf("rows %d-%d/%d", start+1, end, t.rowCount)
}
