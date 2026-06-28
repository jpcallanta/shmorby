package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"shmorby/internal/agent"
	"shmorby/internal/tui/styles"
)

// HelpModel manages the full-screen help overlay.
type HelpModel struct {
	visible bool
	scroll  int
}

// NewHelpModel creates a new help overlay.
func NewHelpModel() *HelpModel {
	return &HelpModel{}
}

// Visible reports whether the help overlay is showing.
func (h *HelpModel) Visible() bool {
	return h.visible
}

// Toggle shows/hides the help overlay.
func (h *HelpModel) Toggle() {
	h.visible = !h.visible
	if !h.visible {
		h.scroll = 0
	}
}

// Show opens the help overlay.
func (h *HelpModel) Show() {
	h.visible = true
	h.scroll = 0
}

// Hide closes the help overlay.
func (h *HelpModel) Hide() {
	h.visible = false
	h.scroll = 0
}

// ScrollUp scrolls the help content up by one line.
func (h *HelpModel) ScrollUp() {
	if h.scroll > 0 {
		h.scroll--
	}
}

// ScrollDown scrolls the help content down by one line.
func (h *HelpModel) ScrollDown(contentHeight, viewHeight int) {
	if contentHeight > viewHeight && h.scroll < contentHeight-viewHeight {
		h.scroll++
	}
}

// helpSection is a named section in the help overlay.
type helpSection struct {
	title string
	lines []string
}

// helpContent returns the full help content as sections.
func helpContent(mode string, params []agent.ParamInfo) []helpSection {
	sections := []helpSection{
		{
			title: "AGENT MODES",
			lines: []string{
				"  tab / shift+tab    Cycle agent modes",
				"  operate            Full tool access (default)",
				"  diagnose           Read-only inspection",
			},
		},
		{
			title: "SLASH COMMANDS",
			lines: []string{
				"  /help              Show this screen",
				"  /set <param> <value>  Override a config parameter",
				"  /quit              Exit shmorby",
				"  /reset             Clear conversation history",
				"  /model <name>      Switch LLM model",
				"  /agent <mode>      Switch agent mode",
				"  /scope             Show loaded scope context",
				"  /memory            Memory management",
				"  /context           Token usage and compression stats",
				"  /log <level>       Set log verbosity",
				"  /tui               Toggle fullscreen mode",
			},
		},
		// Build CONFIG PARAMETERS section dynamically.
		buildConfigParamsSection(params),
		{
			title: "KEYBOARD SHORTCUTS",
			lines: []string{
				"  ctrl+h             Show this help",
				"  ctrl+p             Command palette",
				"  ctrl+r             Reverse-i-search input history",
				"  ctrl+c             Quit shmorby",
				"  ctrl+v             Paste from clipboard",
				"  ctrl+l             Toggle log section",
				"  ctrl+t             Toggle thinking block",
				"  ctrl+x             Leader key (see below)",
				"  tab / shift+tab    Cycle agent modes (empty input)",
				"  pgup / pgdn        Scroll output by page",
				"  up / down          Scroll output by line",
				"  home / end         Top / bottom of output",
			},
		},
		{
			title: "LEADER KEY (ctrl+x)",
			lines: []string{
				"  ctrl+x c           Compact session",
				"  ctrl+x n           New session",
				"  ctrl+x l           Session list",
				"  ctrl+x m           Model list / switch",
				"  ctrl+x t           Theme list / switch",
				"  ctrl+x a           Agent list / switch",
				"  ctrl+x u           Undo last message",
				"  ctrl+x r           Redo",
				"  ctrl+x e           Open external editor",
				"  ctrl+x x           Export session",
				"  ctrl+x q           Quit",
				"  ctrl+x s           Status view",
				"  ctrl+x h           Tips / help",
				"  ctrl+x b           Toggle sidebar",
				"  ctrl+x y           Copy selected text",
			},
		},
		{
			title: "PERMISSIONS",
			lines: []string{
				"  shell              allow",
				"  ssh                allow",
				"  sudo               ask (default disabled)",
				"  aws                ask (default disabled)",
			},
		},
	}

	return sections
}

// buildConfigParamsSection creates a help section from ParamInfo.
func buildConfigParamsSection(params []agent.ParamInfo) helpSection {
	lines := make([]string, 0, len(params)+1)
	lines = append(lines, "  (key · current value · valid options)")
	for _, p := range params {
		line := fmt.Sprintf("  %-28s %-14s · %s",
			p.Key, p.CurrentValue, p.ValidOptions)
		lines = append(lines, line)
	}
	return helpSection{
		title: "CONFIG PARAMETERS",
		lines: lines,
	}
}

// renderHelpOverlay renders the full-screen help overlay.
func (m Model) renderHelpOverlay() string {
	var params []agent.ParamInfo
	if m.configOverrider != nil {
		params = m.configOverrider.OverrideableParams()
	}
	sections := helpContent(m.mode, params)
	theme := m.theme

	var sb strings.Builder

	// Title bar.
	title := " /help"
	sb.WriteString(theme.PopupTitle.Render(title) + "\n")

	// Render each section.
	for _, s := range sections {
		sectionStyle := lipgloss.NewStyle().Foreground(styles.Mauve).Bold(true)
		sb.WriteString(sectionStyle.Render("  "+s.title) + "\n")

		if s.title == "CONFIG PARAMETERS" {
			paramKeyStyle := theme.PopupItem.Bold(true)
			paramValStyle := lipgloss.NewStyle().
				Foreground(styles.Teal)
			paramOptStyle := lipgloss.NewStyle().
				Foreground(styles.Overlay2)
			for _, line := range s.lines {
				// Header line: "(key · current value · valid options)"
				if strings.HasPrefix(strings.TrimSpace(line), "(key") {
					sb.WriteString(
						theme.PopupDesc.Render(line) + "\n",
					)
					continue
				}
				// Format: "  %-28s %-14s · %s"
				if len(line) >= 48 {
					keyPart := strings.TrimSpace(line[2:30])
					valPart := strings.TrimSpace(line[31:45])
					optPart := strings.TrimSpace(line[48:])
					sb.WriteString(
						paramKeyStyle.Render("  "+keyPart) +
							paramValStyle.Render(" "+valPart) +
							paramOptStyle.Render(" · "+optPart) +
							"\n",
					)
				} else {
					sb.WriteString(
						theme.PopupDesc.Render(line) + "\n",
					)
				}
			}
		} else {
			for _, line := range s.lines {
				// Split into key and description for styling.
				if idx := strings.Index(line, "  "); idx >= 0 {
					key := line[:idx+2]
					desc := strings.TrimLeft(line[idx+2:], " ")
					sb.WriteString(theme.PopupItem.Render(key) +
						theme.PopupDesc.Render(desc) + "\n")
				} else {
					sb.WriteString(
						theme.PopupItem.Render(line) + "\n",
					)
				}
			}
		}
		sb.WriteString("\n")
	}

	// Footer.
	footer := " Press any key to close."
	sb.WriteString(theme.PopupDesc.Render(footer))

	result := sb.String()
	if m.height > 0 {
		lines := strings.Count(result, "\n") + 1
		if pad := m.height - lines; pad > 0 {
			result += strings.Repeat("\n", pad)
		}
	}
	return result
}
