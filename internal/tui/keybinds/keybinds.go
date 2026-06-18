// Package keybinds implements leader key and keybind map system.
package keybinds

import (
	"fmt"
	"strings"
	"time"
)

// Action represents a named action triggered by a keybind.
type Action string

// Action constants for all leader-key and direct bindings.
const (
	ActionCompact         Action = "compact"
	ActionNew             Action = "new"
	ActionList            Action = "list"
	ActionModel           Action = "model"
	ActionTheme           Action = "theme"
	ActionAgent           Action = "agent"
	ActionUndo            Action = "undo"
	ActionRedo            Action = "redo"
	ActionEditor          Action = "editor"
	ActionExport          Action = "export"
	ActionQuit            Action = "quit"
	ActionStatus          Action = "status"
	ActionSidebar         Action = "sidebar"
	ActionTips            Action = "tips"
	ActionCopy            Action = "copy"
	ActionChild           Action = "child"
	ActionParent          Action = "parent"
	ActionAgentCycle      Action = "agent_cycle"
	ActionAgentCycleRev   Action = "agent_cycle_reverse"
	ActionCommandList     Action = "command_list"
	ActionHistorySearch   Action = "history_search"
	ActionSessionChild    Action = "session_child"
	ActionSessionChildRev Action = "session_child_rev"
	ActionSessionParent   Action = "session_parent"
	ActionNone            Action = "none"
)

// Binding maps a key to an action.
type Binding struct {
	Key    string
	Label  string
	Action Action
}

// LeaderKey implements a leader-key sequence system.
type LeaderKey struct {
	Key      string
	Timeout  time.Duration
	Bindings map[string]Action
	active   bool
	deadline time.Time
}

// NewLeaderKey creates a leader key with the given key and timeout.
func NewLeaderKey(key string, timeout time.Duration) *LeaderKey {
	return &LeaderKey{
		Key:      key,
		Timeout:  timeout,
		Bindings: make(map[string]Action),
	}
}

// Activate starts listening for the second key.
func (l *LeaderKey) Activate() {
	l.active = true
	l.deadline = time.Now().Add(l.Timeout)
}

// Active reports whether the leader key sequence is active.
func (l *LeaderKey) Active() bool {
	return l.active
}

// Deadline returns when the leader sequence times out.
func (l *LeaderKey) Deadline() time.Time {
	return l.deadline
}

// Deactivate ends the leader sequence.
func (l *LeaderKey) Deactivate() {
	l.active = false
}

// HandleKey processes a key during the leader sequence. Returns the action
// and whether the key was consumed. If the leader is not active, returns
// ("", false) so the caller can process the key normally.
func (l *LeaderKey) HandleKey(key string) (Action, bool) {
	if !l.active {
		return "", false
	}
	l.active = false
	if time.Now().After(l.deadline) {
		return ActionNone, true
	}
	if action, ok := l.Bindings[key]; ok {
		return action, true
	}
	return ActionNone, true
}

// IsLeaderKey reports whether the given key is the leader key.
func (l *LeaderKey) IsLeaderKey(key string) bool {
	return key == l.Key
}

// RegisterBinding maps a key to an action.
func (l *LeaderKey) RegisterBinding(key string, action Action) {
	l.Bindings[key] = action
}

// BindingsList returns all registered bindings as a sorted slice.
func (l *LeaderKey) BindingsList() []Binding {
	var list []Binding
	labelMap := map[Action]string{
		ActionCompact:         "Compact session",
		ActionNew:             "New session",
		ActionList:            "Session list",
		ActionModel:           "Switch model",
		ActionTheme:           "Switch theme",
		ActionAgent:           "Switch agent",
		ActionUndo:            "Undo",
		ActionRedo:            "Redo",
		ActionEditor:          "Open editor",
		ActionExport:          "Export session",
		ActionQuit:            "Quit",
		ActionStatus:          "View status",
		ActionSidebar:         "Toggle sidebar",
		ActionTips:            "Toggle tips",
		ActionCopy:            "Copy messages",
		ActionChild:           "First child session",
		ActionParent:          "Parent session",
		ActionAgentCycle:      "Cycle agent forward",
		ActionAgentCycleRev:   "Cycle agent reverse",
		ActionCommandList:     "Command palette",
		ActionHistorySearch:   "History search",
		ActionSessionChild:    "Next child session",
		ActionSessionChildRev: "Previous child session",
		ActionSessionParent:   "Parent session",
	}
	for key, action := range l.Bindings {
		list = append(list, Binding{
			Key:    key,
			Label:  labelMap[action],
			Action: action,
		})
	}
	return list
}

// ParseKeybind parses a keybind spec string into canonical form.
// Supports: "tab", "shift+tab", "ctrl+x", "<leader>x", "up", etc.
// Unknown keys are accepted to allow graceful degradation.
func ParseKeybind(s string) (string, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "", fmt.Errorf("empty keybind")
	}
	if s == "none" {
		return "none", nil
	}
	// Handle <leader>x format
	if strings.HasPrefix(s, "<leader>") {
		suffix := strings.TrimPrefix(s, "<leader>")
		if suffix == "" {
			return "", fmt.Errorf("incomplete leader bind: %q", s)
		}
		return s, nil
	}
	// Validate comma-separated alternatives
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if !isValidKey(part) {
			return "", fmt.Errorf("unknown key: %q", part)
		}
	}
	return s, nil
}

// isValidKey reports whether a single key token is recognized.
// Accepts any ctrl+letter, shift+letter, alt+letter, F-key,
// arrow, modifier, or single printable letter.
func isValidKey(key string) bool {
	known := map[string]bool{
		"tab": true, "shift+tab": true, "ctrl+tab": true,
		"ctrl+shift+tab": true,
		"return":         true, "enter": true, "escape": true, "esc": true,
		"up": true, "down": true, "left": true, "right": true,
		"pageup": true, "pagedown": true, "home": true, "end": true,
		"space": true, "backspace": true, "delete": true,
	}
	if known[key] {
		return true
	}
	// F1-F12
	if len(key) == 3 && key[0] == 'f' && key[1] >= '1' && key[1] <= '9' && key[2] >= '0' && key[2] <= '9' {
		return true
	}
	// ctrl+letter, shift+letter, alt+letter
	parts := strings.SplitN(key, "+", 2)
	if len(parts) == 2 {
		mod := parts[0]
		if mod == "ctrl" || mod == "shift" || mod == "alt" || mod == "meta" {
			if len(parts[1]) == 1 {
				return true
			}
			// ctrl+shift+letter
			sub := strings.SplitN(parts[1], "+", 2)
			if len(sub) == 2 && (sub[0] == "shift" || sub[0] == "ctrl") && len(sub[1]) == 1 {
				return true
			}
		}
	}
	// Single printable letter or number
	if len(key) == 1 {
		return true
	}
	return false
}
