// Package spinner provides a timer-based spinner animation.
package spinner

import (
	"fmt"
	"time"
)

// Frames for the braille spinner animation.
var Frames = []string{
	"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
}

// Model holds spinner state.
type Model struct {
	frame       int
	start       time.Time
	active      bool
	spinnerText string
}

// Start begins the animation.
func (m *Model) Start(text string) {
	m.frame = 0
	m.start = time.Now()
	m.spinnerText = text
	m.active = true
}

// Stop halts the animation.
func (m *Model) Stop() {
	m.active = false
}

// Tick advances the frame and returns the current display string.
func (m *Model) Tick() string {
	if !m.active {
		return ""
	}
	m.frame = (m.frame + 1) % len(Frames)
	return m.View()
}

// Elapsed returns time since start.
func (m *Model) Elapsed() time.Duration {
	return time.Since(m.start)
}

// View returns the spinner display without elapsed time.
// The caller appends timing and token info.
func (m *Model) View() string {
	if !m.active {
		return ""
	}
	return fmt.Sprintf(
		"%s %s",
		Frames[m.frame],
		m.spinnerText,
	)
}
