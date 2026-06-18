package navigation

import (
	"testing"
)

func TestModeSwitcher_CycleForward(t *testing.T) {
	ms := NewModeSwitcher()
	if ms.Current() != "operate" {
		t.Errorf("want operate, got %q", ms.Current())
	}
	ms.CycleForward()
	if ms.Current() != "diagnose" {
		t.Errorf("want diagnose, got %q", ms.Current())
	}
	ms.CycleForward()
	if ms.Current() != "operate" {
		t.Errorf("want operate (wrap), got %q", ms.Current())
	}
}

func TestModeSwitcher_CycleReverse(t *testing.T) {
	ms := NewModeSwitcher()
	ms.CycleReverse()
	if ms.Current() != "diagnose" {
		t.Errorf("want diagnose, got %q", ms.Current())
	}
	ms.CycleReverse()
	if ms.Current() != "operate" {
		t.Errorf("want operate, got %q", ms.Current())
	}
}

func TestModeSwitcher_Current(t *testing.T) {
	ms := NewModeSwitcher()
	if ms.Current() != "operate" {
		t.Errorf("want operate, got %q", ms.Current())
	}
}

func TestModeSwitcher_SetCurrent(t *testing.T) {
	ms := NewModeSwitcher()
	if !ms.SetCurrent("diagnose") {
		t.Error("SetCurrent(diagnose) should return true")
	}
	if ms.Current() != "diagnose" {
		t.Errorf("want diagnose, got %q", ms.Current())
	}
	if ms.SetCurrent("invalid") {
		t.Error("SetCurrent(invalid) should return false")
	}
}

func TestModeSwitcher_Modes(t *testing.T) {
	ms := NewModeSwitcher()
	modes := ms.Modes()
	if len(modes) != 2 {
		t.Fatalf("want 2 modes, got %d", len(modes))
	}
	if modes[0] != "operate" || modes[1] != "diagnose" {
		t.Errorf("want [operate diagnose], got %v", modes)
	}
}
