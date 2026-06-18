package navigation

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Executor runs shell commands (same interface as tools.Executor).
type Executor interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// OSExecutor uses the real os/exec package.
type OSExecutor struct{}

// Run executes a command and returns combined output.
func (OSExecutor) Run(
	ctx context.Context, name string, args ...string,
) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Output holds the result of a shell command execution.
type Output struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ShellCmdHandler handles !-prefixed shell commands.
type ShellCmdHandler struct {
	executor    Executor
	lastCommand string
	shell       string
	mode        string
	permLevel   string
}

// NewShellCmdHandler creates a handler for ! commands.
func NewShellCmdHandler(
	executor Executor, shell, mode, permLevel string,
) *ShellCmdHandler {
	if shell == "" {
		shell = "bash"
	}
	return &ShellCmdHandler{
		executor:  executor,
		shell:     shell,
		mode:      mode,
		permLevel: permLevel,
	}
}

// SetMode updates the current agent mode.
func (h *ShellCmdHandler) SetMode(mode string) {
	h.mode = mode
}

// mutatingCmds lists command prefixes that modify system state.
var mutatingCmds = []string{
	"rm ", "rmdir", "mv ", "cp ", "chmod", "chown",
	"kill", "pkill", "reboot", "shutdown",
	"apt ", "apt-get", "yum ", "dnf ", "pacman",
	"systemctl", "service", "mount", "umount",
	"dd ", "mkfs", "fdisk", "parted",
	"iptables", "nft ",
}

// Handle processes a !-prefixed input line.
// Returns (handled, output, error). Returns handled=false for non-! input.
func (h *ShellCmdHandler) Handle(input string) (bool, Output, error) {
	if !strings.HasPrefix(input, "!") {
		return false, Output{}, nil
	}
	cmd := strings.TrimPrefix(input, "!")
	if cmd == "" || cmd == "!" {
		// "!" or "!!" repeat last
		if h.lastCommand == "" {
			return true, Output{},
				fmt.Errorf("no previous command to repeat")
		}
		cmd = h.lastCommand
	} else {
		h.lastCommand = cmd
	}
	// Deny mutating commands in diagnose mode.
	if h.mode == "diagnose" {
		for _, m := range mutatingCmds {
			if strings.HasPrefix(cmd, m) {
				return true, Output{},
					fmt.Errorf("denied in diagnose mode: %s", cmd)
			}
		}
	}
	// Permission check (same rules as shell tool).
	if h.permLevel == "deny" {
		return true, Output{},
			fmt.Errorf("tool: permission denied")
	}
	out, err := h.executor.Run(
		context.Background(), h.shell, "-c", cmd,
	)
	exitCode := 0
	var stderr string
	if err != nil {
		exitCode = 1
		stderr = err.Error()
	}
	return true, Output{
		Stdout:   string(out),
		Stderr:   stderr,
		ExitCode: exitCode,
	}, nil
}

// LastCommand returns the last executed ! command.
func (h *ShellCmdHandler) LastCommand() string {
	return h.lastCommand
}

// FormatOutput formats the shell command output for display.
func FormatOutput(out Output) string {
	var b strings.Builder
	if out.Stdout != "" {
		b.WriteString(out.Stdout)
	}
	if out.Stderr != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(out.Stderr)
	}
	if out.ExitCode != 0 {
		b.WriteString(fmt.Sprintf("\nexit code: %d", out.ExitCode))
	}
	return b.String()
}
