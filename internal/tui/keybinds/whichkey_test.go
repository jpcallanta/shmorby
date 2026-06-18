package keybinds

import (
	"strings"
	"testing"
	"time"
)

func TestWhichKey_ShowDismiss(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	if w.Visible() {
		t.Error("should not be visible initially")
	}
	w.Show([]Binding{{Key: "c", Label: "Compact", Action: ActionCompact}}, 80, 2*time.Second)
	if !w.Visible() {
		t.Error("should be visible after Show")
	}
	w.Dismiss()
	if w.Visible() {
		t.Error("should not be visible after Dismiss")
	}
}

func TestWhichKey_View(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	w.Show([]Binding{
		{Key: "c", Label: "Compact", Action: ActionCompact},
		{Key: "n", Label: "New", Action: ActionNew},
	}, 80, 2*time.Second)
	view := w.View()
	if !strings.Contains(view, "ctrl+x") {
		t.Error("view missing leader key")
	}
	if !strings.Contains(view, "Compact") {
		t.Error("view missing label")
	}
	if !strings.Contains(view, "esc/timeout") {
		t.Error("view missing dismiss hint")
	}
}

func TestWhichKey_View_Hidden(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	if w.View() != "" {
		t.Error("hidden which key should render empty")
	}
}

func TestWhichKey_Expired(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	w.deadline = time.Now().Add(-1 * time.Second)
	w.visible = true
	if !w.Expired() {
		t.Error("should be expired")
	}
}

func TestWhichKey_NotExpired(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	w.deadline = time.Now().Add(1 * time.Hour)
	w.visible = true
	if w.Expired() {
		t.Error("should not be expired")
	}
}

func TestWhichKey_SetBindings(t *testing.T) {
	w := NewWhichKey("ctrl+x")
	bindings := []Binding{{Key: "q", Label: "Quit", Action: ActionQuit}}
	w.SetBindings(bindings)
	w.Show(bindings, 80, 2*time.Second)
	view := w.View()
	if !strings.Contains(view, "Quit") {
		t.Error("view missing updated binding")
	}
}
