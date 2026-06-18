// Package sessiontab provides a multi-session tab bar.
package sessiontab

// Tab represents a single session tab.
type Tab struct {
	ID       string
	Label    string
	Active   bool
	Spinning bool
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

// Add creates a new session tab and makes it active.
func (tb *TabBar) Add(id, label string) {
	tb.tabs[tb.active].Active = false
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.active = i
			tb.tabs[i].Active = true
			tb.UpdateVisibility()
			return
		}
	}
	tb.tabs = append(tb.tabs, Tab{ID: id, Label: label, Active: true})
	tb.active = len(tb.tabs) - 1
	tb.UpdateVisibility()
}

// Remove deletes a tab by ID. Returns false if only one tab remains.
func (tb *TabBar) Remove(id string) bool {
	if len(tb.tabs) <= 1 {
		return false
	}
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.tabs = append(tb.tabs[:i], tb.tabs[i+1:]...)
			// Clear all Active flags before reassigning.
			for j := range tb.tabs {
				tb.tabs[j].Active = false
			}
			if tb.active >= len(tb.tabs) {
				tb.active = len(tb.tabs) - 1
			}
			tb.tabs[tb.active].Active = true
			tb.UpdateVisibility()
			return true
		}
	}
	return false
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

// Next cycles to the next tab (right).
func (tb *TabBar) Next() {
	if len(tb.tabs) <= 1 {
		return
	}
	tb.tabs[tb.active].Active = false
	tb.active = (tb.active + 1) % len(tb.tabs)
	tb.tabs[tb.active].Active = true
}

// Previous cycles to the previous tab (left).
func (tb *TabBar) Previous() {
	if len(tb.tabs) <= 1 {
		return
	}
	tb.tabs[tb.active].Active = false
	tb.active = (tb.active - 1 + len(tb.tabs)) % len(tb.tabs)
	tb.tabs[tb.active].Active = true
}

// Tabs returns all tabs.
func (tb *TabBar) Tabs() []Tab {
	out := make([]Tab, len(tb.tabs))
	copy(out, tb.tabs)
	return out
}

// Count returns the number of tabs.
func (tb *TabBar) Count() int {
	return len(tb.tabs)
}

// ActiveIndex returns the active tab index.
func (tb *TabBar) ActiveIndex() int {
	return tb.active
}

// SetSpinning marks a tab as processing.
func (tb *TabBar) SetSpinning(id string, spinning bool) {
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.tabs[i].Spinning = spinning
			return
		}
	}
}
