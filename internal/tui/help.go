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
	// contentHeight is the number of scrollable body lines (the fixed
	// title and footer rows are excluded). It lives here so scroll
	// clamping always uses the real content size.
	contentHeight int
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

// SetContentHeight records the number of scrollable body lines so the
// scroll clamp can use the real rendered size.
func (h *HelpModel) SetContentHeight(n int) {
	if n < 0 {
		n = 0
	}
	h.contentHeight = n
}

// ScrollUp scrolls the help content up by one line.
func (h *HelpModel) ScrollUp() {
	if h.scroll > 0 {
		h.scroll--
	}
}

// ScrollDown scrolls the help content down by one line.
func (h *HelpModel) ScrollDown(viewHeight int) {
	if h.scroll < h.maxScroll(viewHeight) {
		h.scroll++
	}
}

// PageUp scrolls the help content up by one page.
func (h *HelpModel) PageUp(viewHeight int) {
	h.scroll -= h.pageSize(viewHeight)
	if h.scroll < 0 {
		h.scroll = 0
	}
}

// PageDown scrolls the help content down by one page.
func (h *HelpModel) PageDown(viewHeight int) {
	h.scroll += h.pageSize(viewHeight)
	if max := h.maxScroll(viewHeight); h.scroll > max {
		h.scroll = max
	}
}

// ScrollToTop jumps to the first line of the help content.
func (h *HelpModel) ScrollToTop() {
	h.scroll = 0
}

// ScrollToBottom jumps to the last reachable line of the help content.
func (h *HelpModel) ScrollToBottom(viewHeight int) {
	h.scroll = h.maxScroll(viewHeight)
}

// maxScroll returns the largest scroll offset that still shows the last
// body line. The viewport reserves one row for the title bar and one for
// the footer.
func (h *HelpModel) maxScroll(viewHeight int) int {
	if d := h.contentHeight - h.bodyRows(viewHeight); d > 0 {
		return d
	}
	return 0
}

// bodyRows returns how many body lines fit between the title and footer.
func (h *HelpModel) bodyRows(viewHeight int) int {
	if rows := viewHeight - 2; rows > 0 {
		return rows
	}
	return 1
}

// pageSize returns how many lines one page scroll covers.
func (h *HelpModel) pageSize(viewHeight int) int {
	if size := viewHeight - 2; size > 0 {
		return size
	}
	return 1
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
				"  chat               General conversation & research",
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
				"  /platform <name>   Switch LLM provider",
				"  /apikey <key>      Set API key for current provider",
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

// helpLineCount returns the number of body lines the help overlay renders
// for the given sections (excluding the fixed title and footer rows).
// renderHelpBody renders exactly one line per source line (no wrapping),
// so this is purely structural and cheap to compute for scroll clamping.
func helpLineCount(sections []helpSection) int {
	n := 0
	for _, s := range sections {
		n += 1 + len(s.lines) + 1 // section title + lines + blank separator
	}
	return n
}

// renderHelpBody renders the styled help body lines for the given sections.
// The title bar and footer are laid out separately by renderHelpOverlay,
// so the returned slice is exactly the scrollable content.
func renderHelpBody(sections []helpSection, theme styles.Theme) []string {
	body := make([]string, 0, helpLineCount(sections))

	for _, s := range sections {
		sectionStyle := lipgloss.NewStyle().Foreground(styles.Mauve).Bold(true)
		body = append(body, sectionStyle.Render("  "+s.title))

		if s.title == "CONFIG PARAMETERS" {
			paramKeyStyle := theme.PopupItem.Bold(true)
			paramValStyle := lipgloss.NewStyle().
				Foreground(styles.Teal)
			paramOptStyle := lipgloss.NewStyle().
				Foreground(styles.Overlay2)
			for _, line := range s.lines {
				// Header line: "(key · current value · valid options)"
				if strings.HasPrefix(strings.TrimSpace(line), "(key") {
					body = append(body,
						theme.PopupDesc.Render(line))
					continue
				}
				// Format: "  %-28s %-14s · %s"
				if len(line) >= 48 {
					keyPart := strings.TrimSpace(line[2:30])
					valPart := strings.TrimSpace(line[31:45])
					optPart := strings.TrimSpace(line[48:])
					body = append(body,
						paramKeyStyle.Render("  "+keyPart)+
							paramValStyle.Render(" "+valPart)+
							paramOptStyle.Render(" · "+optPart))
				} else {
					body = append(body,
						theme.PopupDesc.Render(line))
				}
			}
		} else {
			for _, line := range s.lines {
				// Split into key and description for styling.
				if idx := strings.Index(line, "  "); idx >= 0 {
					key := line[:idx+2]
					desc := strings.TrimLeft(line[idx+2:], " ")
					body = append(body,
						theme.PopupItem.Render(key)+
							theme.PopupDesc.Render(desc))
				} else {
					body = append(body,
						theme.PopupItem.Render(line))
				}
			}
		}

		// Blank separator between sections.
		body = append(body, "")
	}

	return body
}

// renderHelpOverlay renders the full-screen help overlay.
func (m Model) renderHelpOverlay() string {
	var params []agent.ParamInfo
	if m.configOverrider != nil {
		params = m.configOverrider.OverrideableParams()
	}
	sections := helpContent(m.mode, params)
	theme := m.theme

	body := renderHelpBody(sections, theme)

	viewHeight := m.height
	if viewHeight <= 0 {
		// Window size unknown (e.g. headless tests): show everything.
		viewHeight = len(body) + 2
	}
	bodyRows := viewHeight - 2
	if bodyRows < 1 {
		bodyRows = 1
	}
	maxScroll := len(body) - bodyRows
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.showHelp.scroll
	if scroll > maxScroll {
		scroll = maxScroll
	}

	var sb strings.Builder

	// Title bar (fixed; never scrolls away).
	sb.WriteString(theme.PopupTitle.Render(" /help") + "\n")

	// Scrollable body clipped to the viewport.
	for i := 0; i < bodyRows; i++ {
		if idx := scroll + i; idx < len(body) {
			sb.WriteString(body[idx] + "\n")
		} else {
			sb.WriteString("\n")
		}
	}

	// Footer (pinned at the bottom) with scroll position when paged.
	footer := " Press any key to close."
	if len(body) > bodyRows {
		footer += fmt.Sprintf("  ▰ %d/%d", scroll+1, len(body))
	}
	sb.WriteString(theme.PopupDesc.Render(footer))

	return sb.String()
}
