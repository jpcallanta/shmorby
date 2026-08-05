package agent

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	// statusMaxLen truncates descriptions to this length.
	statusMaxLen = 60
)

// StatusGenerator produces short, present-tense descriptions of the
// current tool action. It generates descriptions directly from tool
// metadata and arguments without any LLM call.
type StatusGenerator struct{}

// NewStatusGenerator creates a generator. The provider parameter is
// accepted for API compatibility but ignored — descriptions are
// generated from tool metadata.
func NewStatusGenerator(_ interface{}, _ string) *StatusGenerator {
	return &StatusGenerator{}
}

// Generate produces a short (≤60 chars), present-tense description of
// the tool action based on the tool name and its parsed arguments.
func (g *StatusGenerator) Generate(
	_ context.Context, toolName, description, command string,
) string {
	if g == nil {
		return ""
	}
	return describeTool(toolName, command)
}

// describeTool returns a short, present-tense description based on
// tool name and the raw command/args string.
func describeTool(toolName, command string) string {
	switch toolName {
	case "shell", "sudo":
		return describeShellCommand(command)
	case "ssh":
		return describeSSHCommand(command)
	case "aws":
		return describeAWSCommand(command)
	case "find":
		return describeFindCommand(command)
	case "websearch":
		return describeWebSearch(command)
	case "webfetch":
		return describeWebFetch(command)
	case "task":
		return describeTask(command)
	default:
		return describeGeneric(toolName, command)
	}
}

// describeShellCommand generates a description for shell/sudo commands.
func describeShellCommand(command string) string {
	if command == "" {
		return "running shell command"
	}
	cmd := firstToken(command)
	if cmd == "" {
		return "running shell command"
	}
	return truncate("running " + summarizeCommand(command))
}

// describeSSHCommand generates a description for SSH commands.
func describeSSHCommand(command string) string {
	if command == "" {
		return "running remote command"
	}
	return truncate("ssh: " + summarizeCommand(command))
}

// describeAWSCommand generates a description for AWS CLI commands.
func describeAWSCommand(command string) string {
	if command == "" {
		return "running aws command"
	}
	// extractCommand already formats as "aws <args>", so use directly.
	return truncate(command)
}

// describeFindCommand generates a description for find/glob commands.
func describeFindCommand(command string) string {
	// Try to extract pattern from JSON args.
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if json.Unmarshal([]byte(command), &args) == nil {
		if args.Pattern != "" {
			if args.Path != "" {
				return truncate("finding " + args.Pattern + " in " + args.Path)
			}
			return truncate("finding " + args.Pattern)
		}
	}
	return "searching for files"
}

// describeWebSearch generates a description for web search commands.
func describeWebSearch(command string) string {
	var args struct {
		Query string `json:"query"`
	}
	if json.Unmarshal([]byte(command), &args) == nil && args.Query != "" {
		return truncate("searching: " + args.Query)
	}
	return "searching the web"
}

// describeWebFetch generates a description for web fetch commands.
func describeWebFetch(command string) string {
	var args struct {
		URL string `json:"url"`
	}
	if json.Unmarshal([]byte(command), &args) == nil && args.URL != "" {
		return truncate("fetching " + args.URL)
	}
	return "fetching url"
}

// describeTask generates a description for task orchestration.
func describeTask(command string) string {
	var args struct {
		Description string `json:"description"`
	}
	if json.Unmarshal([]byte(command), &args) == nil && args.Description != "" {
		return truncate("task: " + args.Description)
	}
	return "running subtask"
}

// describeGeneric generates a description for unknown tools.
func describeGeneric(toolName, command string) string {
	if command != "" {
		return truncate(toolName + ": " + summarizeCommand(command))
	}
	return "running " + toolName
}

// summarizeCommand extracts the first meaningful token(s) from a command
// string for use in short descriptions.
func summarizeCommand(command string) string {
	cmd := firstToken(command)
	if cmd == "" {
		return command
	}
	// For common commands, show the subcommand too.
	switch cmd {
	case "git":
		tokens := strings.Fields(command)
		if len(tokens) >= 2 {
			return strings.Join(tokens[:min(3, len(tokens))], " ")
		}
	case "docker":
		tokens := strings.Fields(command)
		if len(tokens) >= 2 {
			return strings.Join(tokens[:min(3, len(tokens))], " ")
		}
	case "kubectl":
		tokens := strings.Fields(command)
		if len(tokens) >= 2 {
			return strings.Join(tokens[:min(3, len(tokens))], " ")
		}
	}
	return cmd
}

// firstToken returns the first whitespace-delimited token.
func firstToken(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	idx := strings.IndexAny(s, " \t\n")
	if idx < 0 {
		return s
	}
	return s[:idx]
}

// truncate shortens s to maxLen runes, appending "…" if truncated.
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= statusMaxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:statusMaxLen-1]) + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
