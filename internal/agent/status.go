package agent

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"shmorby/internal/tools"
)

const (
	// statusMaxLen truncates descriptions to this length.
	statusMaxLen = 60
)

// StatusGenerator produces short, present-tense descriptions of the
// current tool action. It generates descriptions directly from tool
// metadata and arguments without any LLM call.
type StatusGenerator struct{}

// NewStatusGenerator creates a generator. Descriptions are generated
// from tool metadata — no provider or model name is needed.
func NewStatusGenerator() *StatusGenerator {
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
	case tools.ToolShell, tools.ToolSudo:
		return describeShellCommand(command)
	case tools.ToolSSH:
		return describeSSHCommand(command)
	case tools.ToolAWS:
		return describeAWSCommand(command)
	case tools.ToolFind:
		return describeFindCommand(command)
	case tools.ToolWebSearch:
		return describeWebSearch(command)
	case tools.ToolWebFetch:
		return describeWebFetch(command)
	case tools.ToolTask:
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
	// For common commands, show the subcommand (and next token)
	// too — e.g. "git commit -m" or "docker ps -a".
	switch cmd {
	case "git", "docker", "kubectl":
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
