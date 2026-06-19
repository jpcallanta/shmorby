// Package keybinds implements leader key and keybind map system.
package keybinds

import "time"

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
