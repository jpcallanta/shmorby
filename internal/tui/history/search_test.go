package history

import (
	"testing"
)

func TestReverseISearch_Toggle(t *testing.T) {
	h := New(100)
	s := NewReverseISearch(h)
	if s.Visible() {
		t.Error("should not be visible initially")
	}
	s.Toggle()
	if !s.Visible() {
		t.Error("should be visible after Toggle")
	}
	s.Toggle()
	if s.Visible() {
		t.Error("should not be visible after second Toggle")
	}
}

func TestReverseISearch_Query(t *testing.T) {
	h := New(100)
	h.Add("hello world")
	h.Add("goodbye")
	s := NewReverseISearch(h)
	s.Toggle()
	s.SetQuery("hello")
	matches := s.Matches()
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	if matches[0] != "hello world" {
		t.Errorf("want 'hello world', got %q", matches[0])
	}
}

func TestReverseISearch_EmptyQuery(t *testing.T) {
	h := New(100)
	h.Add("first")
	h.Add("second")
	h.Add("third")
	s := NewReverseISearch(h)
	s.Toggle()
	matches := s.Matches()
	if len(matches) != 3 {
		t.Fatalf("want 3 matches, got %d", len(matches))
	}
	// Should be in reverse order
	if matches[0] != "third" {
		t.Errorf("want 'third' first, got %q", matches[0])
	}
}

func TestReverseISearch_Selected(t *testing.T) {
	h := New(100)
	h.Add("alpha")
	h.Add("beta")
	h.Add("gamma")
	s := NewReverseISearch(h)
	s.Toggle()
	s.SetQuery("a")
	// Matches in reverse order: gamma, alpha — selected is first (gamma).
	sel := s.Selected()
	if sel != "gamma" {
		t.Errorf("want gamma, got %q", sel)
	}
}

func TestReverseISearch_CycleForward(t *testing.T) {
	h := New(100)
	h.Add("one")
	h.Add("two")
	s := NewReverseISearch(h)
	s.Toggle()
	s.CycleForward()
	if s.SelectedIndex() != 1 {
		t.Errorf("want index 1, got %d", s.SelectedIndex())
	}
}

func TestReverseISearch_CycleReverse(t *testing.T) {
	h := New(100)
	h.Add("one")
	h.Add("two")
	s := NewReverseISearch(h)
	s.Toggle()
	s.CycleReverse()
	if s.SelectedIndex() != 1 {
		t.Errorf("want index 1, got %d", s.SelectedIndex())
	}
}

func TestReverseISearch_Dismiss(t *testing.T) {
	h := New(100)
	s := NewReverseISearch(h)
	s.Toggle()
	s.Dismiss()
	if s.Visible() {
		t.Error("should not be visible after Dismiss")
	}
	if s.Query() != "" {
		t.Errorf("query should be empty, got %q", s.Query())
	}
}

func TestReverseISearch_AddRune(t *testing.T) {
	h := New(100)
	h.Add("hello world")
	s := NewReverseISearch(h)
	s.Toggle()
	s.AddRune('h')
	if s.Query() != "h" {
		t.Errorf("want query 'h', got %q", s.Query())
	}
	if len(s.Matches()) != 1 {
		t.Errorf("want 1 match, got %d", len(s.Matches()))
	}
}

func TestReverseISearch_NoMatch(t *testing.T) {
	h := New(100)
	h.Add("hello")
	s := NewReverseISearch(h)
	s.Toggle()
	s.SetQuery("zzz")
	if len(s.Matches()) != 0 {
		t.Errorf("want 0 matches, got %d", len(s.Matches()))
	}
	if s.Selected() != "" {
		t.Errorf("selected should be empty, got %q", s.Selected())
	}
}
