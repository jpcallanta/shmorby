package keybinds

import (
	"testing"
	"time"
)

func TestLeaderKey_ActivateDeactivate(t *testing.T) {
	lk := NewLeaderKey("space", 2*time.Second)
	if lk.Active() {
		t.Error("expected inactive initially")
	}
	lk.Activate()
	if !lk.Active() {
		t.Error("expected active after Activate")
	}
	lk.Deactivate()
	if lk.Active() {
		t.Error("expected inactive after Deactivate")
	}
}

func TestLeaderKey_Deadline(t *testing.T) {
	lk := NewLeaderKey("space", 2*time.Second)
	lk.Activate()
	if lk.Deadline().Before(time.Now()) {
		t.Error("deadline should be in the future")
	}
}

func TestLeaderKey_HandleKey_NotActive(t *testing.T) {
	lk := NewLeaderKey("space", 2*time.Second)
	action, consumed := lk.HandleKey("n")
	if consumed {
		t.Error("should not be consumed when inactive")
	}
	if action != "" {
		t.Errorf("want empty action, got %q", action)
	}
}

func TestLeaderKey_HandleKey_Active(t *testing.T) {
	lk := NewLeaderKey("space", 2*time.Second)
	lk.RegisterBinding("n", ActionNew)
	lk.RegisterBinding("c", ActionCompact)

	lk.Activate()

	tests := []struct {
		key      string
		wantAct  Action
		wantCons bool
	}{
		{"n", ActionNew, true},
		{"c", ActionCompact, true},
		{"x", ActionNone, true},
	}
	for _, tt := range tests {
		lk.Activate()
		action, consumed := lk.HandleKey(tt.key)
		if consumed != tt.wantCons {
			t.Errorf("HandleKey(%q): consumed=%v, want %v", tt.key, consumed, tt.wantCons)
		}
		if action != tt.wantAct {
			t.Errorf("HandleKey(%q): action=%q, want %q", tt.key, action, tt.wantAct)
		}
	}
}

func TestLeaderKey_BindingsList(t *testing.T) {
	lk := NewLeaderKey("space", 2*time.Second)
	lk.RegisterBinding("n", ActionNew)
	lk.RegisterBinding("c", ActionCompact)

	bindings := lk.BindingsList()
	if len(bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(bindings))
	}
}
