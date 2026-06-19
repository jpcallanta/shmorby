// Package render provides markdown rendering for the TUI output pane.
package render

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// RenderMarkdown renders markdown text to ANSI terminal output using glamour.
// Width is the target word-wrap width. Empty string is returned on error.
func RenderMarkdown(md string, width int) string {
	if strings.TrimSpace(md) == "" {
		return md
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
