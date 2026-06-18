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

// TestSSHTool_Name_ReturnsSSH checks Name returns "ssh".
func TestSSHTool_Name_ReturnsSSH(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	if tool.Name() != "ssh" {
		t.Errorf("want 'ssh', got %q", tool.Name())
	}
}

// TestSSHTool_Description_NotEmpty checks Description is non-empty.
func TestSSHTool_Description_NotEmpty(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	if tool.Description() == "" {
		t.Error("want non-empty description")
	}
}

// TestSSHTool_Parameters_ValidJSON checks Parameters is valid JSON.
func TestSSHTool_Parameters_ValidJSON(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	var v interface{}
	if err := json.Unmarshal(tool.Parameters(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestSSHTool_Run_ExecutesSSHCommand checks SSH tool runs the expected
// command.
func TestSSHTool_Run_ExecutesSSHCommand(t *testing.T) {
	mock := &mockExecutor{
		Out: []byte("uptime 99 days"),
	}
	tool := NewSSHTool("allow", mock)
	args := []byte(`{"host":"example.com","command":"uptime"}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "uptime 99 days") {
		t.Errorf("want output containing 'uptime 99 days', got %q",
			out)
	}
	if mock.Name != "ssh" {
		t.Errorf("want ssh command, got %q", mock.Name)
	}
	if len(mock.Args) < 5 {
		t.Fatalf("want 5+ args (including -o flags), got %d",
			len(mock.Args))
	}
	if mock.Args[len(mock.Args)-1] != "uptime" {
		t.Errorf("want last arg 'uptime', got %q",
			mock.Args[len(mock.Args)-1])
	}
	// Assert BatchMode=yes and StrictHostKeyChecking=accept-new.
	hasBatchMode := false
	hasStrictHost := false
	for i, a := range mock.Args {
		if a == "-o" && i+1 < len(mock.Args) {
			if mock.Args[i+1] == "BatchMode=yes" {
				hasBatchMode = true
			}
			if mock.Args[i+1] ==
				"StrictHostKeyChecking=accept-new" {
				hasStrictHost = true
			}
		}
	}
	if !hasBatchMode {
		t.Errorf("want -o BatchMode=yes in args, got %v",
			mock.Args)
	}
	if !hasStrictHost {
		t.Errorf(
			"want -o StrictHostKeyChecking=accept-new in args,"+
				" got %v", mock.Args)
	}
}

// TestSSHTool_Run_WithUserAndPort checks -l and -p flags.
func TestSSHTool_Run_WithUserAndPort(t *testing.T) {
	mock := &mockExecutor{Out: []byte("ok")}
	tool := NewSSHTool("allow", mock)
	args := []byte(
		`{"host":"x.com","command":"whoami","user":"admin","port":2222}`,
	)

	_, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	foundUser := false
	foundPort := false
	for i, a := range mock.Args {
		if a == "-l" && i+1 < len(mock.Args) &&
			mock.Args[i+1] == "admin" {
			foundUser = true
		}
		if a == "-p" && i+1 < len(mock.Args) &&
			mock.Args[i+1] == "2222" {
			foundPort = true
		}
	}
	if !foundUser {
		t.Errorf("want -l admin in args, got %v", mock.Args)
	}
	if !foundPort {
		t.Errorf("want -p 2222 in args, got %v", mock.Args)
	}
}

// TestSSHTool_Run_MissingHost_ReturnsError checks missing host error.
func TestSSHTool_Run_MissingHost_ReturnsError(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"command":"uptime"}`),
	)
	if err == nil {
		t.Fatal("want error for missing host, got nil")
	}
}

// TestSSHTool_Run_MissingCommand_ReturnsError checks missing command
// error.
func TestSSHTool_Run_MissingCommand_ReturnsError(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"host":"example.com"}`),
	)
	if err == nil {
		t.Fatal("want error for missing command, got nil")
	}
}

// TestSSHTool_Run_Deny_ReturnsError checks deny blocks execution.
func TestSSHTool_Run_Deny_ReturnsError(t *testing.T) {
	mock := &mockExecutor{Out: []byte("should-not-run")}
	tool := NewSSHTool("deny", mock)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"host":"x.com","command":"uptime"}`),
	)
	if err == nil {
		t.Fatal("want error for deny, got nil")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("want 'denied' in error, got %v", err)
	}
}

// TestSSHTool_Run_ExecError_ReturnsOutput checks exec error preserves
// output.
func TestSSHTool_Run_ExecError_ReturnsOutput(t *testing.T) {
	mock := &mockExecutor{
		Out:    []byte("permission denied"),
		RunErr: fmt.Errorf("exit status 1"),
	}
	tool := NewSSHTool("allow", mock)
	args := []byte(
		`{"host":"x.com","command":"ls /root"}`,
	)

	out, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want error for exec failure, got nil")
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("want output preserved, got %q", out)
	}
}

// TestSSHTool_Run_NonZeroExit_ReturnsOutput checks non-zero exit
// returns output with exit code (no Go error).
func TestSSHTool_Run_NonZeroExit_ReturnsOutput(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatal("failed to create ExitError")
	}

	mock := &mockExecutor{
		Out:    []byte("permission denied"),
		RunErr: exitErr,
	}
	tool := NewSSHTool("allow", mock)
	args := []byte(
		`{"host":"x.com","command":"ls /root"}`,
	)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want nil for non-zero exit, got %v", err)
	}
	if !strings.Contains(out, "permission denied") {
		t.Errorf("want output preserved, got %q", out)
	}
	if !strings.Contains(out, "exit code: 1") {
		t.Errorf("want 'exit code: 1' in output, got %q", out)
	}
}

// TestSSHTool_Run_InvalidJSON_ReturnsError checks bad JSON returns
// error.
func TestSSHTool_Run_InvalidJSON_ReturnsError(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "invalid ssh args") {
		t.Errorf("want 'invalid ssh args' in error, got %v", err)
	}
}

// TestNewSSHTool_NilExecutor_DefaultsToOS checks nil executor sets
// OSExecutor.
func TestNewSSHTool_NilExecutor_DefaultsToOS(t *testing.T) {
	tool := NewSSHTool("allow", nil)
	if tool.executor == nil {
		t.Error("want non-nil executor, got nil")
	}
}
