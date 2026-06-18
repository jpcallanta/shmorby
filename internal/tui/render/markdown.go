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

// RenderMarkdownWithStyle renders markdown with a specific glamour style.
// style can be "auto", "dark", "light", or a path to a style JSON file.
func RenderMarkdownWithStyle(md string, width int, style string) string {
	if strings.TrimSpace(md) == "" {
		return md
	}
	var opts []glamour.TermRendererOption
	switch style {
	case "dark":
		opts = append(opts, glamour.WithStandardStyle("dark"))
	case "light":
		opts = append(opts, glamour.WithStandardStyle("light"))
	case "auto":
		opts = append(opts, glamour.WithStandardStyle("dark"))
	default:
		opts = append(opts, glamour.WithStandardStyle("dark"))
	}
	opts = append(opts, glamour.WithWordWrap(width))

	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}
