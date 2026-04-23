package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elijahrou/surfsk8s/internal/ui/theme"
)

type detailLayoutSpan uint8

const (
	detailLayoutSpanCompact detailLayoutSpan = iota
	detailLayoutSpanFull
)

type detailLayoutCard struct {
	Title    string
	Fields   []detailField
	Lines    []string
	Groups   []detailLayoutGroup
	Standout bool
	Span     detailLayoutSpan
}

type detailLayoutGroup struct {
	Title  string
	Fields []detailField
	Lines  []string
}

type detailLayoutBlock struct {
	Rendered string
	Span     detailLayoutSpan
}

func renderDetailCardLayout(width int, cards []detailLayoutCard) string {
	availableWidth := max(20, width)
	filtered := make([]detailLayoutCard, 0, len(cards))
	for _, card := range cards {
		if isEmptyDetailLayoutCard(card) {
			continue
		}
		filtered = append(filtered, card)
	}
	if len(filtered) == 0 {
		return ""
	}

	const gap = 2
	const minCompactWidth = 40
	maxColumns := min(3, max(1, (availableWidth+gap)/(minCompactWidth+gap)))

	sections := make([]string, 0, len(filtered))
	pending := make([]detailLayoutCard, 0, len(filtered))
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		columns := chooseDetailCardColumns(pending, maxColumns, availableWidth, gap)
		if columns == 1 {
			stacked := make([]string, 0, len(pending))
			for _, card := range pending {
				stacked = append(stacked, renderDetailLayoutCard(card, availableWidth))
			}
			sections = append(sections, strings.Join(stacked, "\n\n"))
			pending = pending[:0]
			return
		}
		columnWidth := max(24, (availableWidth-(columns-1)*gap)/columns)
		columnBodies, columnWidths := layoutDetailCards(pending, columns, columnWidth)
		parts := make([]string, 0, columns*2-1)
		for idx := 0; idx < columns; idx++ {
			if idx != 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			body := columnBodies[idx]
			width := columnWidth
			if body == "" {
				width = max(minCompactWidth, columnWidths[idx])
				body = strings.Repeat(" ", width)
			}
			parts = append(parts, lipgloss.NewStyle().Width(width).Render(body))
		}
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
		pending = pending[:0]
	}

	for _, card := range filtered {
		if card.Span == detailLayoutSpanFull {
			flushPending()
			sections = append(sections, renderDetailLayoutCard(card, availableWidth))
			continue
		}
		pending = append(pending, card)
	}
	flushPending()
	return strings.Join(sections, "\n\n")
}

func renderDetailLayoutCard(card detailLayoutCard, width int) string {
	style := detailCardStyle.Copy()
	if card.Standout {
		style = detailSummaryCardStyle.Copy()
	} else if card.Span == detailLayoutSpanCompact {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	}
	innerWidth := 0
	if width > 0 {
		if style.GetHorizontalFrameSize() > 0 {
			innerWidth = max(1, width-style.GetHorizontalFrameSize())
			style = style.Width(innerWidth)
		} else {
			innerWidth = max(1, width-style.GetHorizontalPadding())
			style = style.Width(innerWidth)
		}
	}
	content := renderDetailLayoutCardContent(card, innerWidth)
	return style.Render(content)
}

func renderDetailLayoutCardContent(card detailLayoutCard, width int) string {
	parts := make([]string, 0, 3)
	if len(card.Groups) != 0 {
		parts = append(parts, renderDetailGroupGrid(card.Groups, width))
	}
	if len(card.Fields) != 0 {
		parts = append(parts, renderDetailFieldGrid(card.Fields, width))
	}
	if lines := strings.TrimSpace(strings.Join(card.Lines, "\n")); lines != "" {
		parts = append(parts, lines)
	}
	body := strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "\n\n"))
	if strings.TrimSpace(card.Title) == "" {
		return strings.TrimSpace(body)
	}
	if strings.TrimSpace(body) == "" {
		return theme.HeaderStyle.Render(card.Title)
	}
	return strings.TrimSpace(theme.HeaderStyle.Render(card.Title) + "\n" + body)
}

