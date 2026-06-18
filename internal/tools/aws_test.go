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

// TestAWSTool_Name_ReturnsAWS checks Name returns "aws".
func TestAWSTool_Name_ReturnsAWS(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	if tool.Name() != "aws" {
		t.Errorf("want 'aws', got %q", tool.Name())
	}
}

// TestAWSTool_Description_NotEmpty checks Description is non-empty.
func TestAWSTool_Description_NotEmpty(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	if tool.Description() == "" {
		t.Error("want non-empty description")
	}
}

// TestAWSTool_Parameters_ValidJSON checks Parameters is valid JSON.
func TestAWSTool_Parameters_ValidJSON(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	var v interface{}
	if err := json.Unmarshal(tool.Parameters(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestAWSTool_Run_ExecutesAWSCommand checks aws tool runs aws CLI.
func TestAWSTool_Run_ExecutesAWSCommand(t *testing.T) {
	mock := &mockExecutor{
		Out: []byte(`{"Instances":[]}`),
	}
	tool := NewAWSTool("allow", mock)
	args := []byte(
		`{"args":["ec2","describe-instances","--region","us-east-1"]}`,
	)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "Instances") {
		t.Errorf("want output containing 'Instances', got %q", out)
	}
	if mock.Name != "aws" {
		t.Errorf("want aws command, got %q", mock.Name)
	}
	if len(mock.Args) < 2 {
		t.Fatalf("want 2+ args, got %d", len(mock.Args))
	}
	if mock.Args[0] != "ec2" {
		t.Errorf("want first arg 'ec2', got %q", mock.Args[0])
	}
}

// TestAWSTool_Run_MissingArgs_ReturnsError checks missing args.
func TestAWSTool_Run_MissingArgs_ReturnsError(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatal("want error for missing args, got nil")
	}
}

// TestAWSTool_Run_EmptyArgs_ReturnsError checks empty args array.
func TestAWSTool_Run_EmptyArgs_ReturnsError(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"args":[]}`),
	)
	if err == nil {
		t.Fatal("want error for empty args, got nil")
	}
}

// TestAWSTool_Run_Deny_ReturnsError checks deny blocks.
func TestAWSTool_Run_Deny_ReturnsError(t *testing.T) {
	mock := &mockExecutor{Out: []byte("should-not-run")}
	tool := NewAWSTool("deny", mock)
	_, err := tool.Run(
		context.Background(),
		[]byte(`{"args":["s3","ls"]}`),
	)
	if err == nil {
		t.Fatal("want error for deny, got nil")
	}
}

// TestAWSTool_Run_ExecError_ReturnsOutput checks exec error preserves
// output.
func TestAWSTool_Run_ExecError_ReturnsOutput(t *testing.T) {
	mock := &mockExecutor{
		Out:    []byte("An error occurred"),
		RunErr: fmt.Errorf("exit status 1"),
	}
	tool := NewAWSTool("allow", mock)
	args := []byte(`{"args":["ec2","describe-instances"]}`)

	out, err := tool.Run(context.Background(), args)
	if err == nil {
		t.Fatal("want error for exec failure, got nil")
	}
	if !strings.Contains(out, "An error occurred") {
		t.Errorf("want output preserved, got %q", out)
	}
}

// TestAWSTool_Run_NonZeroExit_ReturnsOutput checks non-zero exit
// returns output with exit code (no Go error).
func TestAWSTool_Run_NonZeroExit_ReturnsOutput(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 1")
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatal("failed to create ExitError")
	}

	mock := &mockExecutor{
		Out:    []byte("An error occurred"),
		RunErr: exitErr,
	}
	tool := NewAWSTool("allow", mock)
	args := []byte(`{"args":["ec2","describe-instances"]}`)

	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("want nil for non-zero exit, got %v", err)
	}
	if !strings.Contains(out, "An error occurred") {
		t.Errorf("want output preserved, got %q", out)
	}
	if !strings.Contains(out, "exit code: 1") {
		t.Errorf("want 'exit code: 1' in output, got %q", out)
	}
}

// TestAWSTool_Run_InvalidJSON_ReturnsError checks bad JSON.
func TestAWSTool_Run_InvalidJSON_ReturnsError(t *testing.T) {
	tool := NewAWSTool("allow", nil)
	_, err := tool.Run(context.Background(), []byte(`not json`))
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}
