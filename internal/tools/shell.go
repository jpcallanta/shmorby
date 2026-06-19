package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"log/slog"
)

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
}

// SetDefaultTimeout sets the default timeout in seconds for this tool.
func (s *ShellTool) SetDefaultTimeout(t int) {
	if t > 0 {
		s.defaultTimeout = t
	}
}

// Creates a ShellTool with the given shell, workdir, and permission
// level. Empty shell defaults to $SHELL then bash.
// Empty workdir defaults to os.Getwd().
func NewShellTool(cfgShell, scopeWD, permLevel string) *ShellTool {
	sh := cfgShell
	if sh == "" {
		sh = os.Getenv("SHELL")
	}
	if sh == "" {
		sh = "bash"
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

// PermLevel returns the configured permission level.
func (s *ShellTool) PermLevel() string { return s.perm }

// Parses args, executes with timeout, truncates output, and logs
// audit info. Permission is enforced by the agent loop.
// Non-zero exits are returned as output text with appended exit code
// (not as Go errors).
func (s *ShellTool) Run(
	ctx context.Context, args json.RawMessage,
) (string, error) {
	var a shellArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return "", fmt.Errorf(
			"invalid shell args: %s - "+
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
		cwd = a.Cwd
	}
	timeout := s.defaultTimeout
	if a.TimeoutSeconds > 0 {
		timeout = a.TimeoutSeconds
	}

	cmdCtx, cancel := context.WithTimeout(ctx,
		time.Duration(timeout)*time.Second,
	)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, s.shell, "-c", a.Command)
	cmd.Dir = cwd

	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	// Determine exit code for audit and output.
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
		"tool", "shell",
		"duration_ms", elapsed.Milliseconds(),
		"exit", exitStr,
		"args", string(RedactArgs(args)),
	)

	truncated := TruncateOutput(out)

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
			// Non-zero exit: append exit code to output, return as
			// success so phase 6 gets the full result text.
			result := string(truncated)
			if result != "" && !strings.HasSuffix(result, "\n") {
				result += "\n"
			}
			result += fmt.Sprintf("exit code: %d", exitErr.ExitCode())

			return result, nil
		}
		// Other error (timeout, exec failure, signal kill):
		// return as error.
		return string(truncated), err
	}

	return string(truncated), nil
}
