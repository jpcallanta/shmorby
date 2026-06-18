package navigation

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockExecutor simulates shell execution for testing.
type mockExecutor struct {
	output string
	err    error
}

func (m *mockExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return []byte(m.output + fmt.Sprintf(" [%s %v]", name, args)), m.err
}

func TestShellCmdHandler_NotBang(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{}, "bash", "operate", "allow")
	handled, _, err := h.Handle("hello")
	if handled {
		t.Error("should not be handled")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestShellCmdHandler_Executes(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{output: "hello"}, "bash", "operate", "allow")
	handled, out, err := h.Handle("!echo hello")
	if !handled {
		t.Fatal("should be handled")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Stdout, "hello") {
		t.Errorf("output missing 'hello', got %q", out.Stdout)
	}
}

func TestShellCmdHandler_RepeatLast(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{output: "echoed"}, "bash", "operate", "allow")
	h.Handle("!echo first")
	handled, out, err := h.Handle("!!")
	if !handled {
		t.Fatal("!! should be handled")
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.Stdout, "echoed") {
		t.Errorf("output missing 'echoed', got %q", out.Stdout)
	}
}

func TestShellCmdHandler_RepeatEmpty(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{}, "bash", "operate", "allow")
	handled, _, err := h.Handle("!")
	if !handled {
		t.Fatal("! should be handled")
	}
	if err == nil {
		t.Error("expected error for empty last command")
	}
}

func TestShellCmdHandler_DiagnoseDenyMutating(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{}, "bash", "diagnose", "allow")
	handled, _, err := h.Handle("!rm -rf /tmp/test")
	if !handled {
		t.Fatal("should be handled")
	}
	if err == nil {
		t.Error("expected error for mutating cmd in diagnose")
	}
}

func TestShellCmdHandler_PermissionDeny(t *testing.T) {
	h := NewShellCmdHandler(&mockExecutor{}, "bash", "operate", "deny")
	handled, _, err := h.Handle("!ls")
	if !handled {
		t.Fatal("should be handled")
	}
	if err == nil {
		t.Error("expected permission denied error")
	}
}

func TestFormatOutput(t *testing.T) {
	out := Output{
		Stdout:   "hello\nworld",
		Stderr:   "",
		ExitCode: 0,
	}
	result := FormatOutput(out)
	if result != "hello\nworld" {
		t.Errorf("want %q, got %q", "hello\nworld", result)
	}
}

func TestFormatOutput_WithExitCode(t *testing.T) {
	out := Output{
		Stdout:   "output",
		ExitCode: 1,
	}
	result := FormatOutput(out)
	if !strings.Contains(result, "exit code: 1") {
		t.Errorf("output missing exit code, got %q", result)
	}
}
