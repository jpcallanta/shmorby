package agent

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"shmorby/internal/fileread"
	"shmorby/internal/xdg"
)

//go:embed prompts/operate.txt
var operatePrompt string

//go:embed prompts/diagnose.txt
var diagnosePrompt string

//go:embed prompts/chat.txt
var chatPrompt string

//go:embed prompts/code.txt
var codePrompt string

//go:embed prompts/max_steps.txt
var MaxStepsPrompt string

// SystemPrompt builds the system prompt for the given mode.
//
// mode: "operate", "diagnose", "chat", or "code"
// scope: scope content to append
// override: path to a file that replaces the embed body (optional);
//
//	the scope appendix is still appended regardless of override.
//
// projectRoot: resolved absolute path to the project root directory
//
// (reported in the environment hint so the model knows its working
// directory). Empty string uses the default xdg workdir.
//
// The override file, if provided, replaces only the embedded prompt body;
// the scope appendix is always appended.
func SystemPrompt(mode, scope, override, projectRoot string) (string, error) {
	var body string

	// Determine the base prompt body.
	switch mode {
	case "operate":
		body = operatePrompt
	case "diagnose":
		body = diagnosePrompt
	case "chat":
		body = chatPrompt
	case "code":
		body = codePrompt
	default:
		return "", fmt.Errorf(
			"invalid agent mode %q (want operate|"+
				"diagnose|chat|code)", mode,
		)
	}

	// Override replaces the embed body only; scope appendix stays.
	if override != "" {
		// Use size-limited read to prevent OOM from oversized files.
		content, err := fileread.ReadFileLimited(override, 0)
		if err != nil {
			return "", fmt.Errorf("read system-prompt file %q: %w", override, err)
		}
		body = string(content)
	}

	// Inject OS/shell hint so the model emits shell-appropriate
	// commands (PowerShell vs bash). Include the project root so
	// the model knows its working directory.
	body = body + "\n\n" + envHint(projectRoot)

	// Append scope appendix if scope content provided.
	if scope != "" {
		return fmt.Sprintf("%s\n\n## Scope Context\n\n%s", body, scope), nil
	}

	return body, nil
}

// Builds an OS/shell environment block for prompt injection.
// projectRoot is the resolved project directory; empty falls back
// to the default xdg workdir.
func envHint(projectRoot string) string {
	osName := runtime.GOOS
	shell := xdg.DefaultShell()
	base := strings.ToLower(filepath.Base(shell))

	hint := ""

	switch {
	case strings.Contains(base, "pwsh") ||
		strings.Contains(base, "powershell"):
		hint = "Use PowerShell: Get-ChildItem not ls, " +
			"Get-Content not cat, Invoke-WebRequest not " +
			"curl, Get-Process not ps."
	case strings.Contains(base, "cmd"):
		hint = "Use cmd.exe: dir not ls, type not cat; " +
			"prefer PowerShell for HTTP."
	default:
		hint = "Use bash: ls, cat, curl, ps aux, systemctl."
	}

	workdir := projectRoot
	if workdir == "" {
		workdir = xdg.DefaultWorkDir()
	}

	return fmt.Sprintf(
		"## Environment\nOS: %s\nShell: %s\nWorkdir: %s\nHint: %s\n",
		osName, shell, workdir, hint,
	)
}
