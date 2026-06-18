package config

import (
	"testing"
)

func TestDefaultConfig_TUINav(t *testing.T) {
	cfg := defaultConfig()
	if cfg.TUI.Nav.FollowMode != true {
		t.Error("want follow_mode true by default")
	}
	if cfg.TUI.Nav.ScrollLinesPerTick != 5 {
		t.Errorf("want scroll_lines_per_tick 5, got %d", cfg.TUI.Nav.ScrollLinesPerTick)
	}
	if cfg.TUI.Nav.LeaderTimeout != 2000 {
		t.Errorf("want leader_timeout 2000, got %d", cfg.TUI.Nav.LeaderTimeout)
	}
	if cfg.TUI.Nav.HistorySize != 100 {
		t.Errorf("want history_size 100, got %d", cfg.TUI.Nav.HistorySize)
	}
	if cfg.TUI.Nav.Keybinds.Leader != "ctrl+x" {
		t.Errorf("want leader ctrl+x, got %q", cfg.TUI.Nav.Keybinds.Leader)
	}
	if cfg.TUI.Nav.Keybinds.AgentCycle != "tab" {
		t.Errorf("want agent_cycle tab, got %q", cfg.TUI.Nav.Keybinds.AgentCycle)
	}
	if cfg.TUI.Nav.Keybinds.AgentCycleReverse != "shift+tab" {
		t.Errorf("want agent_cycle_reverse shift+tab, got %q", cfg.TUI.Nav.Keybinds.AgentCycleReverse)
	}
	if cfg.TUI.Nav.Keybinds.CommandList != "ctrl+p" {
		t.Errorf("want command_list ctrl+p, got %q", cfg.TUI.Nav.Keybinds.CommandList)
	}
	if cfg.TUI.Nav.Keybinds.HistorySearch != "ctrl+r" {
		t.Errorf("want history_search ctrl+r, got %q", cfg.TUI.Nav.Keybinds.HistorySearch)
	}
}

func TestDefaultConfig_TUINavScrolling(t *testing.T) {
	cfg := defaultConfig()
	if cfg.TUI.Nav.ScrollAcceleration.Enabled {
		t.Error("scroll acceleration should be disabled by default")
	}
}
