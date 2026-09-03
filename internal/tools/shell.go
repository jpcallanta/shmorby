package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"shmorby/internal/audit"
	cmdexec "shmorby/internal/exec"
	"shmorby/internal/health"
	"shmorby/internal/xdg"
)

// Hard ceiling on per-call timeout_seconds to prevent resource
// exhaustion. A single tool call with timeout=86400 would hold a
// process slot, file descriptors, and pipe buffers for 24 hours.
// This ceiling is enforced in shell, ssh, sudo, and aws.
const maxTimeoutSeconds = 3600 // 1 hour

//go:embed shell.txt
var shellDescription string

var shellParams = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "shell command to execute"
		},
		"cwd": {
			"type": "string",
			"description": "working directory (default: scope workdir or process cwd)"
		},
		"timeout_seconds": {
			"type": "integer",
			"description": "timeout in seconds (default: 120)"
		}
	},
	"required": ["command"]
}`)

type shellArgs struct {
	Command        string `json:"command"`
	Cwd            string `json:"cwd,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// ShellTool implements Tool for shell command execution.
type ShellTool struct {
	shell          string
	workdir        string
	perm           string
	defaultTimeout int
	auditLogger    *audit.Logger
}

// Sets the default timeout in seconds for this tool.
func (s *ShellTool) SetDefaultTimeout(t int) {
	if t > 0 {
		s.defaultTimeout = t
	}
}

// Creates a ShellTool with the given shell, workdir,
// and permission level. Empty shell defaults to the OS-preferred
// shell via xdg.DefaultShell(). Empty workdir defaults to
// os.Getwd().
func NewShellTool(
	cfgShell, scopeWD, permLevel string,
) *ShellTool {
	sh := cfgShell
	if sh == "" {
		sh = xdg.DefaultShell()
	}
	wd := scopeWD
	if wd == "" {
		wd, _ = os.Getwd()
	}

	return &ShellTool{
		shell:          sh,
		workdir:        wd,
		perm:           permLevel,
		defaultTimeout: 120,
	}
}

// Returns the tool name.
func (s *ShellTool) Name() string { return "shell" }

// Returns the embedded LLM description.
func (s *ShellTool) Description() string { return shellDescription }

// Returns the JSON schema for shell parameters.
func (s *ShellTool) Parameters() json.RawMessage { return shellParams }

// Returns the configured permission level.
func (s *ShellTool) PermLevel() string { return s.perm }

// Updates the permission level at runtime.
func (s *ShellTool) SetPerm(level string) { s.perm = level }

// Sets the audit logger for this tool.
func (s *ShellTool) SetAuditLogger(l *audit.Logger) { s.auditLogger = l }

// Updates the shell binary path at runtime.
func (s *ShellTool) SetShell(path string) { s.shell = path }

