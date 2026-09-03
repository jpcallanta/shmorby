// Package viewport wraps bubbles/viewport with follow mode, new-content
// detection, and output selection.
package viewport

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Selection tracks mouse drag selection boundaries.
type Selection struct {
	StartLine int
	EndLine   int
	Active    bool
	Dragging  bool
}

// Model wraps bubbles/viewport.Model with follow mode and output selection.
type Model struct {
	vp             viewport.Model
	followMode     bool
	newContent     bool
	selectionMode  bool
	selectionStart int
	selectionEnd   int
	sel            Selection
}

// Creates a Model with the given dimensions.
func New(width, height int) Model {
	vp := viewport.New(width, height)
	return Model{
		vp:         vp,
		followMode: true,
	}
}

// Delegates to the underlying viewport for key events.
func (m *Model) Update(msg tea.Msg) {
	m.vp, _ = m.vp.Update(msg)
}

// Returns the viewport's rendered content.
func (m Model) View() string {
	return m.vp.View()
}

// Returns the viewport width.
func (m Model) Width() int {
	return m.vp.Width
}

// Updates the viewport width without recreating it.
func (m *Model) SetWidth(w int) {
	m.vp.Width = w
}

// Returns the viewport height.
func (m Model) Height() int {
	return m.vp.Height
}

// Updates the viewport height without recreating it.
func (m *Model) SetHeight(h int) {
	m.vp.Height = h
}

// Replaces the content and re-enables follow mode.
func (m *Model) SetContent(content string) {
	m.vp.SetContent(content)
	if m.followMode {
		m.vp.GotoBottom()
	}
}

// Returns the current scroll position as a percentage.
func (m Model) ScrollPercent() float64 {
	return m.vp.ScrollPercent()
}

// Reports whether the viewport is scrolled to the very bottom.
func (m Model) AtBottom() bool {
	return m.vp.ScrollPercent() >= 1.0
}

// Reports whether follow mode is enabled.
func (m Model) FollowMode() bool {
	return m.followMode
}

// Enables or disables follow mode.
func (m *Model) SetFollowMode(enabled bool) {
	m.followMode = enabled
	if enabled {
		m.vp.GotoBottom()
		m.newContent = false
	}
}

// Reports whether new content has arrived while paused.
func (m Model) NewContent() bool {
	return m.newContent
}

// Scrolls up by the given number of lines.
func (m *Model) ScrollUp(n int) {
	m.vp.ScrollUp(n)
	m.checkFollowMode()
}

// Scrolls down by the given number of lines.
// Re-enables follow mode if the user reaches the bottom.
func (m *Model) ScrollDown(n int) {
	m.vp.ScrollDown(n)
	if m.AtBottom() {
		m.SetFollowMode(true)
	}
}

// Scrolls up by half the viewport height.
func (m *Model) ScrollHalfPageUp() {
	m.vp.HalfPageUp()
	m.checkFollowMode()
}

// Scrolls down by half the viewport height.
// Re-enables follow mode if the user reaches the bottom.
func (m *Model) ScrollHalfPageDown() {
	m.vp.HalfPageDown()
	if m.AtBottom() {
		m.SetFollowMode(true)
	}
}

// Scrolls to the top of the content.
func (m *Model) GotoTop() {
	m.vp.GotoTop()
	m.followMode = false
}

// Scrolls to the bottom and re-enables follow mode.
func (m *Model) GotoBottom() {
	m.SetFollowMode(true)
}

// Turns off follow mode if user has scrolled away from bottom.
func (m *Model) checkFollowMode() {
	if m.followMode && !m.AtBottom() {
		m.followMode = false
	}
}

// Should be called after new content is appended.
// If follow mode is paused, it sets the new-content indicator.
func (m *Model) NotifyContentAdded() {
	if !m.followMode {
		m.newContent = true
	}
}

// Reports whether output selection is active.
func (m Model) SelectionMode() bool {
	return m.selectionMode
}

// Toggles selection mode.
func (m *Model) SetSelectionMode(enabled bool) {
	m.selectionMode = enabled
	if !enabled {
		m.selectionStart = 0
		m.selectionEnd = 0
		m.sel = Selection{}
	}
}

// Returns the start index of the selection.
func (m Model) SelectionStart() int {
	return m.selectionStart
}

// Returns the end index of the selection.
func (m Model) SelectionEnd() int {
	return m.selectionEnd
}

// Handles mouse events for selection when mouse tracking is enabled.
func (m *Model) MouseMsg(msg tea.MouseMsg) {
	// Dragging in selection mode: track mouse position.
	if m.selectionMode && m.sel.Dragging {
		m.handleDrag(msg)
		return
	}

	// Click enters selection mode and starts a drag.
	if msg.Action == tea.MouseActionPress &&
		msg.Button == tea.MouseButtonLeft {
		if !m.selectionMode {
			m.selectionMode = true
		}
		line := msg.Y + m.vp.YOffset
		m.sel.StartLine = line
		m.sel.EndLine = line
		m.sel.Dragging = true
		return
	}

	// Delegate remaining mouse events to bubbles viewport.
	m.vp, _ = m.vp.Update(msg)
	m.checkFollowMode()
}

// Returns the selection boundaries from a mouse drag.
func (m *Model) DragSelection() (start, end int, active bool) {
	return m.sel.StartLine, m.sel.EndLine, m.sel.Active
}

// Reports whether a mouse drag is in progress.
func (m *Model) IsDragging() bool {
	return m.sel.Dragging
}

// Updates selection during mouse drag.
func (m *Model) handleDrag(msg tea.MouseMsg) {
	if msg.Action == tea.MouseActionRelease &&
		msg.Button == tea.MouseButtonLeft {
		m.sel.Dragging = false
		m.sel.Active = true
		m.selectionStart = m.sel.StartLine
		m.selectionEnd = m.sel.EndLine
		return
	}
	m.sel.EndLine = msg.Y + m.vp.YOffset
}
