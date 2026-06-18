package tui

import (
	"strings"
	"testing"
)

func TestNewPermissionPrompt(t *testing.T) {
	p := NewPermissionPrompt("shell", "systemctl restart nginx", "service restart", "rule[3]")
	if p.Tool != "shell" {
		t.Errorf("want shell, got %s", p.Tool)
	}
	if p.Command != "systemctl restart nginx" {
		t.Errorf("want systemctl restart nginx, got %s", p.Command)
	}
	if p.Reason != "service restart" {
		t.Errorf("want service restart, got %s", p.Reason)
	}
	if p.Rule != "rule[3]" {
		t.Errorf("want rule[3], got %s", p.Rule)
	}
	if p.Choice == nil {
		t.Error("choice channel should not be nil")
	}
}

func TestRenderPermissionPrompt(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.permission = &PermissionPrompt{
		Tool:    "shell",
		Command: "systemctl restart nginx",
		Reason:  "service restart",
	}
	rendered := m.renderPermissionPrompt(80)
	if !strings.Contains(rendered, "systemctl restart nginx") {
		t.Error("rendered prompt missing command")
	}
	if !strings.Contains(rendered, "service restart") {
		t.Error("rendered prompt missing reason")
	}
	if !strings.Contains(rendered, "[y] allow") {
		t.Error("rendered prompt missing allow option")
	}
	if !strings.Contains(rendered, "[n] deny") {
		t.Error("rendered prompt missing deny option")
	}
	if !strings.Contains(rendered, "[a] allow all") {
		t.Error("rendered prompt missing allow-all option")
	}
}

func TestPermissionPrompt_EmptyWhenNil(t *testing.T) {
	m := NewModel(Config{})
	rendered := m.renderPermissionPrompt(80)
	if rendered != "" {
		t.Error("should be empty when no permission prompt")
	}
}

func TestPermissionChoiceRoundTrip(t *testing.T) {
	p := NewPermissionPrompt("shell", "cmd", "reason", "rule")
	p.Choice <- PermissionAllow
	if got := <-p.Choice; got != PermissionAllow {
		t.Errorf("want allow, got %v", got)
	}
}
