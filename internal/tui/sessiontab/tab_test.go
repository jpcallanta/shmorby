package sessiontab

import (
	"testing"
)

func TestTabBar_Activate_NotFound(t *testing.T) {
	tb := New("s1", "Session 1")
	if tb.Activate("nonexistent") {
		t.Error("Activate should return false for unknown id")
	}
}

func TestTabBar_Tabs(t *testing.T) {
	tb := New("s1", "Session 1")
	tabs := tb.Tabs()
	if len(tabs) != 1 {
		t.Fatalf("want 1 tab, got %d", len(tabs))
	}
	if tabs[0].ID != "s1" {
		t.Errorf("want id s1, got %q", tabs[0].ID)
	}
	if !tabs[0].Active {
		t.Error("tab should be active")
	}
}
