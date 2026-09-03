package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"shmorby/internal/agent"
	"shmorby/internal/config"
	"shmorby/internal/tui/styles"
)

// Tests that /help opens the help overlay.
func TestModelCommand_Help(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.showHelp == nil || !m.showHelp.Visible() {
		t.Error("help overlay should be visible after /help")
	}
}

// Tests that help overlay renders in View when visible.
// Uses a tall terminal so every section is above the fold; paging tests
// below cover reachability in a small terminal.
func TestModelView_HelpOverlay(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 200
	m.showHelp.Show()

	view := m.View()

	if !strings.Contains(view, "/help") {
		t.Error("help overlay should contain /help title")
	}
	if !strings.Contains(view, "AGENT MODES") {
		t.Error("help overlay should contain AGENT MODES section")
	}
	if !strings.Contains(view, "SLASH COMMANDS") {
		t.Error("help overlay should contain SLASH COMMANDS section")
	}
	if !strings.Contains(view, "KEYBOARD SHORTCUTS") {
		t.Error("help overlay should contain KEYBOARD SHORTCUTS section")
	}
	if !strings.Contains(view, "LEADER KEY") {
		t.Error("help overlay should contain LEADER KEY section")
	}
	if !strings.Contains(view, "PERMISSIONS") {
		t.Error("help overlay should contain PERMISSIONS section")
	}
	if !strings.Contains(view, "Press any key to close") {
		t.Error("help overlay should contain close hint")
	}
}

// Tests that any key closes the help overlay.
func TestModelUpdate_HelpClosesOnAnyKey(t *testing.T) {
	m := NewModel(Config{})
	m.showHelp.Show()

	if !m.showHelp.Visible() {
		t.Fatal("help should be visible")
	}

	updated, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'a'},
	})
	m = updated.(Model)

	if m.showHelp.Visible() {
		t.Error("help overlay should be closed after any key")
	}
}

// Tests that help overlay is not visible by default.
func TestHelpModel_DefaultHidden(t *testing.T) {
	m := NewModel(Config{})
	if m.showHelp == nil {
		t.Fatal("showHelp should be initialized")
	}
	if m.showHelp.Visible() {
		t.Error("help overlay should not be visible by default")
	}
}

// Tests that help overlay Toggle works.
func TestHelpModel_Toggle(t *testing.T) {
	h := NewHelpModel()
	if h.Visible() {
		t.Error("should start hidden")
	}
	h.Toggle()
	if !h.Visible() {
		t.Error("should be visible after toggle")
	}
	h.Toggle()
	if h.Visible() {
		t.Error("should be hidden after second toggle")
	}
}

// Tests that help overlay Show/Hide work.
func TestHelpModel_ShowHide(t *testing.T) {
	h := NewHelpModel()
	h.Show()
	if !h.Visible() {
		t.Error("should be visible after Show")
	}
	h.Hide()
	if h.Visible() {
		t.Error("should be hidden after Hide")
	}
}

// Tests that help content contains all required sections.
func TestHelpContent_AllSections(t *testing.T) {
	sections := helpContent("operate", nil)

	titles := []string{
		"AGENT MODES",
		"SLASH COMMANDS",
		"CONFIG PARAMETERS",
		"KEYBOARD SHORTCUTS",
		"LEADER KEY (ctrl+x)",
		"PERMISSIONS",
	}
	for _, expected := range titles {
		found := false
		for _, s := range sections {
			if s.title == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("help content missing section %q", expected)
		}
	}
}

// Tests that help content is non-empty.
func TestHelpContent_NonEmpty(t *testing.T) {
	sections := helpContent("operate", nil)
	if len(sections) == 0 {
		t.Error("helpContent should return non-empty sections")
	}
}

// Tests that help content shows the current mode.
func TestHelpContent_ShowsMode(t *testing.T) {
	sections := helpContent("diagnose", nil)
	found := false
	for _, s := range sections {
		for _, line := range s.lines {
			if strings.Contains(line, "diagnose") {
				found = true
			}
		}
	}
	if !found {
		t.Error("help content should mention current mode")
	}
}