// Parses args, executes with timeout, truncates output, and redacts
// secrets. Permission is enforced by the agent loop.
// Non-zero exits are returned as output text with appended exit code
// (not as Go errors).
//
// Execution uses shellCommand() to build an OS-appropriate
// exec.Cmd (supporting cwd and PowerShell/CMD/bash dispatch),
// then delegates pipe reads and process-group management to the
// shared internal/exec package. The audit logger receives
// separate stdout/stderr streams for structured capture.
func (s *ShellTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a shellArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf(
			"invalid shell args: %w - "+
				`expected {"command":"<cmd>","cwd":"<dir>",`+
				`"timeout_seconds":<int>}`,
			err,
		)
	}
	if a.Command == "" {
		return "", fmt.Errorf(
			`invalid shell args: missing required field "command"`,
		)
	}

	cwd := s.workdir
	if a.Cwd != "" {
		// Validate the model-supplied cwd up front: a nonexistent
		// path or a non-directory otherwise surfaces as a raw OS
		// error from cmd.Start(). os.Stat follows symlinks, so a
		// symlinked cwd is accepted iff its target is a usable
		// directory.
		info, err := os.Stat(a.Cwd)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("shell: invalid cwd: %q", a.Cwd)
		}
		cwd = a.Cwd
	}
	timeout := s.defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = a.TimeoutSeconds
	}
	// Clamp to a hard ceiling to prevent resource exhaustion
	// via unbounded timeout (e.g. 86400s).
	if timeout > maxTimeoutSeconds {
		timeout = maxTimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	// Build OS-appropriate command with shell dispatch
	// (PowerShell, CMD, bash, etc.) and set working directory.
	cmd := shellCommand(s.shell, a.Command)
	cmd.Dir = cwd
	cmdexec.SetupProcessGroup(cmd)

	start := time.Now()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", health.Wrap("shell", time.Since(start),
			fmt.Errorf("stdout pipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", health.Wrap("shell", time.Since(start),
			fmt.Errorf("stderr pipe: %w", err))
	}

	if err := cmd.Start(); err != nil {
		return "", health.Wrap("shell", time.Since(start),
			fmt.Errorf("start: %w", err))
	}

	done := make(chan struct{})

	go func() {
		select {
		case <-done:
			return
		case <-cmdCtx.Done():
			select {
			case <-done:
				return
			default:
			}

			if cmd.Process != nil {
				cmdexec.KillProcessGroup(cmd.Process.Pid)
			}
		}
	}()

	var outBuf, errBuf strings.Builder
	readDone := make(chan error, 2)

	// LimitReader caps buffered bytes at the hard buffer limit so a
	// runaway command cannot exhaust memory before TruncateOutput
	// runs; TruncateOutput still applies the configured MaxOutput
	// cap afterwards.
	go func() {
		_, err := io.Copy(&outBuf,
			cmdexec.NewContextReader(
				io.LimitReader(stdout, cmdexec.MaxOutputBufferBytes),
				cmdCtx,
			))
		readDone <- err
	}()
	go func() {
		_, err := io.Copy(&errBuf,
			cmdexec.NewContextReader(
				io.LimitReader(stderr, cmdexec.MaxOutputBufferBytes),
				cmdCtx,
			))
		readDone <- err
	}()

	for i := 0; i < 2; i++ {
		if rErr := <-readDone; rErr != nil &&
			!errors.Is(rErr, context.Canceled) {
			slog.Warn("pipe read error", "err", rErr)
		}
	}

	waitErr := cmd.Wait()
	close(done)
	elapsed := time.Since(start)

	out := []byte(outBuf.String() + errBuf.String())

	// Flag the hard buffer cap so a consumer sees the truncation
	// even when MaxOutput is 0 (unlimited).
	if int64(outBuf.Len()) >= cmdexec.MaxOutputBufferBytes ||
		int64(errBuf.Len()) >= cmdexec.MaxOutputBufferBytes {
		out = append(out, fmt.Sprintf(
			"\n... (stream capped at %d bytes)",
			cmdexec.MaxOutputBufferBytes,
		)...)
	}

	slog.Info("tool run",
		"tool", ToolShell,
		"duration_ms", elapsed.Milliseconds(),
		"exit", formatExitStatus(waitErr),
		"args", string(RedactArgs(args)),
	)

	logAuditEvent(s.auditLogger, ctx, ToolShell, args, elapsed, waitErr, nil,
		&audit.OutputCapture{
			SessionID:  SessionIDFrom(ctx),
			Stdout:     outBuf.String(),
			Stderr:     errBuf.String(),
			StdoutSize: outBuf.Len(),
			StderrSize: errBuf.Len(),
			Checksum:   audit.ComputeChecksum(outBuf.String() + errBuf.String()),
		},
	)

	result := string(TruncateOutput(out))

	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return result, health.Wrap(ToolShell, elapsed,
			fmt.Errorf("shell exec timed out after %ds: %w",
				timeout, cmdCtx.Err()),
		)
	}
	if errors.Is(cmdCtx.Err(), context.Canceled) {
		return result, health.Wrap(ToolShell, elapsed,
			fmt.Errorf("shell exec cancelled: %w", cmdCtx.Err()),
		)
	}
	if waitErr != nil {
		return exitCodeResult(result, waitErr, elapsed, ToolShell)
	}

	return result, nil
}

// Builds an exec.Cmd with OS-appropriate flags for the
// configured shell.
func shellCommand(shell, command string) *exec.Cmd {
	base := strings.ToLower(filepath.Base(shell))
	base = strings.TrimSuffix(base, ".exe")

	switch base {
	case "pwsh", "powershell":
		cmdStr := wrapPowerShell(command)

		return exec.Command(
			shell, "-NoProfile",
			"-NonInteractive", "-Command", cmdStr,
		)
	case "cmd":
		return exec.Command(shell, "/d", "/c", command)
	case "bash", "sh":
		return exec.Command(shell, "-c", command)
	default:
		lower := strings.ToLower(shell)

		if strings.Contains(lower, "powershell") ||
			strings.Contains(lower, "pwsh") {
			cmdStr := wrapPowerShell(command)

			return exec.Command(
				shell, "-NoProfile",
				"-NonInteractive", "-Command", cmdStr,
			)
		}

		if strings.Contains(lower, "cmd.exe") {
			return exec.Command(shell, "/d", "/c", command)
		}

		return exec.Command(shell, "-c", command)
	}
}

// Wraps PowerShell command to propagate native exit code.
func wrapPowerShell(cmd string) string {
	if strings.Contains(cmd, "$LASTEXITCODE") {
		return cmd
	}

	return cmd + "; exit $LASTEXITCODE"
}
