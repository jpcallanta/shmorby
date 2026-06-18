// Package completion provides a slash-command completion engine for the TUI.
package completion

import (
	"strings"
)

// Command describes a slash command and its description.
type Command struct {
	Name        string
	Description string
}

// Engine provides slash-command completion by prefix matching.
type Engine struct {
	commands []Command
}

// New creates an Engine with the standard set of slash commands.
func New() *Engine {
	return &Engine{
		commands: []Command{
			{Name: "/quit", Description: "Exit shmorby"},
			{Name: "/reset", Description: "Clear session history"},
			{Name: "/model", Description: "Switch LLM model"},
			{Name: "/agent", Description: "Switch agent mode (operate/diagnose)"},
			{Name: "/scope", Description: "Show loaded scope files"},
			{Name: "/memory", Description: "Memory management"},
			{Name: "/context", Description: "Context compression stats"},
			{Name: "/tui", Description: "Toggle fullscreen mode"},
			{Name: "/help", Description: "Show this help"},
		},
	}
}

// Complete returns commands matching the given input prefix.
// Returns nil when input does not start with "/".
func (e *Engine) Complete(input string) []Command {
	if !strings.HasPrefix(input, "/") {
		return nil
	}
	prefix := strings.ToLower(input)
	var matches []Command
	for _, cmd := range e.commands {
		if strings.HasPrefix(cmd.Name, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// All returns all registered commands.
func (e *Engine) All() []Command {
	out := make([]Command, len(e.commands))
	copy(out, e.commands)
	return out
}
