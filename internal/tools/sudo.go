package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"shmorby/internal/audit"
)

//go:embed sudo.txt
var sudoDescription string

var sudoParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "command to execute with sudo"
		},
		"timeout_seconds": {
			"type": "integer",
			"description": "timeout in seconds (default: 120)"
		}
	},
	"required": ["command"]
}`)

type sudoArgs struct {
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// SudoTool implements Tool for sudo -n command execution.
type SudoTool struct {
	perm           string
	executor       Executor
	defaultTimeout int
	auditLogger    *audit.Logger
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (s *SudoTool) SetDefaultTimeout(t int) {
	if t > 0 {
		s.defaultTimeout = t
	}
}

// Creates a SudoTool with the given permission level and executor.
// Pass nil executor to use the real OS executor.
func NewSudoTool(permLevel string, executor Executor) *SudoTool {
	if executor == nil {
		executor = OSExecutor{}
	}

	return &SudoTool{
		perm:           permLevel,
		executor:       executor,
		defaultTimeout: 120,
	}
}

// Returns the tool name.
func (s *SudoTool) Name() string { return "sudo" }

// Returns the embedded LLM description.
func (s *SudoTool) Description() string { return sudoDescription }

// Returns the JSON schema for sudo parameters.
func (s *SudoTool) Parameters() json.RawMessage { return sudoParams }

// PermLevel returns the configured permission level.
func (s *SudoTool) PermLevel() string { return s.perm }

// SetPerm updates the permission level at runtime.
func (s *SudoTool) SetPerm(level string) { s.perm = level }

// SetAuditLogger sets the audit logger for this tool.
func (s *SudoTool) SetAuditLogger(l *audit.Logger) { s.auditLogger = l }

// Parses args, executes sudo -n with timeout, truncates output, and
// redacts secrets. Permission is enforced by the agent loop.
func (s *SudoTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a sudoArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid sudo args: %w", err)
	}
	if a.Command == "" {
		return "", fmt.Errorf(
			`sudo: missing required field "command"`,
		)
	}

	timeout := s.defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = a.TimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()
	out, err := s.executor.Run(
		cmdCtx, "sudo", "-n", "sh", "-c", a.Command,
	)
	elapsed := time.Since(start)

	exitStr := "0"
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitStr = fmt.Sprintf("%d", exitErr.ExitCode())
		} else {
			exitStr = fmt.Sprintf("error: %v", err)
		}
	}

	slog.Info("tool run",
		"tool", "sudo",
		"duration_ms", elapsed.Milliseconds(),
		"exit", exitStr,
		"args", string(RedactArgs(args)),
	)

	if s.auditLogger != nil {
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			}
		}
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		s.auditLogger.LogToolRun(
			audit.AuditEntry{
				SessionID:  SessionIDFrom(ctx),
				Tool:       "sudo",
				Args:       string(RedactArgs(args)),
				DurationMs: elapsed.Milliseconds(),
				ExitCode:   &exitCode,
				Error:      errMsg,
			},
			&audit.OutputCapture{
				SessionID:  SessionIDFrom(ctx),
				Stdout:     string(out),
				StdoutSize: len(out),
				Checksum:   audit.ComputeChecksum(string(out)),
			},
		)
	}

	result := string(TruncateOutput(out))

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
			if result != "" && !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += fmt.Sprintf("exit code: %d",
				exitErr.ExitCode())

			return result, nil
		}

		return result, fmt.Errorf("sudo exec: %w", err)
	}

	return result, nil
}