func renderDetailGroupGrid(groups []detailLayoutGroup, width int) string {
	filtered := make([]detailLayoutGroup, 0, len(groups))
	for _, group := range groups {
		if isEmptyDetailLayoutGroup(group) {
			continue
		}
		filtered = append(filtered, group)
	}
	if len(filtered) == 0 {
		return ""
	}
	if width < 80 || len(filtered) == 1 {
		rendered := make([]string, 0, len(filtered))
		for _, group := range filtered {
			rendered = append(rendered, renderDetailLayoutGroup(group, width))
		}
		return strings.Join(rendered, "\n\n")
	}
	const gap = 4
	columns := min(4, len(filtered))
	cellWidth := max(24, (width-(columns-1)*gap)/columns)
	rows := make([]string, 0, (len(filtered)+columns-1)/columns)
	for start := 0; start < len(filtered); start += columns {
		end := min(len(filtered), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, lipgloss.NewStyle().Width(cellWidth).Render(renderDetailLayoutGroup(filtered[idx], cellWidth)))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return strings.Join(rows, "\n\n")
}

func renderDetailLayoutGroup(group detailLayoutGroup, width int) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(group.Title) != "" {
		parts = append(parts, theme.HeaderStyle.Render(group.Title))
	}
	if len(group.Fields) != 0 {
		parts = append(parts, renderDetailFieldGrid(group.Fields, width))
	}
	if lines := strings.TrimSpace(strings.Join(group.Lines, "\n")); lines != "" {
		parts = append(parts, lines)
	}
	return strings.TrimSpace(strings.Join(filterEmptyStrings(parts), "\n"))
}

func isEmptyDetailLayoutGroup(group detailLayoutGroup) bool {
	if strings.TrimSpace(group.Title) != "" && (len(group.Fields) != 0 || len(group.Lines) != 0) {
		return false
	}
	for _, field := range group.Fields {
		if strings.TrimSpace(field.Value) != "" {
			return false
		}
	}
	for _, line := range group.Lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return strings.TrimSpace(group.Title) == ""
}

func renderDetailFieldGrid(fields []detailField, width int) string {
	filtered := make([]detailField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		filtered = append(filtered, field)
	}
	if len(filtered) == 0 {
		return ""
	}
	if width < 72 || len(filtered) < 4 {
		return strings.Join(detailFieldLines(filtered), "\n")
	}
	const gap = 4
	columns := chooseDetailFieldColumns(len(filtered), width, gap)
	if columns <= 1 {
		return strings.Join(detailFieldLines(filtered), "\n")
	}
	cellWidth := max(16, (width-(columns-1)*gap)/columns)
	rows := make([]string, 0, (len(filtered)+columns-1)/columns)
	for start := 0; start < len(filtered); start += columns {
		end := min(len(filtered), start+columns)
		parts := make([]string, 0, (end-start)*2-1)
		for idx := start; idx < end; idx++ {
			if idx != start {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			parts = append(parts, lipgloss.NewStyle().Width(cellWidth).Render(detailFieldLines(filtered[idx : idx+1])[0]))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
	}
	return strings.Join(rows, "\n")
}

func chooseDetailFieldColumns(fieldCount int, width int, gap int) int {
	maxColumns := min(4, fieldCount)
	for columns := maxColumns; columns > 1; columns-- {
		cellWidth := (width - (columns-1)*gap) / columns
		if cellWidth >= 24 {
			return columns
		}
	}
	return 1
}

func renderDetailBlockLayout(width int, blocks []detailLayoutBlock) string {
	availableWidth := max(20, width)
	filtered := make([]detailLayoutBlock, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block.Rendered) == "" {
			continue
		}
		filtered = append(filtered, block)
	}
	if len(filtered) == 0 {
		return ""
	}

	const gap = 2
	const minCompactWidth = 40
	maxColumns := min(3, max(1, (availableWidth+gap)/(minCompactWidth+gap)))

	sections := make([]string, 0, len(filtered))
	pending := make([]detailLayoutBlock, 0, len(filtered))
	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		columns := chooseDetailBlockColumns(pending, maxColumns, availableWidth, gap)
		columnBodies, columnWidths := layoutDetailBlocks(pending, columns)
		parts := make([]string, 0, columns*2-1)
		for idx := 0; idx < columns; idx++ {
			if idx != 0 {
				parts = append(parts, strings.Repeat(" ", gap))
			}
			body := columnBodies[idx]
			width := columnWidths[idx]
			if body == "" {
				width = minCompactWidth
				body = strings.Repeat(" ", width)
			}
			parts = append(parts, lipgloss.NewStyle().Width(width).Render(body))
		}
		sections = append(sections, lipgloss.JoinHorizontal(lipgloss.Top, parts...))
		pending = pending[:0]
	}

	for _, block := range filtered {
		if block.Span == detailLayoutSpanFull {
			flushPending()
			sections = append(sections, block.Rendered)
			continue
		}
		pending = append(pending, block)
	}
	flushPending()
	return strings.Join(sections, "\n\n")
}

