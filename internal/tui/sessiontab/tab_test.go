package sessiontab

import (
	"testing"
)

func TestTabBar_New(t *testing.T) {
	tb := New("session-1", "Session 1")
	if tb.Count() != 1 {
		t.Fatalf("want 1 tab, got %d", tb.Count())
	}
	if tb.ActiveID() != "session-1" {
		t.Errorf("want session-1, got %q", tb.ActiveID())
	}
	if tb.Visible() {
		t.Error("should not be visible with single tab")
	}
}

func TestTabBar_Add(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	if tb.Count() != 2 {
		t.Fatalf("want 2 tabs, got %d", tb.Count())
	}
	if !tb.Visible() {
		t.Error("should be visible with 2 tabs")
	}
	if tb.ActiveID() != "s2" {
		t.Errorf("active should be s2, got %q", tb.ActiveID())
	}
}

func TestTabBar_Activate(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	if !tb.Activate("s1") {
		t.Fatal("Activate should return true")
	}
	if tb.ActiveID() != "s1" {
		t.Errorf("active should be s1, got %q", tb.ActiveID())
	}
}

func TestTabBar_Activate_NotFound(t *testing.T) {
	tb := New("s1", "Session 1")
	if tb.Activate("nonexistent") {
		t.Error("Activate should return false for unknown id")
	}
}

func TestTabBar_Next(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	tb.Activate("s1")
	tb.Next()
	if tb.ActiveID() != "s2" {
		t.Errorf("active should be s2, got %q", tb.ActiveID())
	}
	tb.Next()
	if tb.ActiveID() != "s1" {
		t.Errorf("active should wrap to s1, got %q", tb.ActiveID())
	}
}

func TestTabBar_Previous(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	tb.Activate("s1")
	tb.Previous()
	if tb.ActiveID() != "s2" {
		t.Errorf("active should be s2, got %q", tb.ActiveID())
	}
}

func TestTabBar_Remove(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	if !tb.Remove("s2") {
		t.Fatal("Remove should return true")
	}
	if tb.Count() != 1 {
		t.Fatalf("want 1 tab, got %d", tb.Count())
	}
	if tb.ActiveID() != "s1" {
		t.Errorf("active should be s1, got %q", tb.ActiveID())
	}
}

func TestTabBar_Remove_LastTab(t *testing.T) {
	tb := New("s1", "Session 1")
	if tb.Remove("s1") {
		t.Error("Remove should return false for last tab")
	}
}

func TestTabBar_SetSpinning(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.SetSpinning("s1", true)
	tabs := tb.Tabs()
	if !tabs[0].Spinning {
		t.Error("tab should be spinning")
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

func TestTabBar_ActiveIndex(t *testing.T) {
	tb := New("s1", "Session 1")
	tb.Add("s2", "Session 2")
	if tb.ActiveIndex() != 1 {
		t.Errorf("want index 1, got %d", tb.ActiveIndex())
	}
}
