package styles

import (
	"testing"
)

func TestDefaultTheme(t *testing.T) {
	theme := DefaultTheme()
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("PromptNormal should have a foreground color")
	}
	if theme.PermPrompt.GetForeground() == nil {
		t.Error("PermPrompt should have a foreground color")
	}
	if theme.Selection.GetForeground() == nil {
		t.Error("Selection should have a foreground color")
	}
}

func TestGetTheme_Default(t *testing.T) {
	theme := GetTheme("catppuccin-mocha")
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("mocha theme should have PromptNormal color")
	}
}

func TestGetTheme_Latte(t *testing.T) {
	theme := GetTheme("catppuccin-latte")
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("latte theme should have PromptNormal color")
	}
}

func TestGetTheme_Frappe(t *testing.T) {
	theme := GetTheme("catppuccin-frappe")
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("frappe theme should have PromptNormal color")
	}
}

func TestGetTheme_Macchiato(t *testing.T) {
	theme := GetTheme("catppuccin-macchiato")
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("macchiato theme should have PromptNormal color")
	}
}

func TestGetTheme_Minimal(t *testing.T) {
	theme := GetTheme("minimal")
	rendered := theme.Selection.Render("test")
	if rendered == "" {
		t.Error("minimal theme selection should render")
	}
}

func TestGetTheme_Unknown(t *testing.T) {
	theme := GetTheme("nonexistent")
	if theme.PromptNormal.GetForeground() == nil {
		t.Error("unknown theme should fallback to default")
	}
}

func TestAllThemesHaveKeys(t *testing.T) {
	expected := []string{
		"catppuccin-mocha",
		"catppuccin-latte",
		"catppuccin-frappe",
		"catppuccin-macchiato",
		"minimal",
	}
	for _, name := range expected {
		if _, ok := Themes[name]; !ok {
			t.Errorf("missing theme: %s", name)
		}
	}
}