func chooseDetailCardColumns(cards []detailLayoutCard, maxColumns int, availableWidth int, gap int) int {
	for columns := maxColumns; columns > 1; columns-- {
		columnWidth := max(24, (availableWidth-(columns-1)*gap)/columns)
		_, widths := layoutDetailCards(cards, columns, columnWidth)
		if totalDetailLayoutWidth(widths, gap) <= availableWidth {
			return columns
		}
	}
	return 1
}

func layoutDetailCards(cards []detailLayoutCard, columns int, cardWidth int) ([]string, []int) {
	columnBodies := make([]string, columns)
	columnWidths := make([]int, columns)
	for idx, card := range cards {
		rendered := renderDetailLayoutCard(card, cardWidth)
		columnIndex := idx % columns
		if columnBodies[columnIndex] != "" {
			columnBodies[columnIndex] += "\n\n"
		}
		columnBodies[columnIndex] += rendered
		columnWidths[columnIndex] = max(columnWidths[columnIndex], lipgloss.Width(rendered))
	}
	return columnBodies, columnWidths
}

func chooseDetailBlockColumns(blocks []detailLayoutBlock, maxColumns int, availableWidth int, gap int) int {
	for columns := maxColumns; columns > 1; columns-- {
		_, widths := layoutDetailBlocks(blocks, columns)
		if totalDetailLayoutWidth(widths, gap) <= availableWidth {
			return columns
		}
	}
	return 1
}

func layoutDetailBlocks(blocks []detailLayoutBlock, columns int) ([]string, []int) {
	columnBodies := make([]string, columns)
	columnWidths := make([]int, columns)
	for idx, block := range blocks {
		columnIndex := idx % columns
		if columnBodies[columnIndex] != "" {
			columnBodies[columnIndex] += "\n\n"
		}
		columnBodies[columnIndex] += block.Rendered
		columnWidths[columnIndex] = max(columnWidths[columnIndex], lipgloss.Width(block.Rendered))
	}
	return columnBodies, columnWidths
}

func totalDetailLayoutWidth(widths []int, gap int) int {
	total := 0
	active := 0
	for _, width := range widths {
		if width <= 0 {
			continue
		}
		total += width
		active++
	}
	if active <= 1 {
		return total
	}
	return total + (active-1)*gap
}

func detailFieldLines(fields []detailField) []string {
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			continue
		}
		lines = append(lines, field.Label+":  "+value)
	}
	return lines
}

func isEmptyDetailLayoutCard(card detailLayoutCard) bool {
	if strings.TrimSpace(card.Title) != "" && (len(card.Lines) != 0 || len(card.Fields) != 0 || len(card.Groups) != 0) {
		return false
	}
	for _, group := range card.Groups {
		if !isEmptyDetailLayoutGroup(group) {
			return false
		}
	}
	for _, field := range card.Fields {
		if strings.TrimSpace(field.Value) != "" {
			return false
		}
	}
	for _, line := range card.Lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return strings.TrimSpace(card.Title) == ""
}
