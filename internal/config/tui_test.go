package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultConfig_TUINav(t *testing.T) {
	cfg := defaultConfig()
	if cfg.TUI.Nav.FollowMode != true {
		t.Error("want follow_mode true by default")
	}
	if cfg.TUI.Nav.ScrollLinesPerTick != 5 {
		t.Errorf("want scroll_lines_per_tick 5, got %d", cfg.TUI.Nav.ScrollLinesPerTick)
	}
	if cfg.TUI.Nav.HistorySize != 100 {
		t.Errorf("want history_size 100, got %d", cfg.TUI.Nav.HistorySize)
	}
}

func TestDefaultConfig_TUINavScrolling(t *testing.T) {
	cfg := defaultConfig()
	if cfg.TUI.Nav.ScrollAcceleration.Enabled {
		t.Error("scroll acceleration should be disabled by default")
	}
}

// Tests that theme_files defaults to unset and parses from YAML.
func TestConfigParse_ThemeFiles(t *testing.T) {
	cfg := defaultConfig()
	if len(cfg.TUI.ThemeFiles) != 0 {
		t.Errorf("want no theme_files by default, got %v", cfg.TUI.ThemeFiles)
	}

	var parsed Config
	in := []byte("tui:\n  theme: nord\n" +
		"  theme_files:\n    - ~/.config/shmorby/themes.yaml\n")
	if err := yaml.Unmarshal(in, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed.TUI.ThemeFiles) != 1 ||
		parsed.TUI.ThemeFiles[0] != "~/.config/shmorby/themes.yaml" {
		t.Errorf("want one theme_files entry, got %v", parsed.TUI.ThemeFiles)
	}
}

// Tests that mouse tracking defaults to false as an intent guard.
func TestDefaultConfig_TUIMouse(t *testing.T) {
	cfg := defaultConfig()

	if cfg.TUI.Mouse {
		t.Error("want mouse tracking false by default")
	}
}

// Tests that mouse tracking parses from YAML.
func TestConfigParse_TUIMouse(t *testing.T) {
	var parsed Config
	in := []byte("tui:\n  mouse: true\n")
	if err := yaml.Unmarshal(in, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !parsed.TUI.Mouse {
		t.Error("want mouse tracking true from YAML")
	}
}
