package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func TestTableColumnJumpEdges(t *testing.T) {
	table := NewTable([]Column{{Title: "A", Width: 4}, {Title: "B", Width: 4}, {Title: "C", Width: 4}})
	table.SetSize(40, 4)
	table.SetRows([][]string{{"a", "b", "c"}})

	table.MoveColumnEnd()
	if got, want := table.SelectedColumnIndex(), 2; got != want {
		t.Fatalf("column = %d, want %d", got, want)
	}
	table.MoveColumnStart()
	if got, want := table.SelectedColumnIndex(), 0; got != want {
		t.Fatalf("column = %d, want %d", got, want)
	}
}

func TestTableVirtualScrollAndCursorClamp(t *testing.T) {
	table := NewTable([]Column{{Title: "Name", Width: 8}, {Title: "Status", Width: 7}})
	table.SetSize(32, 4)
	table.SetRows([][]string{
		{"pod-01", "Running"},
		{"pod-02", "Running"},
		{"pod-03", "Running"},
		{"pod-04", "Running"},
		{"pod-05", "Running"},
	})

	table.MoveDown(4)
	if got, want := table.SelectedIndex(), 4; got != want {
		t.Fatalf("selected = %d, want %d", got, want)
	}
	if got, want := table.offset, 2; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}

	view := stripANSI(table.View())
	if strings.Contains(view, "pod-01") || strings.Contains(view, "pod-02") {
		t.Fatalf("view should not render rows above viewport:\n%s", view)
	}
	if !strings.Contains(view, "pod-03") || !strings.Contains(view, "pod-05") {
		t.Fatalf("view missing visible rows:\n%s", view)
	}

	table.SetRows([][]string{{"only", "Running"}})
	if got, want := table.SelectedIndex(), 0; got != want {
		t.Fatalf("selected after clamp = %d, want %d", got, want)
	}
	if got, want := table.offset, 0; got != want {
		t.Fatalf("offset after clamp = %d, want %d", got, want)
	}
}

func TestTableEmptyMessageConfigurable(t *testing.T) {
	table := NewTable([]Column{{Title: "Name", Width: 12}})
	table.SetEmptyMessage("Nothing here")
	view := stripANSI(table.View())
	if !strings.Contains(view, "Nothing here") {
		t.Fatalf("missing empty message:\n%s", view)
	}
}

func TestTableTruncatesLongCells(t *testing.T) {
	table := NewTable([]Column{{Title: "Name", Width: 12}})
	table.SetRows([][]string{{"this-is-a-very-long-cell"}})
	view := stripANSI(table.View())
	if strings.Contains(view, "this-is-a-very-long-cell") {
		t.Fatalf("expected long cell to truncate:\n%s", view)
	}
	if !strings.Contains(view, "this-is-a-v…") {
		t.Fatalf("missing truncated cell:\n%s", view)
	}
}

func TestTableTruncatesANSIColoredCellsByVisibleWidth(t *testing.T) {
	table := NewTable([]Column{{Title: "CPU", Width: 12}})
	table.SetRows([][]string{{"\x1b[31m████████\x1b[0m 120m/500m"}})
	view := stripANSI(table.View())
	if !strings.Contains(view, "████████") {
		t.Fatalf("missing visible bar content:\n%s", view)
	}
	if strings.Contains(view, "120m/500m") {
		t.Fatalf("expected visible-width truncation to clip long ANSI cell:\n%s", view)
	}
}

func TestTableRendersWrappedCenteredHeadersAndDividers(t *testing.T) {
	table := NewTable([]Column{{Title: "Context", Width: 10}, {Title: "Cluster-IP", Width: 10}, {Title: "Age", Width: 6}})
	table.SetVariant(TableVariantRich)
	table.SetRows([][]string{{"dev", "10.0.0.1", "5m"}})
	view := stripANSI(table.View())
	if !strings.Contains(view, "│") {
		t.Fatalf("missing column separator:\n%s", view)
	}
	if !strings.Contains(view, "┼") {
		t.Fatalf("missing header divider:\n%s", view)
	}
	if !strings.Contains(view, "Cluster") || !strings.Contains(view, "IP") {
		t.Fatalf("missing wrapped header title:\n%s", view)
	}
}

func TestTableTopDividerUsesColumnJoins(t *testing.T) {
	table := NewTable([]Column{{Title: "A", Width: 4}, {Title: "B", Width: 4}, {Title: "C", Width: 4}})
	table.SetVariant(TableVariantRich)
	table.SetSize(20, 5)
	divider := stripANSI(table.TopDivider())
	if !strings.Contains(divider, "┬") {
		t.Fatalf("missing top divider join: %q", divider)
	}
}

func TestTableHorizontalViewportKeepsSelectedColumnVisible(t *testing.T) {
	table := NewTable([]Column{{Title: "A", Width: 8}, {Title: "B", Width: 8}, {Title: "C", Width: 8}, {Title: "D", Width: 8}})
	table.SetVariant(TableVariantRich)
	table.SetSize(18, 4)
	table.SetRows([][]string{{"a", "b", "c", "d"}})
	table.MoveColumnEnd()
	view := stripANSI(table.View())
	if !strings.Contains(view, "D") {
		t.Fatalf("expected last column visible:\n%s", view)
	}
	if strings.Contains(view, "A") && strings.Contains(view, "B") && strings.Contains(view, "C") && strings.Contains(view, "D") {
		t.Fatalf("expected horizontal viewport, got all columns:\n%s", view)
	}
}

func TestTableVisualCellSelectionHighlightsRange(t *testing.T) {
	table := NewTable([]Column{{Title: "A", Width: 4}, {Title: "B", Width: 4}, {Title: "C", Width: 4}})
	table.SetVariant(TableVariantRich)
	table.SetSize(30, 5)
	table.SetRows([][]string{{"a1", "b1", "c1"}, {"a2", "b2", "c2"}})
	table.BeginCellSelection()
	table.MoveRight(1)
	table.MoveDown(1)
	rowStart, rowEnd := table.SelectedRowRange()
	colStart, colEnd := table.SelectedColumnRange()
	if got, want := rowStart, 0; got != want {
		t.Fatalf("rowStart = %d, want %d", got, want)
	}
	if got, want := rowEnd, 2; got != want {
		t.Fatalf("rowEnd = %d, want %d", got, want)
	}
	if got, want := colStart, 0; got != want {
		t.Fatalf("colStart = %d, want %d", got, want)
	}
	if got, want := colEnd, 2; got != want {
		t.Fatalf("colEnd = %d, want %d", got, want)
	}
}

func TestDataStyleForColumnStorageNotTreatedAsAge(t *testing.T) {
	// Regression: "STORAGE" contains substring "AGE"; it must not use ageStyle.
	// Use a CPU-like millicore string that ageStyle would interpret as "120 minutes" if misclassified.
	val := "120m"
	plain := lipgloss.NewStyle().Render(val)
	storage := dataStyleForColumn("STORAGE", val).Render(val)
	if storage != plain {
		t.Fatalf("STORAGE column should use default styling for %q, got %q want %q", val, storage, plain)
	}
}
