package styles

import (
	"strings"
	"testing"
)

func TestRenderBorderedBox_WithTitle(t *testing.T) {
	ts := DefaultTheme()
	result := RenderBorderedBox("Test", "content", 40,
		ts.PopupTitle, ts.PopupItem)
	if !strings.Contains(result, "Test") {
		t.Error("bordered box missing title")
	}
	if !strings.Contains(result, "content") {
		t.Error("bordered box missing content")
	}
}

func TestRenderBorderedBox_NoTitle(t *testing.T) {
	ts := DefaultTheme()
	result := RenderBorderedBox("", "content", 40,
		ts.PopupTitle, ts.PopupItem)
	if !strings.Contains(result, "content") {
		t.Error("bordered box missing content")
	}
}

func TestRenderTabBar_Empty(t *testing.T) {
	result := RenderTabBar(nil, 0, 80)
	if result != "" {
		t.Errorf("want empty, got %q", result)
	}
}

func TestRenderTabBar_SingleTab(t *testing.T) {
	ts := DefaultTheme()
	tabs := []TabStyle{
		{Label: "session-1", ActiveStyle: ts.TabActive, InactiveStyle: ts.TabInactive, SpinStyle: ts.TabSpin},
	}
	result := RenderTabBar(tabs, 0, 80)
	if !strings.Contains(result, "session-1") {
		t.Error("tab bar missing label")
	}
}

func TestRenderTabBar_MultipleTabs(t *testing.T) {
	ts := DefaultTheme()
	tabs := []TabStyle{
		{Label: "tab1", ActiveStyle: ts.TabActive, InactiveStyle: ts.TabInactive, SpinStyle: ts.TabSpin},
		{Label: "tab2", ActiveStyle: ts.TabActive, InactiveStyle: ts.TabInactive, SpinStyle: ts.TabSpin},
	}
	result := RenderTabBar(tabs, 0, 80)
	if !strings.Contains(result, "tab1") || !strings.Contains(result, "tab2") {
		t.Error("tab bar missing tab labels")
	}
}

func TestTruncateLabel_Short(t *testing.T) {
	result := truncateLabel("hello", 10)
	if result != "hello" {
		t.Errorf("want %q, got %q", "hello", result)
	}
}

func TestTruncateLabel_Long(t *testing.T) {
	result := truncateLabel("abcdefghijklmnop", 10)
	if result != "abcdefghi…" {
		t.Errorf("want %q, got %q", "abcdefghi…", result)
	}
}
