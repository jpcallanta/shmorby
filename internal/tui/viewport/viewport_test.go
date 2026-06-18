package viewport

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	vp := New(80, 20)
	if vp.Width() != 80 {
		t.Errorf("want width 80, got %d", vp.Width())
	}
	if vp.Height() != 20 {
		t.Errorf("want height 20, got %d", vp.Height())
	}
	if !vp.FollowMode() {
		t.Error("follow mode should be enabled by default")
	}
}

func TestFollowMode_AutoScroll(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent("line 1\nline 2")
	if !vp.AtBottom() {
		t.Error("should be at bottom after SetContent with follow mode")
	}
}

func TestFollowMode_PauseOnScrollUp(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.LineUp(1)
	if vp.FollowMode() {
		t.Error("follow mode should be disabled after scrolling up")
	}
}

func TestFollowMode_ReEnableOnGotoBottom(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.LineUp(1)
	vp.GotoBottom()
	if !vp.FollowMode() {
		t.Error("follow mode should be re-enabled after GotoBottom")
	}
}

func TestNewContentIndicator(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.LineUp(1)
	vp.NotifyContentAdded()
	if !vp.NewContent() {
		t.Error("new content indicator should be set when follow is paused")
	}
}

func TestNewContentIndicator_ClearedOnGotoBottom(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent("content")
	vp.LineUp(1)
	vp.NotifyContentAdded()
	vp.GotoBottom()
	if vp.NewContent() {
		t.Error("new content indicator should be cleared after GotoBottom")
	}
}

func TestSelectionMode(t *testing.T) {
	vp := New(80, 20)
	if vp.SelectionMode() {
		t.Error("selection mode should be disabled by default")
	}
	vp.SetSelectionMode(true)
	if !vp.SelectionMode() {
		t.Error("selection mode should be enabled")
	}
}

func TestSelectionMode_Disable(t *testing.T) {
	vp := New(80, 20)
	vp.SetSelectionMode(true)
	vp.selectionStart = 5
	vp.selectionEnd = 10
	vp.SetSelectionMode(false)
	if vp.SelectionMode() {
		t.Error("selection mode should be disabled")
	}
	if vp.SelectionStart() != 0 {
		t.Errorf("want selection start 0, got %d", vp.SelectionStart())
	}
	if vp.SelectionEnd() != 0 {
		t.Errorf("want selection end 0, got %d", vp.SelectionEnd())
	}
}

func TestSelectedText(t *testing.T) {
	vp := New(80, 20)
	lines := []string{"line a", "line b", "line c"}
	vp.selectionStart = 0
	vp.selectionEnd = 2
	selected := vp.SelectedText(lines)
	if selected != "line a\nline b" {
		t.Errorf("want %q, got %q", "line a\nline b", selected)
	}
}

func TestSelectedText_Swapped(t *testing.T) {
	vp := New(80, 20)
	lines := []string{"a", "b", "c"}
	vp.selectionStart = 2
	vp.selectionEnd = 0
	selected := vp.SelectedText(lines)
	if selected != "a\nb" {
		t.Errorf("want %q, got %q", "a\nb", selected)
	}
}

func TestSelectedText_OutOfRange(t *testing.T) {
	vp := New(80, 20)
	vp.selectionStart = 0
	vp.selectionEnd = 99
	lines := []string{"a", "b"}
	selected := vp.SelectedText(lines)
	if selected != "a\nb" {
		t.Errorf("want %q, got %q", "a\nb", selected)
	}
}

func TestSelectedText_Empty(t *testing.T) {
	vp := New(80, 20)
	vp.selectionStart = 0
	vp.selectionEnd = 0
	lines := []string{"a", "b"}
	selected := vp.SelectedText(lines)
	if selected != "" {
		t.Errorf("want empty, got %q", selected)
	}
}

func TestGotoTop(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.GotoTop()
	if vp.FollowMode() {
		t.Error("follow mode should be disabled after GotoTop")
	}
}

func TestScrollPercent(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	pct := vp.ScrollPercent()
	if pct < 0 || pct > 1.0 {
		t.Errorf("scroll percent out of range: %f", pct)
	}
}

func TestMoveSelection(t *testing.T) {
	vp := New(80, 20)
	vp.selectionEnd = 5
	vp.MoveSelection(3)
	if vp.SelectionEnd() != 8 {
		t.Errorf("want end 8, got %d", vp.SelectionEnd())
	}
	vp.MoveSelection(-10)
	if vp.SelectionEnd() != 0 {
		t.Errorf("want end 0, got %d", vp.SelectionEnd())
	}
}

func TestSetWidth(t *testing.T) {
	vp := New(80, 20)
	vp.SetWidth(100)
	if vp.Width() != 100 {
		t.Errorf("want width 100, got %d", vp.Width())
	}
}

func TestSetHeight(t *testing.T) {
	vp := New(80, 20)
	vp.SetHeight(30)
	if vp.Height() != 30 {
		t.Errorf("want height 30, got %d", vp.Height())
	}
}
