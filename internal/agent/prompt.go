package agent

import (
	_ "embed"
	"fmt"

	"shmorby/internal/fileread"
)

//go:embed prompts/operate.txt
var operatePrompt string

//go:embed prompts/diagnose.txt
var diagnosePrompt string

//go:embed prompts/chat.txt
var chatPrompt string

//go:embed prompts/max_steps.txt
var MaxStepsPrompt string

// SystemPrompt builds the system prompt for the given mode.
//
// mode: "operate", "diagnose", or "chat"
// scope: scope content to append
// override: path to a file that replaces the embed body (optional);
//
//	the scope appendix is still appended regardless of override.
//
// The override file, if provided, replaces only the embedded prompt body;
// the scope appendix is always appended.
func SystemPrompt(mode, scope, override string) (string, error) {
	var body string

	// Determine the base prompt body.
	switch mode {
	case "operate":
		body = operatePrompt
	case "diagnose":
		body = diagnosePrompt
	case "chat":
		body = chatPrompt
	default:
		return "", fmt.Errorf("invalid agent mode %q (want operate|diagnose|chat)", mode)
	}

	// Override replaces the embed body only; scope appendix stays.
	if override != "" {
		// Use size-limited read to prevent OOM from oversized files (issue #46).
		content, err := fileread.ReadFileLimited(override, 0)
		if err != nil {
			return "", fmt.Errorf("read system-prompt file %q: %w", override, err)
		}
		body = string(content)
	}

	// Append scope appendix if scope content provided.
	if scope != "" {
		return fmt.Sprintf("%s\n\n## Scope Context\n\n%s", body, scope), nil
	}

	return body, nil
}
