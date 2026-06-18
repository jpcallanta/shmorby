package spinner

import (
	"strings"
	"testing"
	"time"
)

func TestNewModel_NotActive(t *testing.T) {
	m := Model{}
	if m.View() != "" {
		t.Error("new model should have empty view")
	}
}

func TestStart(t *testing.T) {
	m := Model{}
	m.Start("thinking…")
	if !m.active {
		t.Error("model should be active after Start")
	}
	if m.spinnerText != "thinking…" {
		t.Errorf("want %q, got %q", "thinking…", m.spinnerText)
	}
}

func TestView_AfterStart(t *testing.T) {
	m := Model{}
	m.Start("working")
	view := m.View()
	if !strings.Contains(view, "working") {
		t.Errorf("view should contain spinner text, got %q", view)
	}
}

func TestStop(t *testing.T) {
	m := Model{}
	m.Start("test")
	m.Stop()
	if m.active {
		t.Error("model should be inactive after Stop")
	}
	if m.View() != "" {
		t.Error("view should be empty after Stop")
	}
}

func TestTick(t *testing.T) {
	m := Model{}
	m.Start("test")
	first := m.Tick()
	// Tick should return non-empty
	if first == "" {
		t.Error("Tick should return non-empty when active")
	}
	// Tick should advance the frame
	second := m.Tick()
	if second == first {
		t.Error("Tick should advance frame")
	}
}

func TestTick_WhenInactive(t *testing.T) {
	m := Model{}
	result := m.Tick()
	if result != "" {
		t.Error("Tick should return empty when inactive")
	}
}

func TestFrames(t *testing.T) {
	if len(Frames) == 0 {
		t.Error("Frames should not be empty")
	}
}

func TestFrameRotation(t *testing.T) {
	m := Model{}
	m.Start("test")
	seen := make(map[int]bool)
	for i := 0; i < len(Frames)*2; i++ {
		m.Tick()
		_ = m.frame
		seen[m.frame] = true
	}
	// After len(Frames)*2 ticks, we should have cycled through frames
	if len(seen) != len(Frames) {
		t.Errorf("expected %d unique frames, got %d", len(Frames), len(seen))
	}
}

func TestElapsed(t *testing.T) {
	m := Model{}
	m.Start("test")
	time.Sleep(2 * time.Millisecond)
	if m.Elapsed() < 2*time.Millisecond {
		t.Error("Elapsed should be at least 2ms")
	}
}

func TestView_Format(t *testing.T) {
	m := Model{}
	m.Start("hello")
	view := m.View()
	// Format should be: "⠋ hello (0s)" or similar
	if !strings.HasPrefix(view, "⠋") && !strings.HasPrefix(view, "⠙") {
		t.Errorf("view should start with braille character, got %q", view)
	}
}
