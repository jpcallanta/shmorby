package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestOSExecutor_Run_Echo verifies basic command execution.
func TestOSExecutor_Run_Echo(t *testing.T) {
	var exe OSExecutor
	out, err := exe.Run(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(out) != "hello\n" {
		t.Errorf("want %q, got %q", "hello\n", string(out))
	}
}

// TestOSExecutor_Run_ExitNonZero verifies non-zero exit returns
// error but still provides output.
func TestOSExecutor_Run_ExitNonZero(t *testing.T) {
	var exe OSExecutor
	ctx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()

	out, err := exe.Run(ctx, "sh", "-c", "echo fail; exit 1")
	if err == nil {
		t.Fatal("want error for exit 1")
	}
	if len(out) == 0 {
		t.Error("want output despite error")
	}
}

// TestOSExecutor_Run_ContextCancel verifies context cancellation
// kills the process and returns a degraded error.
func TestOSExecutor_Run_ContextCancel(t *testing.T) {
	var exe OSExecutor
	ctx, cancel := context.WithTimeout(
		context.Background(), 100*time.Millisecond,
	)
	defer cancel()

	_, err := exe.Run(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("want error for cancelled context")
	}
}

// TestOSExecutor_Run_NotFound verifies missing binary returns
// error.
func TestOSExecutor_Run_NotFound(t *testing.T) {
	var exe OSExecutor
	_, err := exe.Run(
		context.Background(), "nonexistent-binary-xyz", "",
	)
	if err == nil {
		t.Fatal("want error for missing binary")
	}
}

// TestOSExecutor_Run_BufferCap_Notice verifies oversized output is
// capped at the buffer limit with a visible notice instead of
// growing memory without bound.
func TestOSExecutor_Run_BufferCap_Notice(t *testing.T) {
	old := MaxOutputBufferBytes
	MaxOutputBufferBytes = 1024
	defer func() { MaxOutputBufferBytes = old }()

	var exe OSExecutor
	out, err := exe.Run(context.Background(), "sh", "-c",
		"dd if=/dev/zero bs=1024 count=8 2>/dev/null | tr '\\0' 'x'")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(string(out), "stream capped") {
		t.Errorf("want capped notice in output, got %d bytes",
			len(out))
	}
	if len(out) > 1024+len("\n... (stream capped at 1024 bytes)") {
		t.Errorf("want output within cap+notice, got %d bytes", len(out))
	}
}
