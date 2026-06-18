package keybinds

import (
	"fmt"
	"strings"
	"time"
)

// WhichKeyModel manages the which-key overlay popup.
type WhichKeyModel struct {
	leader   string
	bindings []Binding
	visible  bool
	deadline time.Time
	width    int
}

// NewWhichKey creates a which-key popup model.
func NewWhichKey(leader string) *WhichKeyModel {
	return &WhichKeyModel{
		leader: leader,
	}
}

// Show starts displaying the popup with the given bindings.
func (w *WhichKeyModel) Show(bindings []Binding, width int, timeout time.Duration) {
	w.bindings = bindings
	w.visible = true
	w.deadline = time.Now().Add(timeout)
	w.width = width
}

// Dismiss hides the popup.
func (w *WhichKeyModel) Dismiss() {
	w.visible = false
	w.bindings = nil
}

// Visible reports whether the popup is shown.
func (w *WhichKeyModel) Visible() bool {
	return w.visible
}

// Expired reports whether the popup deadline has passed.
func (w *WhichKeyModel) Expired() bool {
	return w.visible && time.Now().After(w.deadline)
}

// SetBindings updates the binding list.
func (w *WhichKeyModel) SetBindings(bindings []Binding) {
	w.bindings = bindings
}

// View renders the which-key popup content.
func (w *WhichKeyModel) View() string {
	if !w.visible || len(w.bindings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" Leader: %s\n", w.leader))
	b.WriteString(strings.Repeat("─", w.width-2) + "\n")
	for _, bind := range w.bindings {
		b.WriteString(fmt.Sprintf("  %s  %s\n", bind.Key, bind.Label))
	}
	b.WriteString(strings.Repeat("─", w.width-2) + "\n")
	b.WriteString(" esc/timeout to cancel")
	return b.String()
}
