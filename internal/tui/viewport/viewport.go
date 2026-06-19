// Package viewport wraps bubbles/viewport with follow mode, new-content
// detection, and output selection.
package viewport

import (
	"strings"

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

// New creates a Model with the given dimensions.
func New(width, height int) Model {
	vp := viewport.New(width, height)
	return Model{
		vp:         vp,
		followMode: true,
	}
}

// Update delegates to the underlying viewport for key events.
func (m *Model) Update(msg tea.Msg) {
	m.vp, _ = m.vp.Update(msg)
}

// View returns the viewport's rendered content.
func (m Model) View() string {
	return m.vp.View()
}

// Width returns the viewport width.
func (m Model) Width() int {
	return m.vp.Width
}

// SetWidth updates the viewport width without recreating it.
func (m *Model) SetWidth(w int) {
	m.vp.Width = w
}

// Height returns the viewport height.
func (m Model) Height() int {
	return m.vp.Height
}

// SetHeight updates the viewport height without recreating it.
func (m *Model) SetHeight(h int) {
	m.vp.Height = h
}

// SetContent replaces the content and re-enables follow mode.
func (m *Model) SetContent(content string) {
	m.vp.SetContent(content)
	if m.followMode {
		m.vp.GotoBottom()
	}
}

// ScrollPercent returns the current scroll position as a percentage.
func (m Model) ScrollPercent() float64 {
	return m.vp.ScrollPercent()
}

// AtBottom returns true if viewport is scrolled to the very bottom.
func (m Model) AtBottom() bool {
	return m.vp.ScrollPercent() >= 1.0
}

// FollowMode returns whether follow mode is enabled.
func (m Model) FollowMode() bool {
	return m.followMode
}

// SetFollowMode enables or disables follow mode.
func (m *Model) SetFollowMode(enabled bool) {
	m.followMode = enabled
	if enabled {
		m.vp.GotoBottom()
		m.newContent = false
	}
}

// NewContent returns whether new content has arrived while paused.
func (m Model) NewContent() bool {
	return m.newContent
}

// ScrollUp scrolls up by the given number of lines.
func (m *Model) ScrollUp(n int) {
	m.vp.ScrollUp(n)
	m.checkFollowMode()
}

// ScrollDown scrolls down by the given number of lines.
// Re-enables follow mode if the user reaches the bottom.
func (m *Model) ScrollDown(n int) {
	m.vp.ScrollDown(n)
	if m.AtBottom() {
		m.SetFollowMode(true)
	}
}

// ScrollHalfPageUp scrolls up by half the viewport height.
func (m *Model) ScrollHalfPageUp() {
	m.vp.HalfPageUp()
	m.checkFollowMode()
}

// ScrollHalfPageDown scrolls down by half the viewport height.
// Re-enables follow mode if the user reaches the bottom.
func (m *Model) ScrollHalfPageDown() {
	m.vp.HalfPageDown()
	if m.AtBottom() {
		m.SetFollowMode(true)
	}
}

// GotoTop scrolls to the top of the content.
func (m *Model) GotoTop() {
	m.vp.GotoTop()
	m.followMode = false
}

// GotoBottom scrolls to the bottom and re-enables follow mode.
func (m *Model) GotoBottom() {
	m.SetFollowMode(true)
}

// checkFollowMode turns off follow mode if user has scrolled away from bottom.
func (m *Model) checkFollowMode() {
	if m.followMode && !m.AtBottom() {
		m.followMode = false
	}
}

// NotifyContentAdded should be called after new content is appended.
// If follow mode is paused, it sets the new-content indicator.
func (m *Model) NotifyContentAdded() {
	if !m.followMode {
		m.newContent = true
	}
}

// SelectionMode returns whether output selection is active.
func (m Model) SelectionMode() bool {
	return m.selectionMode
}

// SetSelectionMode toggles selection mode.
func (m *Model) SetSelectionMode(enabled bool) {
	m.selectionMode = enabled
	if !enabled {
		m.selectionStart = 0
		m.selectionEnd = 0
		m.sel = Selection{}
	}
}

// SelectionStart returns the start index of the selection.
func (m Model) SelectionStart() int {
	return m.selectionStart
}

// SelectionEnd returns the end index of the selection.
func (m Model) SelectionEnd() int {
	return m.selectionEnd
}

// MoveSelection moves the selection end by delta lines.
func (m *Model) MoveSelection(delta int) {
	m.selectionEnd += delta
	if m.selectionEnd < 0 {
		m.selectionEnd = 0
	}
}

// SelectedText returns the currently selected text range from lines.
func (m *Model) SelectedText(lines []string) string {
	start := m.selectionStart
	end := m.selectionEnd
	if start > end {
		start, end = end, start
	}
	if start >= len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	var sb strings.Builder
	for i := start; i < end; i++ {
		if i > start {
			sb.WriteString("\n")
		}
		sb.WriteString(lines[i])
	}
	return sb.String()
}

// MouseMsg handles mouse events for selection when mouse tracking is enabled.
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

// DragSelection returns the selection boundaries from a mouse drag.
func (m *Model) DragSelection() (start, end int, active bool) {
	return m.sel.StartLine, m.sel.EndLine, m.sel.Active
}

// IsDragging reports whether a mouse drag is in progress.
func (m *Model) IsDragging() bool {
	return m.sel.Dragging
}

// handleDrag updates selection during mouse drag.
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

// KeyMap returns the underlying viewport keymap for customization.
func (m *Model) KeyMap() *viewport.KeyMap {
	return &m.vp.KeyMap
}
