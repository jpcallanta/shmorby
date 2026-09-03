package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"shmorby/internal/audit"
	cmdexec "shmorby/internal/exec"
	"shmorby/internal/health"
)

// ExecTool holds the shared fields and behavior for command-execution
// tools (ssh, sudo, aws). Embedding it provides SetDefaultTimeout and
// access to perm/executor/defaultTimeout/auditLogger without repeating
// the struct layout in each tool.
type ExecTool struct {
	perm           string
	executor       cmdexec.Executor
	defaultTimeout int
	auditLogger    *audit.Logger
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (e *ExecTool) SetDefaultTimeout(t int) {
	if t > 0 {
		e.defaultTimeout = t
	}
}

// SetAuditLogger assigns the audit logger for this tool.
func (e *ExecTool) SetAuditLogger(l *audit.Logger) {
	e.auditLogger = l
}

// formatExitStatus renders an error as an exit-status string for slog.
// Returns "0" on success, the integer exit code for an ExitError, or
// "error: <detail>" for any other error.
func formatExitStatus(err error) string {
	if err == nil {
		return "0"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Sprintf("%d", exitErr.ExitCode())
	}
	return fmt.Sprintf("error: %v", err)
}

// extractExitCode returns the integer exit code from an ExitError,
// or 0 when err is nil or not an ExitError.
func extractExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 0
}

// logAuditEvent writes a tool-run entry to the audit logger.
// Skips silently when auditLogger is nil. The caller supplies the
// tool name and output capture (which varies between tools — e.g.
// shell captures stderr separately).
func logAuditEvent(
	auditLogger *audit.Logger,
	ctx context.Context,
	toolName string,
	args []byte,
	elapsed time.Duration,
	err error,
	outBytes []byte,
	capture *audit.OutputCapture,
) {
	if auditLogger == nil {
		return
	}

	exitCode := extractExitCode(err)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	auditLogger.LogToolRun(
		audit.AuditEntry{
			SessionID:  SessionIDFrom(ctx),
			Tool:       toolName,
			Args:       string(RedactArgs(args)),
			DurationMs: elapsed.Milliseconds(),
			ExitCode:   &exitCode,
			Error:      errMsg,
		},
		capture,
	)
}

// exitCodeResult appends "exit code: N" to result when err is a
// non-zero ExitError, returning (result, nil). For other errors it
// returns a health-wrapped error with the tool-specific message.
// toolName is the metrics label and error prefix (e.g. "ssh", "sudo").
func exitCodeResult(
	result string,
	err error,
	elapsed time.Duration,
	toolName string,
) (string, error) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		if result != "" && !strings.HasSuffix(result, "\n") {
			result += "\n"
		}
		result += fmt.Sprintf("exit code: %d", exitErr.ExitCode())
		return result, nil
	}

	return result, health.Wrap(
		toolName, elapsed, fmt.Errorf("%s exec: %w", toolName, err),
	)
}

// resolvePath confines a file path to the project root, or falls back
// to CheckPathSafety when no root is configured. The toolName prefixes
// the error wrapper (e.g. "file_read", "file_write").
func resolvePath(root *ProjectRoot, path, toolName string) (string, error) {
	if root != nil {
		resolved, err := root.CheckPathConfinement(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", toolName, err)
		}
		return resolved, nil
	}

	if err := CheckPathSafety(path); err != nil {
		return "", fmt.Errorf("%s: %w", toolName, err)
	}
	return path, nil
}
