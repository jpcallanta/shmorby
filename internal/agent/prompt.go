package agent

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed prompts/operate.txt
var operatePrompt string

//go:embed prompts/diagnose.txt
var diagnosePrompt string

//go:embed prompts/max_steps.txt
var MaxStepsPrompt string

// SystemPrompt builds the system prompt for the given mode.
//
// mode: "operate" or "diagnose"
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
	default:
		return "", fmt.Errorf("invalid agent mode %q (want operate|diagnose)", mode)
	}

	// Override replaces the embed body only; scope appendix stays.
	if override != "" {
		content, err := os.ReadFile(override)
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
