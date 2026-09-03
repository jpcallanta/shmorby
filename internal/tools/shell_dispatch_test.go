package tools

import (
	"strings"
	"testing"
)

// Table-driven dispatch for shellCommand helper.

func TestShellCommand_PowerShell(t *testing.T) {
	cmd := shellCommand("powershell.exe", "echo hi")

	if got := cmd.Args[0]; got != "powershell.exe" {
		t.Errorf("want Args[0] powershell.exe, got %q", got)
	}

	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "-NoProfile") {
		t.Errorf("want -NoProfile in %q", args)
	}

	if !strings.Contains(args, "-NonInteractive") {
		t.Errorf("want -NonInteractive in %q", args)
	}

	if !strings.Contains(args, "-Command") {
		t.Errorf("want -Command in %q", args)
	}

	if !strings.Contains(args, "exit $LASTEXITCODE") {
		t.Errorf("want exit wrap in %q", args)
	}
}

func TestShellCommand_Pwsh(t *testing.T) {
	cmd := shellCommand("pwsh", "echo hi")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "-NoProfile") {
		t.Errorf("want pwsh -NoProfile, got %q", args)
	}
}

func TestShellCommand_Cmd(t *testing.T) {
	cmd := shellCommand("cmd.exe", "dir")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "/d") {
		t.Errorf("want /d in %q", args)
	}

	if !strings.Contains(args, "/c") {
		t.Errorf("want /c in %q", args)
	}

	if strings.Contains(args, "-NoProfile") {
		t.Errorf("want no -NoProfile for cmd, got %q", args)
	}
}

func TestShellCommand_Bash(t *testing.T) {
	cmd := shellCommand("bash", "ls")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "-c") {
		t.Errorf("want -c in %q", args)
	}

	if strings.Contains(args, "-NoProfile") {
		t.Errorf("want no powershell flags, got %q", args)
	}
}

func TestShellCommand_UnknownFallback(t *testing.T) {
	cmd := shellCommand("myCustomShell", "do work")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "-c") {
		t.Errorf("want -c fallback, got %q", args)
	}
}

func TestShellCommand_CaseInsensitive(t *testing.T) {
	cmd := shellCommand("POWERSHELL.EXE", "echo hi")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "-NoProfile") {
		t.Errorf("want case-insensitive pwsh, got %q", args)
	}
}

func TestShellCommand_PathSubstring(t *testing.T) {
	// Full path containing powershell substring should use
	// PowerShell flags via default branch heuristic.
	cmd := shellCommand(
		"C:\\Tools\\my-powershell-helper.exe",
		"echo hi",
	)
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "-NoProfile") {
		t.Errorf("want powershell flags via substring, got %q", args)
	}
}

func TestShellCommand_CmdPath(t *testing.T) {
	cmd := shellCommand("C:\\Windows\\System32\\cmd.exe", "dir")
	args := strings.Join(cmd.Args, " ")

	if !strings.Contains(args, "/d") {
		t.Errorf("want /d via cmd path, got %q", args)
	}

	if !strings.Contains(args, "/c") {
		t.Errorf("want /c via cmd path, got %q", args)
	}
}

func TestWrapPowerShell_Idempotent(t *testing.T) {
	orig := "echo hi"
	wrapped := wrapPowerShell(orig)

	if !strings.Contains(wrapped, "$LASTEXITCODE") {
		t.Errorf("want wrap, got %q", wrapped)
	}

	double := wrapPowerShell(wrapped)
	if double != wrapped {
		t.Errorf("want idempotent, got %q vs %q", double, wrapped)
	}
}

func TestWrapPowerShell_AlreadyWrapped(t *testing.T) {
	cmd := "do work; exit $LASTEXITCODE"
	got := wrapPowerShell(cmd)

	if got != cmd {
		t.Errorf("want unchanged %q, got %q", cmd, got)
	}
}