// Tests that /help does not interfere with normal operation.
func TestModelCommand_HelpDoesNotQuit(t *testing.T) {
	m := NewModel(Config{})

	_, done, err := m.handleCommand("/help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("/help should not trigger quit")
	}
}

// Tests that View renders normally when help is not visible.
func TestModelView_NormalWhenHelpHidden(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24

	view := m.View()

	// Should contain normal UI elements, not help overlay.
	if strings.Contains(view, "Press any key to close") {
		t.Error("view should not contain help hint when help is hidden")
	}
	if !strings.Contains(view, "agent:") {
		t.Error("view should contain status bar")
	}
}

// Tests help overlay renders all slash commands.
func TestHelpOverlay_AllCommands(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	view := m.View()

	commands := []string{
		"/help", "/quit", "/reset", "/model", "/agent",
		"/scope", "/memory", "/context", "/log", "/tui",
	}
	for _, cmd := range commands {
		if !strings.Contains(view, cmd) {
			t.Errorf("help overlay missing command %q", cmd)
		}
	}
}

// Tests help overlay renders all leader key bindings.
// Uses a tall terminal: LEADER KEY sits below the fold at height=24.
func TestHelpOverlay_LeaderBindings(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 200
	m.showHelp.Show()

	view := m.View()

	bindings := []string{
		"ctrl+x c", "ctrl+x n", "ctrl+x l", "ctrl+x m",
		"ctrl+x q", "ctrl+x h",
	}
	for _, b := range bindings {
		if !strings.Contains(view, b) {
			t.Errorf("help overlay missing leader binding %q", b)
		}
	}
}

// Tests that Escape closes help overlay.
func TestModelUpdate_EscClosesHelp(t *testing.T) {
	m := NewModel(Config{})
	m.showHelp.Show()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.showHelp.Visible() {
		t.Error("help overlay should be closed after Escape")
	}
}

// Tests that ctrl+h opens help overlay.
func TestModelUpdate_CtrlHOpensHelp(t *testing.T) {
	m := NewModel(Config{})
	if m.showHelp.Visible() {
		t.Error("help should start hidden")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = updated.(Model)

	if !m.showHelp.Visible() {
		t.Error("help overlay should be visible after ctrl+h")
	}
}

// Tests that ctrl+h toggles help (open then close).
func TestModelUpdate_CtrlHTogglesHelp(t *testing.T) {
	m := NewModel(Config{})

	// Open.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = updated.(Model)
	if !m.showHelp.Visible() {
		t.Fatal("help should be visible")
	}

	// Close.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = updated.(Model)
	if m.showHelp.Visible() {
		t.Error("help should be closed after second ctrl+h")
	}
}

// Tests that pgup/pgdn scroll help overlay.
func TestModelUpdate_HelpScroll(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	// ScrollDown with pgdn.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	if m.showHelp.scroll == 0 {
		t.Error("help should have scrolled down after pgdn")
	}

	// ScrollUp with pgup.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.showHelp.scroll != 0 {
		t.Error("help should have scrolled back to top after pgup")
	}
}

// Tests that arrow keys scroll help overlay.
func TestModelUpdate_HelpScrollArrows(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	// Down arrow.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.showHelp.scroll == 0 {
		t.Error("help should have scrolled down after down arrow")
	}

	// Up arrow.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.showHelp.scroll != 0 {
		t.Error("help should have scrolled back to top after up arrow")
	}
}

// Tests that Enter closes help overlay.
func TestModelUpdate_EnterClosesHelp(t *testing.T) {
	m := NewModel(Config{})
	m.showHelp.Show()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.showHelp.Visible() {
		t.Error("help overlay should be closed after Enter")
	}
}

// Tests that help overlay remains visible during scroll (not closed).
func TestModelUpdate_HelpScrollDoesNotClose(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)

	if !m.showHelp.Visible() {
		t.Error("help overlay should remain visible after scroll")
	}
}

// Tests ctrl+c description in help overlay says "Quit shmorby".
// Uses a tall terminal: the ctrl+c line sits below the fold at height=24.
func TestHelpOverlay_CtrlCDescription(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 200
	m.showHelp.Show()

	view := m.View()
	if !strings.Contains(view, "Quit shmorby") {
		t.Error("help overlay should describe ctrl+c as 'Quit shmorby'")
	}
}

