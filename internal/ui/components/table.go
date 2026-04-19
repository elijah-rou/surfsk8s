package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

type Column struct {
	Title      string
	Width      int
	AlignRight bool
}

// Table renders a virtual-scrolling table.
// Only visible rows are rendered. Handles 10k+ items without lag.
type Table struct {
	columns        []Column
	rows           [][]string
	windowProvider func(start int, end int) [][]string
	emptyMessage   string
	width          int
	height         int
	cursor         int
	offset         int
	rowCount       int
}

func NewTable(columns []Column) Table {
	if len(columns) == 0 {
		panic("components.NewTable: empty columns")
	}
	return Table{columns: columns, emptyMessage: "No items", height: 1}
}

func (t *Table) SetColumns(columns []Column) {
	if len(columns) == 0 {
		panic("components.Table.SetColumns: empty columns")
	}
	t.columns = columns
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

func (t *Table) View() string {
	if len(t.columns) == 0 {
		return ""
	}

	lines := make([]string, 0, t.visibleHeight()+1)
	lines = append(lines, theme.HeaderStyle.Render(t.renderRow(t.headerCells())))

	if t.rowCount == 0 {
		lines = append(lines, theme.Muted.Render(t.emptyMessage))
		return strings.Join(lines, "\n")
	}

	start, end := t.visibleRange()
	windowRows := t.windowRows(start, end)
	for idx := range windowRows {
		absoluteIndex := start + idx
		line := t.renderRow(windowRows[idx])
		if absoluteIndex == t.cursor {
			line = theme.SelectedRow.Render(line)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (t *Table) headerCells() []string {
	cells := make([]string, 0, len(t.columns))
	for _, column := range t.columns {
		cells = append(cells, column.Title)
	}
	return cells
}

func (t *Table) renderRow(cells []string) string {
	parts := make([]string, 0, len(t.columns))
	for idx, column := range t.columns {
		cell := ""
		if idx < len(cells) {
			cell = truncateCell(cells[idx], column.Width)
		}

		style := lipgloss.NewStyle().Width(column.Width).MaxWidth(column.Width)
		if column.AlignRight {
			style = style.Align(lipgloss.Right)
		}
		parts = append(parts, style.Render(cell))
	}
	return strings.Join(parts, " ")
}

func truncateCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func (t *Table) clampViewport() {
	if t.rowCount == 0 {
		t.cursor = 0
		t.offset = 0
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
}

func (t *Table) visibleHeight() int {
	visible := t.height - 1
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
