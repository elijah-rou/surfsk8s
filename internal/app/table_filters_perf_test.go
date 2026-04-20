package app

import "testing"

func TestHasAnyEnabledColumnFilters(t *testing.T) {
	t.Parallel()
	if hasAnyEnabledColumnFilters(nil) {
		t.Fatalf("expected false for nil")
	}
	if hasAnyEnabledColumnFilters([]tableColumnFilter{{Enabled: false}}) {
		t.Fatalf("expected false when disabled")
	}
	if !hasAnyEnabledColumnFilters([]tableColumnFilter{{Enabled: true}}) {
		t.Fatalf("expected true when enabled")
	}
}