// Tests that /tui is in help overlay slash commands.
func TestHelpOverlay_TUICommand(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	view := m.View()
	if !strings.Contains(view, "/tui") {
		t.Error("help overlay should list /tui command")
	}
}

// Tests that PgDn changes the rendered help frame and PgUp restores it.
func TestHelpOverlay_PageNavigationChangesFrame(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	top := m.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	paged := m.View()
	if paged == top {
		t.Error("PgDn should change the rendered help frame")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if back := m.View(); back != top {
		t.Error("PgUp should restore the original help frame")
	}
}

// Tests that the bottom of the help content is reachable in a small
// terminal and the footer stays pinned.
func TestHelpOverlay_BottomReachableInSmallTerminal(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "PERMISSIONS") {
		t.Error("PERMISSIONS section should be reachable at height=24")
	}
	if !strings.Contains(view, "Press any key to close") {
		t.Error("footer should stay visible when scrolled to bottom")
	}
}

// Tests that scrolling is clamped at the end of the help content.
func TestHelpOverlay_ScrollClampedAtEnd(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	bottom := m.View()

	for i := 0; i < 5; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = updated.(Model)
	}
	if m.showHelp.scroll != m.showHelp.maxScroll(m.height) {
		t.Errorf("scroll should be clamped at maxScroll=%d, got %d",
			m.showHelp.maxScroll(m.height), m.showHelp.scroll)
	}
	if after := m.View(); after != bottom {
		t.Error("view should not change when scrolled past the end")
	}
}

// Tests that the footer is visible at the top of the help overlay too.
func TestHelpOverlay_FooterVisibleAtTop(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	if !strings.Contains(m.View(), "Press any key to close") {
		t.Error("footer should be visible at the top of the help overlay")
	}
}

// Tests that Home/End jump to the top/bottom of the help content.
func TestHelpOverlay_HomeEndJump(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	top := m.View()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if m.showHelp.scroll == 0 {
		t.Fatal("End should scroll past the top")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if m.showHelp.scroll != 0 {
		t.Errorf("Home should jump to top, scroll=%d", m.showHelp.scroll)
	}
	if home := m.View(); home != top {
		t.Error("Home should restore the top help frame")
	}
}

// Tests that the footer shows a scroll position indicator when the
// content overflows the viewport.
func TestHelpOverlay_PositionIndicator(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.showHelp.Show()

	wantTotal := helpLineCount(helpContent(m.mode, nil))
	top := m.View()
	if !strings.Contains(top, fmt.Sprintf("▰ 1/%d", wantTotal)) {
		t.Errorf("expected position indicator '▰ 1/%d' at top, got: %q",
			wantTotal, top)
	}
}

// Tests that every runtime-changeable config parameter is surfaced in the
// help overlay's CONFIG PARAMETERS section when a live overrider is
// present. Regression guard: a param accepted by /set without being added
// to the help output fails this test.
func TestHelpOverlay_ConfigParamsRendered(t *testing.T) {
	co := agent.NewConfigOverrider(&config.Config{
		Provider: "ollama",
		Model:    "test-model",
	}, nil, nil, nil)
	params := co.OverrideableParams()
	if len(params) == 0 {
		t.Fatal("expected live overrideable params")
	}

	theme := styles.GetTheme("catppuccin-mocha")
	body := renderHelpBody(helpContent("operate", params), theme)

	start := -1
	for i, line := range body {
		if strings.Contains(line, "CONFIG PARAMETERS") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("help content missing CONFIG PARAMETERS section")
	}

	var sectionText strings.Builder
	for _, line := range body[start+1:] {
		if line == "" { // blank separator ends the section
			break
		}
		sectionText.WriteString(line)
	}
	if !strings.Contains(sectionText.String(), "(key") {
		t.Error("CONFIG PARAMETERS section missing header line")
	}
	for _, p := range params {
		if !strings.Contains(sectionText.String(), p.Key) {
			t.Errorf(
				"help overlay CONFIG PARAMETERS section missing %q",
				p.Key,
			)
		}
	}
}
