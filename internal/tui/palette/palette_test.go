package palette

import (
	"testing"
)

func TestCommandPalette_Toggle(t *testing.T) {
	p := New()
	if p.Visible() {
		t.Error("should not be visible initially")
	}
	p.Toggle()
	if !p.Visible() {
		t.Error("should be visible after Toggle")
	}
	p.Toggle()
	if p.Visible() {
		t.Error("should not be visible after second Toggle")
	}
}

func TestCommandPalette_AddItem(t *testing.T) {
	p := New()
	p.AddItem(CommandItem{
		Name:        "compact",
		Slash:       "/compact",
		Description: "Compact session",
		Shortcut:    "ctrl+x c",
	})
	p.Toggle()
	// Should show filter to match
	items := p.Filtered()
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].Name != "compact" {
		t.Errorf("want compact, got %q", items[0].Name)
	}
}

func TestCommandPalette_Filter(t *testing.T) {
	p := New()
	p.AddItem(CommandItem{Name: "compact", Slash: "/compact"})
	p.AddItem(CommandItem{Name: "model", Slash: "/model"})
	p.SetFilter("comp")
	items := p.Filtered()
	if len(items) != 1 {
		t.Fatalf("want 1 match, got %d", len(items))
	}
	if items[0].Name != "compact" {
		t.Errorf("want compact, got %q", items[0].Name)
	}
}

func TestCommandPalette_Filter_NoMatch(t *testing.T) {
	p := New()
	p.AddItem(CommandItem{Name: "compact"})
	p.SetFilter("zzz")
	items := p.Filtered()
	if len(items) != 0 {
		t.Errorf("want 0 matches, got %d", len(items))
	}
}

func TestCommandPalette_Execute(t *testing.T) {
	p := New()
	executed := false
	p.AddItem(CommandItem{
		Name:   "compact",
		Action: func() { executed = true },
	})
	p.Toggle()
	if !p.Execute() {
		t.Error("Execute should return true")
	}
	if !executed {
		t.Error("action should have been called")
	}
	if p.Visible() {
		t.Error("palette should dismiss after execute")
	}
}

func TestCommandPalette_Execute_NoSelection(t *testing.T) {
	p := New()
	if p.Execute() {
		t.Error("Execute with no items should return false")
	}
}

func TestCommandPalette_MoveDownUp(t *testing.T) {
	p := New()
	p.AddItem(CommandItem{Name: "compact"})
	p.AddItem(CommandItem{Name: "model"})
	p.Toggle()
	if p.selected != 0 {
		t.Errorf("want selected 0, got %d", p.selected)
	}
	p.MoveDown()
	if p.selected != 1 {
		t.Errorf("want selected 1, got %d", p.selected)
	}
	p.MoveUp()
	if p.selected != 0 {
		t.Errorf("want selected 0, got %d", p.selected)
	}
}

func TestFuzzyMatch(t *testing.T) {
	if !fuzzyMatch("compact", "comp") {
		t.Error("prefix should match")
	}
	if !fuzzyMatch("compact", "pact") {
		t.Error("substring should match")
	}
	if fuzzyMatch("compact", "xyz") {
		t.Error("no match should return false")
	}
	if !fuzzyMatch("Compact", "compact") {
		t.Error("case insensitive match")
	}
}
