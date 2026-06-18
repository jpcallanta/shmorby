// Package tui implements the Bubbletea-based terminal UI.
package tui

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"shmorby/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// partialANSI matches ANSI escape sequences with an optional final
// letter terminator. Used with ReplaceAllStringFunc to strip only
// incomplete sequences (no terminator letter).
var partialANSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]?`)

// fullANSI matches all ANSI escape sequences.
var fullANSI = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes all ANSI escape sequences from s.
func StripANSI(s string) string {
	return fullANSI.ReplaceAllString(s, "")
}

// StripPartialANSI removes incomplete ANSI escape sequences from s
// while keeping complete sequences intact.
func StripPartialANSI(s string) string {
	return partialANSI.ReplaceAllStringFunc(s, func(match string) string {
		last := match[len(match)-1]
		if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
			return match
		}
		return ""
	})
}

// renderStatus returns the status line content.
func (m Model) renderStatus() string {
	providerName := "none"
	if m.provider != nil {
		providerName = m.provider.Name()
	}
	modeStyle := m.theme.ModeOperate
	if m.mode == "diagnose" {
		modeStyle = m.theme.ModeDiag
	}
	parts := []string{
		m.theme.StatusKey.Render("agent:") +
			" " + modeStyle.Render(m.mode),
		m.theme.StatusKey.Render("provider:") +
			" " + m.theme.StatusValue.Render(providerName),
		m.theme.StatusKey.Render("model:") +
			" " + m.theme.StatusValue.Render(m.model),
	}
	if m.selectionMode {
		parts = append(parts,
			m.theme.StatusKey.Render("mode:")+
				" "+m.theme.StatusValue.Render("[SELECT]"),
		)
	}
	if m.copyNotify != "" && time.Since(m.copyNotifyTime) < 2*time.Second {
		parts = append(parts,
			m.theme.StatusValue.Render(m.copyNotify),
		)
	}
	parts = append(parts,
		m.theme.StatusKey.Render("log:")+
			" "+m.theme.StatusValue.Render(m.logLevel.String()),
	)
	if m.ctxStats != nil && !m.running {
		ctx := fmt.Sprintf("ctx:%s/%s",
			formatTokens(m.ctxStats.EstimatedTokens),
			formatTokens(m.ctxStats.ContextWindow))
		if m.ctxStats.Fallback {
			ctx += "?"
		}
		if m.ctxStats.Compressions > 0 {
			ctx += fmt.Sprintf(" (compressed %dx)", m.ctxStats.Compressions)
		}
		parts = append(parts,
			m.theme.StatusKey.Render("")+
				" "+m.theme.StatusValue.Render(ctx))
	}
	return strings.Join(parts, " │ ")
}

// renderLogSection renders expanded log entries.
func (m Model) renderLogSection() string {
	var b strings.Builder
	width := m.width
	if width <= 0 {
		width = 60
	}
	b.WriteString(logSep(width, fmt.Sprintf(
		"log (%d)", len(m.logEntries),
	)))

	start := 0
	if len(m.logEntries) > m.logDisplayLimit {
		start = len(m.logEntries) - m.logDisplayLimit
	}
	for _, entry := range m.logEntries[start:] {
		b.WriteString(m.renderLogEntry(entry))
	}

	b.WriteString(logEndSep(width, "log"))
	return b.String()
}

// renderLogEntry formats a single log entry with level icon and color.
func (m Model) renderLogEntry(entry LogEntry) string {
	icon, style := logLevelStyle(entry.Level)
	ts := entry.Time.Format("15:04:05")
	text := fmt.Sprintf("  %s %s — %s", icon, ts, entry.Message)
	return style.Render(text) + "\n"
}

// logLevelStyle returns the icon and lipgloss style for a log level.
func logLevelStyle(l slog.Level) (string, lipgloss.Style) {
	switch {
	case l <= slog.LevelDebug:
		return "[·]", lipgloss.NewStyle().Foreground(styles.Overlay2)
	case l >= slog.LevelError:
		return "[✗]", lipgloss.NewStyle().Foreground(styles.Red)
	case l >= slog.LevelWarn:
		return "[!]", lipgloss.NewStyle().Foreground(styles.Yellow)
	default:
		return "[i]", lipgloss.NewStyle().Foreground(styles.Sapphire)
	}
}

// renderThinkingBlock renders the expanded thinking section.
func (m Model) renderThinkingBlock() string {
	var b strings.Builder
	width := m.width
	if width <= 0 {
		width = 60
	}
	elapsed := m.thinking.Elapsed().Round(time.Second)
	tokens := m.thinking.Tokens()
	b.WriteString(logSep(width, fmt.Sprintf(
		"💭 thinking (%s · %d tokens)", elapsed, tokens,
	)))
	for _, line := range m.thinking.Lines() {
		b.WriteString(m.theme.AgentReply.Render("  "+line) + "\n")
	}
	b.WriteString(logEndSep(width, "thinking"))
	return b.String()
}

// renderThinkingPreview renders a single-line thinking preview.
func (m Model) renderThinkingPreview() string {
	width := m.width
	if width <= 0 {
		width = 60
	}
	elapsed := m.thinking.Elapsed().Round(time.Second)
	tokens := m.thinking.Tokens()
	lines := m.thinking.Lines()
	preview := ""
	if len(lines) > 0 {
		preview = lines[0]
		if len(preview) > 40 {
			preview = preview[:40] + "…"
		}
	}
	return logSep(width, fmt.Sprintf(
		"💭 thinking (%s · %d tokens) %s",
		elapsed, tokens, preview,
	))
}

// logSep returns a labeled separator for log/thinking sections.
func logSep(width int, label string) string {
	prefix := strings.Repeat("─", 3)
	suffixLen := width - len(prefix) - len(label) - 6
	if suffixLen < 0 {
		suffixLen = 0
	}
	suffix := strings.Repeat("─", suffixLen)
	return lipgloss.NewStyle().Foreground(styles.Sapphire).Render(
		prefix+" "+label+" "+suffix,
	) + "\n"
}

// logEndSep returns a closing separator for log/thinking sections.
func logEndSep(width int, label string) string {
	text := "end " + label
	prefix := strings.Repeat("─", 3)
	suffixLen := width - len(prefix) - len(text) - 6
	if suffixLen < 0 {
		suffixLen = 0
	}
	suffix := strings.Repeat("─", suffixLen)
	return lipgloss.NewStyle().Foreground(styles.Sapphire).Render(
		prefix+" "+text+" "+suffix,
	) + "\n"
}

// formatTokens formats a token count for display, e.g. 42000 → "42k".
func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}
