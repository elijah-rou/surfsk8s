package components

import (
	"strings"
	"testing"
)

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

	view := table.View()
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
	view := table.View()
	if !strings.Contains(view, "Nothing here") {
		t.Fatalf("missing empty message:\n%s", view)
	}
}

func TestTableTruncatesLongCells(t *testing.T) {
	table := NewTable([]Column{{Title: "Name", Width: 12}})
	table.SetRows([][]string{{"this-is-a-very-long-cell"}})
	view := table.View()
	if strings.Contains(view, "this-is-a-very-long-cell") {
		t.Fatalf("expected long cell to truncate:\n%s", view)
	}
	if !strings.Contains(view, "this-is-a-v…") {
		t.Fatalf("missing truncated cell:\n%s", view)
	}
}
