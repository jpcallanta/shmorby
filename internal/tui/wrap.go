package tui

import "strings"

// wrapText word-wraps text to a given width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			result.WriteString("\n")
		}
		if len(line) == 0 {
			continue
		}
		for len(line) > width {
			idx := strings.LastIndex(line[:width+1], " ")
			if idx < 0 {
				result.WriteString(line[:width])
				line = line[width:]
			} else {
				result.WriteString(line[:idx])
				line = line[idx+1:]
			}
			result.WriteString("\n")
		}
		result.WriteString(line)
	}
	return result.String()
}
