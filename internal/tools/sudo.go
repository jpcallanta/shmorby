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

// Parses args, executes sudo -n with timeout, truncates output, and
// logs audit info. Permission is enforced by the agent loop.
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

	truncated := TruncateOutput(out)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
			result := string(truncated)
			if result != "" && !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += fmt.Sprintf("exit code: %d",
				exitErr.ExitCode())

			return result, nil
		}

		return string(truncated), err
	}

	return string(truncated), nil
}
