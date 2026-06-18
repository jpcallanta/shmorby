// Package styles defines lipgloss themes for the TUI.
package styles

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderBorderedBox returns a bordered box with optional title.
func RenderBorderedBox(
	title, content string, width int,
	titleStyle, borderStyle lipgloss.Style,
) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderStyle.GetForeground()).
		Width(width-4).
		Padding(0, 1)
	if title != "" {
		inner := titleStyle.Render(" "+title+" ") + "\n" + content
		return box.Render(inner)
	}
	return box.Render(content)
}

// RenderTabBar renders a horizontal tab strip.
func RenderTabBar(tabs []TabStyle, active int, width int) string {
	if len(tabs) == 0 {
		return ""
	}
	var parts []string
	for i, t := range tabs {
		if i == active {
			parts = append(parts, t.ActiveStyle.Render(" "+truncateLabel(t.Label, 20)+" "))
		} else {
			style := t.InactiveStyle
			if t.Spinning {
				style = t.SpinStyle
			}
			parts = append(parts, style.Render(" "+truncateLabel(t.Label, 20)+" "))
		}
	}
	return strings.Join(parts, "")
}

// TabStyle holds styles for a tab.
type TabStyle struct {
	Label         string
	ActiveStyle   lipgloss.Style
	InactiveStyle lipgloss.Style
	SpinStyle     lipgloss.Style
	Spinning      bool
}

func truncateLabel(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
