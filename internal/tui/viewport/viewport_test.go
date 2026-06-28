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
	vp.ScrollUp(1)
	if vp.FollowMode() {
		t.Error("follow mode should be disabled after scrolling up")
	}
}

func TestFollowMode_ReEnableOnGotoBottom(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.ScrollUp(1)
	vp.GotoBottom()
	if !vp.FollowMode() {
		t.Error("follow mode should be re-enabled after GotoBottom")
	}
}

func TestNewContentIndicator(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent(strings.Repeat("line\n", 100))
	vp.ScrollUp(1)
	vp.NotifyContentAdded()
	if !vp.NewContent() {
		t.Error("new content indicator should be set when follow is paused")
	}
}

func TestNewContentIndicator_ClearedOnGotoBottom(t *testing.T) {
	vp := New(80, 20)
	vp.SetContent("content")
	vp.ScrollUp(1)
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
