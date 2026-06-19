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

//go:embed ssh.txt
var sshDescription string

var sshParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"host": {
			"type": "string",
			"description": "remote hostname or IP"
		},
		"command": {
			"type": "string",
			"description": "command to execute on the remote host"
		},
		"user": {
			"type": "string",
			"description": "SSH user"
		},
		"port": {
			"type": "integer",
			"description": "SSH port (default: 22)"
		},
		"timeout_seconds": {
			"type": "integer",
			"description": "timeout in seconds (default: 120)"
		}
	},
	"required": ["host", "command"]
}`)

type sshArgs struct {
	Host           string `json:"host"`
	Command        string `json:"command"`
	User           string `json:"user,omitempty"`
	Port           int    `json:"port,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// SSHTool implements Tool for remote command execution via SSH.
type SSHTool struct {
	perm           string
	executor       Executor
	defaultTimeout int
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (s *SSHTool) SetDefaultTimeout(t int) {
	if t > 0 {
		s.defaultTimeout = t
	}
}

// Creates an SSHTool with the given permission level and executor.
// Pass nil executor to use the real OS executor.
func NewSSHTool(permLevel string, executor Executor) *SSHTool {
	if executor == nil {
		executor = OSExecutor{}
	}

	return &SSHTool{
		perm:           permLevel,
		executor:       executor,
		defaultTimeout: 120,
	}
}

// Returns the tool name.
func (s *SSHTool) Name() string { return "ssh" }

// Returns the embedded LLM description.
func (s *SSHTool) Description() string { return sshDescription }

// Returns the JSON schema for SSH parameters.
func (s *SSHTool) Parameters() json.RawMessage { return sshParams }

// PermLevel returns the configured permission level.
func (s *SSHTool) PermLevel() string { return s.perm }

// Parses args, executes SSH with timeout, truncates output, and logs
// audit info. Permission is enforced by the agent loop.
func (s *SSHTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a sshArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf("invalid ssh args: %w", err)
	}
	if a.Host == "" {
		return "", fmt.Errorf("ssh: missing required field \"host\"")
	}
	if a.Command == "" {
		return "", fmt.Errorf("ssh: missing required field \"command\"")
	}

	sshArgs := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if a.Port > 0 {
		sshArgs = append(sshArgs, "-p", fmt.Sprintf("%d", a.Port))
	}
	if a.User != "" {
		sshArgs = append(sshArgs, "-l", a.User)
	}
	sshArgs = append(sshArgs, a.Host, a.Command)

	timeout := s.defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = a.TimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()
	out, err := s.executor.Run(cmdCtx, "ssh", sshArgs...)
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
		"tool", "ssh",
		"host", a.Host,
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
