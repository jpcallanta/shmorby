package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"shmorby/internal/audit"
	cmdexec "shmorby/internal/exec"
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
	ExecTool
}

// Creates an SSHTool with the given permission level and executor.
// Pass nil executor to use the real OS executor.
func NewSSHTool(permLevel string, executor cmdexec.Executor) *SSHTool {
	if executor == nil {
		executor = cmdexec.OSExecutor{}
	}

	return &SSHTool{
		ExecTool: ExecTool{
			perm:           permLevel,
			executor:       executor,
			defaultTimeout: 120,
		},
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

// SetPerm updates the permission level at runtime.
func (s *SSHTool) SetPerm(level string) { s.perm = level }

// Parses args, executes SSH with timeout, truncates output, and
// redacts secrets. Permission is enforced by the agent loop.
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
	// A nonzero port outside 1-65535 can never connect; reject it
	// with a clean tool-level error instead of spawning ssh and
	// surfacing its confusing failure. Port 0 means unset.
	if a.Port != 0 && (a.Port < 1 || a.Port > 65535) {
		return "", fmt.Errorf(
			"ssh: port must be 1-65535, got %d", a.Port,
		)
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
	// SECURITY: "--" terminates option parsing so a Host
	// starting with "-" is treated as a hostname, not an SSH flag.
	// Without this, a Host like "-oProxyCommand=malicious" would
	// be interpreted as an SSH option (CWE-88 / CWE-77).
	sshArgs = append(sshArgs, "--", a.Host, a.Command)

	timeout := s.defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = a.TimeoutSeconds
	}
	// Clamp to hard ceiling to prevent resource exhaustion.
	if timeout > maxTimeoutSeconds {
		timeout = maxTimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	start := time.Now()
	out, err := s.executor.Run(cmdCtx, "ssh", sshArgs...)
	elapsed := time.Since(start)

	slog.Info("tool run",
		"tool", ToolSSH,
		"host", a.Host,
		"duration_ms", elapsed.Milliseconds(),
		"exit", formatExitStatus(err),
		"args", string(RedactArgs(args)),
	)

	logAuditEvent(s.auditLogger, ctx, ToolSSH, args, elapsed, err, out,
		&audit.OutputCapture{
			SessionID:  SessionIDFrom(ctx),
			Stdout:     string(out),
			StdoutSize: len(out),
			Checksum:   audit.ComputeChecksum(string(out)),
		},
	)

	result := string(TruncateOutput(out))

	if err != nil {
		return exitCodeResult(result, err, elapsed, ToolSSH)
	}

	return result, nil
}
