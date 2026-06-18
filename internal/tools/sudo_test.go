package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestSudoTool_Name_ReturnsSudo checks Name returns "sudo".
func TestSudoTool_Name_ReturnsSudo(t *testing.T) {
	tool := NewSudoTool("allow", nil)
	if tool.Name() != "sudo" {
		t.Errorf("want 'sudo', got %q", tool.Name())
	}
}

// TestSudoTool_Description_NotEmpty checks Description is non-empty.
func TestSudoTool_Description_NotEmpty(t *testing.T) {
	tool := NewSudoTool("allow", nil)
	if tool.Description() == "" {
		t.Error("want non-empty description")
	}
}

// TestSudoTool_Parameters_ValidJSON checks Parameters is valid JSON.
func TestSudoTool_Parameters_ValidJSON(t *testing.T) {
	tool := NewSudoTool("allow", nil)
	var v interface{}
	if err := json.Unmarshal(tool.Parameters(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestSudoTool_Run_ExecutesSudoCommand checks sudo tool runs sudo -n.
func TestSudoTool_Run_ExecutesSudoCommand(t *testing.T) {
	mock := &mockExecutor{
		Out: []byte("ok"),
	}
	tool := NewSudoTool("allow", mock)
	args := []byte(`{"command":"systemctl status nginx"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "ok" {
		t.Errorf("want 'ok', got %q", out)
	}
	if mock.Name != "sudo" {
		t.Errorf("want sudo command, got %q", mock.Name)
	}
	if len(mock.Args) < 3 {
		t.Fatalf("want 3+ args, got %d", len(mock.Args))
	}
	if mock.Args[0] != "-n" {
		t.Errorf("want first arg '-n', got %q", mock.Args[0])
	}
	if mock.Args[1] != "sh" {
		t.Errorf("want second arg 'sh', got %q", mock.Args[1])
	}
	if mock.Args[2] != "-c" {
		t.Errorf("want third arg '-c', got %q", mock.Args[2])
	}
}

// TestSudoTool_Run_MissingCommand_ReturnsError checks missing command.
func TestSudoTool_Run_MissingCommand_ReturnsError(t *testing.T) {
	tool := NewSudoTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing command, got nil")
	}
}

// TestSudoTool_Run_Deny_ReturnsError checks deny blocks.
func TestSudoTool_Run_Deny_ReturnsError(t *testing.T) {
	mock := &mockExecutor{Out: []byte("should-not-run")}
	tool := NewSudoTool("deny", mock)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"command":"whoami"}`),
	)
	if err == nil {
		t.Fatal("want error for deny, got nil")
	}
}

// TestSudoTool_Run_ExecError_ReturnsOutput checks exec error preserves
// output.
func TestSudoTool_Run_ExecError_ReturnsOutput(t *testing.T) {
	mock := &mockExecutor{
		Out:    []byte("sudo: unknown command"),
		RunErr: fmt.Errorf("exit status 1"),
	}
	tool := NewSudoTool("allow", mock)
	args := []byte(`{"command":"bogus"}`)

	out, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want error for exec failure, got nil")
	}
	if !strings.Contains(out, "sudo: unknown command") {
		t.Errorf("want output preserved, got %q", out)
	}
}

// TestSudoTool_Run_NonZeroExit_ReturnsOutput checks non-zero exit
// returns output with exit code (no Go error).
func TestSudoTool_Run_NonZeroExit_ReturnsOutput(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 2")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatal("failed to create ExitError")
	}

	mock := &mockExecutor{
		Out:    []byte("command not found"),
		RunErr: exitErr,
	}
	tool := NewSudoTool("allow", mock)
	args := []byte(`{"command":"bogus"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want nil for non-zero exit, got %v", err)
	}
	if !strings.Contains(out, "command not found") {
		t.Errorf("want output preserved, got %q", out)
	}
	if !strings.Contains(out, "exit code: 2") {
		t.Errorf("want 'exit code: 2' in output, got %q", out)
	}
}

// TestSudoTool_Run_InvalidJSON_ReturnsError checks bad JSON.
func TestSudoTool_Run_InvalidJSON_ReturnsError(t *testing.T) {
	tool := NewSudoTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}
