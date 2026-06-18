package keybinds

import (
	"testing"
	"time"
)

func TestNewLeaderKey(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	if lk.Key != "ctrl+x" {
		t.Errorf("want ctrl+x, got %q", lk.Key)
	}
	if lk.Active() {
		t.Error("should not be active initially")
	}
}

func TestLeaderKey_ActivateDeactivate(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	lk.Activate()
	if !lk.Active() {
		t.Error("should be active after Activate")
	}
	lk.Deactivate()
	if lk.Active() {
		t.Error("should not be active after Deactivate")
	}
}

func TestLeaderKey_HandleKey(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	lk.RegisterBinding("c", ActionCompact)
	lk.Activate()
	action, consumed := lk.HandleKey("c")
	if !consumed {
		t.Error("key should be consumed")
	}
	if action != ActionCompact {
		t.Errorf("want compact, got %s", action)
	}
}

func TestLeaderKey_HandleKey_NotActive(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	lk.RegisterBinding("c", ActionCompact)
	action, consumed := lk.HandleKey("c")
	if consumed {
		t.Error("should not consume when inactive")
	}
	if action != "" {
		t.Errorf("want empty, got %s", action)
	}
}

func TestLeaderKey_IsLeaderKey(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	if !lk.IsLeaderKey("ctrl+x") {
		t.Error("should match leader key")
	}
	if lk.IsLeaderKey("ctrl+p") {
		t.Error("should not match other key")
	}
}

func TestLeaderKey_BindingsList(t *testing.T) {
	lk := NewLeaderKey("ctrl+x", 2*time.Second)
	lk.RegisterBinding("c", ActionCompact)
	lk.RegisterBinding("n", ActionNew)
	bindings := lk.BindingsList()
	if len(bindings) != 2 {
		t.Fatalf("want 2 bindings, got %d", len(bindings))
	}
}

func TestParseKeybind_Valid(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"tab", "tab"},
		{"shift+tab", "shift+tab"},
		{"ctrl+x", "ctrl+x"},
		{"ctrl+p", "ctrl+p"},
		{"<leader>n", "<leader>n"},
		{"none", "none"},
	}
	for _, c := range cases {
		result, err := ParseKeybind(c.input)
		if err != nil {
			t.Errorf("ParseKeybind(%q): unexpected error: %v", c.input, err)
		}
		if result != c.want {
			t.Errorf("ParseKeybind(%q): want %q, got %q", c.input, c.want, result)
		}
	}
}

func TestParseKeybind_Invalid(t *testing.T) {
	_, err := ParseKeybind("unknown_key")
	if err == nil {
		t.Error("expected error for invalid key")
	}
}

func TestParseKeybind_Empty(t *testing.T) {
	_, err := ParseKeybind("")
	if err == nil {
		t.Error("expected error for empty key")
	}
}
