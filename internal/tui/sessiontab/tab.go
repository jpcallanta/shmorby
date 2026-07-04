// Package sessiontab provides a multi-session tab bar.
package sessiontab

// Tab represents a single session tab.
type Tab struct {
	ID         string
	Label      string
	Active     bool
	Spinning   bool
	IsSubagent bool   // true for child subagent sessions
	ParentID   string // parent session ID (empty for root)
	Status     string // "" (running), "ok", "error"
}

// TabBar holds multiple session tabs.
type TabBar struct {
	tabs    []Tab
	active  int
	visible bool
}

// New creates a tab bar with an initial session.
func New(id, label string) *TabBar {
	bar := &TabBar{
		tabs: []Tab{
			{ID: id, Label: label, Active: true},
		},
		active:  0,
		visible: false,
	}
	return bar
}

// Visible reports whether the tab bar should be shown (>1 session).
func (tb *TabBar) Visible() bool {
	return tb.visible
}

// UpdateVisibility toggles visibility based on tab count.
func (tb *TabBar) UpdateVisibility() {
	tb.visible = len(tb.tabs) > 1
}

// Activate switches to the tab with the given ID. Returns false if not found.
func (tb *TabBar) Activate(id string) bool {
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.tabs[tb.active].Active = false
			tb.active = i
			tb.tabs[i].Active = true
			return true
		}
	}
	return false
}

// ActiveID returns the current active tab ID.
func (tb *TabBar) ActiveID() string {
	if tb.active < len(tb.tabs) {
		return tb.tabs[tb.active].ID
	}
	return ""
}

// Tabs returns all tabs.
func (tb *TabBar) Tabs() []Tab {
	out := make([]Tab, len(tb.tabs))
	copy(out, tb.tabs)
	return out
}

// ActiveIndex returns the active tab index.
func (tb *TabBar) ActiveIndex() int {
	return tb.active
}

// AddTab adds a new tab and updates visibility.
func (tb *TabBar) AddTab(t Tab) {
	for i, existing := range tb.tabs {
		if existing.ID == t.ID {
			tb.tabs[i] = t
			tb.UpdateVisibility()
			return
		}
	}
	tb.tabs = append(tb.tabs, t)
	tb.UpdateVisibility()
}

// RemoveTab removes a tab by ID and updates visibility.
func (tb *TabBar) RemoveTab(id string) {
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.tabs = append(tb.tabs[:i], tb.tabs[i+1:]...)
			if tb.active >= len(tb.tabs) {
				tb.active = len(tb.tabs) - 1
			}
			if tb.active >= 0 && tb.active < len(tb.tabs) {
				tb.tabs[tb.active].Active = true
			}
			tb.UpdateVisibility()
			return
		}
	}
}

// UpdateTabStatus sets the status and spinner for a tab by ID.
func (tb *TabBar) UpdateTabStatus(id, status string) {
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.tabs[i].Status = status
			tb.tabs[i].Spinning = status == ""
			return
		}
	}
}
