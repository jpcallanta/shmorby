package agent

import (
	"bytes"
	"strings"
	"testing"

	"shmorby/internal/session"
)

// Tests that REPL /help output contains all required sections.
func TestREPLHelp_AllSections(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	sections := []string{
		"AGENT MODES",
		"SLASH COMMANDS",
		"KEYBOARD SHORTCUTS",
		"LEADER KEY",
		"PERMISSIONS",
	}
	for _, s := range sections {
		if !strings.Contains(output, s) {
			t.Errorf("REPL /help missing section %q", s)
		}
	}
}

// Tests that REPL /help includes all slash commands.
func TestREPLHelp_AllCommands(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	commands := []string{
		"/help", "/quit", "/reset", "/model", "/agent",
		"/scope", "/memory", "/context", "/log", "/tui",
	}
	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("REPL /help missing command %q", cmd)
		}
	}
}

// Tests that REPL /help includes keyboard shortcuts.
func TestREPLHelp_KeyboardShortcuts(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	shortcuts := []string{
		"ctrl+h", "ctrl+p", "ctrl+r", "ctrl+c",
		"ctrl+v", "ctrl+l", "ctrl+t", "ctrl+x",
	}
	for _, sc := range shortcuts {
		if !strings.Contains(output, sc) {
			t.Errorf("REPL /help missing shortcut %q", sc)
		}
	}
}

// Tests that REPL /help includes leader key bindings.
func TestREPLHelp_LeaderKeys(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	bindings := []string{
		"ctrl+x c", "ctrl+x n", "ctrl+x l",
		"ctrl+x q", "ctrl+x h",
	}
	for _, b := range bindings {
		if !strings.Contains(output, b) {
			t.Errorf("REPL /help missing leader binding %q", b)
		}
	}
}

// Tests that REPL /help shows current mode.
func TestREPLHelp_ShowsMode(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "diagnose",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "diagnose") {
		t.Error("REPL /help should show current mode")
	}
}
