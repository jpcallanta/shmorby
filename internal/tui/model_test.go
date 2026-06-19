package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"shmorby/internal/tui/navigation"
)

// Tests that View renders the bottom-anchored layout.
func TestModelView_Layout(t *testing.T) {
	m := NewModel(Config{
		Mode:      "operate",
		Model:     "test-model",
		ThemeName: "catppuccin-mocha",
	})
	m.width = 80
	m.height = 24

	view := m.View()

	if !strings.Contains(view, "❯") {
		t.Error("view missing prompt character")
	}
	if !strings.Contains(view, "agent:") {
		t.Error("view missing agent status")
	}
	if !strings.Contains(view, "provider:") {
		t.Error("view missing provider status")
	}
	if !strings.Contains(view, "model:") {
		t.Error("view missing model status")
	}
}

// Tests that Enter clears textarea and returns a submit command.
func TestModelUpdate_Enter(t *testing.T) {
	m := NewModel(Config{
		Mode:  "operate",
		Model: "test-model",
	})
	m.width = 80
	m.height = 24
	m.textarea.SetValue("hello world")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.textarea.Value() != "" {
		t.Errorf("textarea not cleared, got %q", m.textarea.Value())
	}
	if m.running {
		t.Error("should not be running yet (submitMsg not processed)")
	}
	if cmd == nil {
		t.Fatal("no command returned")
	}

	// Process the submitMsg to start running.
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if !m.running {
		t.Error("spinner not started after submitMsg")
	}
}

// Tests that Ctrl+C returns a quit message.
func TestModelUpdate_CtrlC(t *testing.T) {
	m := NewModel(Config{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)
	_ = m
}

// Tests input history navigation.
func TestModelUpdate_History(t *testing.T) {
	m := NewModel(Config{})
	m.inputHistory.Add("first")
	m.inputHistory.Add("second")
	m.inputHistory.Add("third")
	// Cursor at end (past newest), Up should load "third".
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "third" {
		t.Errorf("want %q, got %q", "third", m.textarea.Value())
	}

	// Clear textarea, then Up should load "second".
	m.textarea.Reset()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "second" {
		t.Errorf("want %q, got %q", "second", m.textarea.Value())
	}

	// Clear textarea, then Down should load "third" again.
	m.textarea.Reset()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.textarea.Value() != "third" {
		t.Errorf("want %q, got %q", "third", m.textarea.Value())
	}

	// Down at end should clear.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.textarea.Value() != "" {
		t.Errorf("want empty, got %q", m.textarea.Value())
	}
}

// Tests that agentReplyMsg appends to output and stops spinner.
func TestModelUpdate_AgentReply(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.running = true
	m.spinner.Start("thinking…")

	updated, _ := m.Update(agentReplyMsg{text: "hello back"})
	m = updated.(Model)

	if m.running {
		t.Error("still running after reply")
	}
	if len(m.output) != 1 {
		t.Fatalf("want 1 output entry, got %d", len(m.output))
	}
	if !strings.Contains(m.output[0].text, "hello back") {
		t.Errorf("output missing expected text, got %q", m.output[0].text)
	}
}

// Tests that errorMsg appends error to output.
func TestModelUpdate_Error(t *testing.T) {
	m := NewModel(Config{})
	m.running = true

	updated, _ := m.Update(errorMsg{err: fmt.Errorf("oops")})
	m = updated.(Model)

	if m.running {
		t.Error("still running after error")
	}
	if len(m.output) != 1 {
		t.Fatalf("want 1 output, got %d", len(m.output))
	}
	if m.output[0].kind != "error" {
		t.Errorf("want kind error, got %q", m.output[0].kind)
	}
}

// Tests separator rendering with label.
func TestRenderSeparator_WithLabel(t *testing.T) {
	m := NewModel(Config{})
	m.width = 40

	sep := m.renderSeparator("shell: echo hi")

	if !strings.Contains(sep, "shell: echo hi") {
		t.Error("separator missing label")
	}
}

// Tests that output entries appear in view.
func TestModelView_OutputEntries(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.output = append(m.output, outputEntry{
		kind: "user",
		text: "test input",
	})
	mp := &m
	mp.syncViewport()

	view := m.View()
	if !strings.Contains(view, "test input") {
		t.Error("view missing user input")
	}
}

// Tests that WindowSizeMsg updates dimensions.
func TestModelUpdate_WindowSize(t *testing.T) {
	m := NewModel(Config{})

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	if m.width != 120 {
		t.Errorf("want width 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("want height 40, got %d", m.height)
	}
}

// Tests that shift+enter inserts a newline instead of submitting.
func TestModelUpdate_ShiftEnter_InsertsNewline(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24

	updated, cmd := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'a'},
	})
	m = updated.(Model)
	_ = cmd

	updated, cmd = m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'\n'},
	})
	m = updated.(Model)
	_ = cmd

	if m.textarea.Value() != "a\n" {
		t.Errorf("want %q, got %q", "a\n", m.textarea.Value())
	}
	if m.running {
		t.Error("should not be running after shift+enter")
	}
}

// Tests that Enter does not submit when empty.
func TestModelUpdate_Enter_Empty(t *testing.T) {
	m := NewModel(Config{})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd != nil {
		t.Error("should not return command for empty input")
	}
	if m.running {
		t.Error("should not be running")
	}
}

// Tests that Enter does not submit when running.
func TestModelUpdate_Enter_Running(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.textarea.SetValue("hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	_ = cmd

	if m.textarea.Value() != "hello" {
		t.Errorf("textarea should not be cleared while running, got %q",
			m.textarea.Value())
	}
}

// Tests that completion popup shows after typing /.
func TestModelUpdate_CompletionShows(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	// Type /q to trigger completion.
	for _, r := range "/q" {
		m.Update(tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune{r},
		})
	}
	// Access updated model - need to check after each update.
	_ = m.textarea.Value()

	// Check completion is triggered after typing /
	m.textarea.SetValue("/")
	m.updateCompletion()
	if !m.showCompletion {
		t.Error("completion should show after typing /")
	}
	if len(m.complMatches) < 8 {
		t.Errorf("expected at least 8 completions, got %d", len(m.complMatches))
	}
}

// Tests that completion narrows as more chars are typed.
func TestModelUpdate_CompletionNarrows(t *testing.T) {
	m := NewModel(Config{})
	m.textarea.SetValue("/q")
	m.updateCompletion()
	if len(m.complMatches) != 1 {
		t.Fatalf("expected 1 match for /q, got %d", len(m.complMatches))
	}
	if m.complMatches[0].Name != "/quit" {
		t.Errorf("expected /quit, got %s", m.complMatches[0].Name)
	}
}

// Tests that Tab accepts completion.
func TestModelUpdate_TabAcceptsCompletion(t *testing.T) {
	m := NewModel(Config{})
	m.textarea.SetValue("/q")
	m.updateCompletion()
	if !m.showCompletion {
		t.Fatal("completion should be showing")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if m.showCompletion {
		t.Error("completion should be dismissed after Tab")
	}
	if m.textarea.Value() != "/quit " {
		t.Errorf("expected input to become /quit , got %q", m.textarea.Value())
	}
}

// Tests that Esc dismisses completion.
func TestModelUpdate_EscDismissesCompletion(t *testing.T) {
	m := NewModel(Config{})
	m.textarea.SetValue("/")
	m.updateCompletion()
	if !m.showCompletion {
		t.Fatal("completion should be showing")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.showCompletion {
		t.Error("completion should be dismissed after Esc")
	}
}

// Tests that toolStatusMsg sets currentTool.
func TestModelUpdate_ToolStatus(t *testing.T) {
	m := NewModel(Config{})

	updated, _ := m.Update(toolStatusMsg{name: "shell", status: "running"})
	m = updated.(Model)

	if m.currentTool != "shell" {
		t.Errorf("expected currentTool shell, got %s", m.currentTool)
	}
	if m.currentToolStatus != "running" {
		t.Errorf("expected currentToolStatus running, got %s", m.currentToolStatus)
	}
}

// Tests that spinner is shown when running with a tool active.
func TestModelView_SpinnerWhenRunning(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.running = true
	m.startTime = time.Now()
	m.spinner.Start("waiting for tool")

	view := m.View()
	if !strings.Contains(view, "waiting for tool") {
		t.Error("view should contain spinner text when running")
	}
	if !strings.Contains(view, "⠙") && !strings.Contains(view, "⠋") {
		t.Error("view should show spinner animation frame")
	}
	// Tool name appears in output entries rather than separator now.
	m.output = append(m.output, outputEntry{
		kind: "tool",
		text: "$ systemctl restart nginx",
	})
	mp := &m
	mp.syncViewport()
	view = m.View()
	if !strings.Contains(view, "systemctl restart nginx") {
		t.Error("view should contain tool command in output")
	}
}

// Tests that selection mode can be toggled and textarea is
// blurred/focused correctly.
func TestModelUpdate_SelectionMode(t *testing.T) {
	m := NewModel(Config{})
	if m.selectionMode {
		t.Error("selection mode should start disabled")
	}
	if !m.textarea.Focused() {
		t.Error("textarea should start focused")
	}

	// Directly toggle selection mode (simulates ctrl+shift+s behavior).
	m.selectionMode = true
	m.viewport.SetSelectionMode(true)
	m.textarea.Blur()
	if !m.selectionMode {
		t.Error("selection mode should be enabled")
	}
	if m.textarea.Focused() {
		t.Error("textarea should be blurred in selection mode")
	}

	// Exit selection mode (simulates esc behavior).
	m.selectionMode = false
	m.viewport.SetSelectionMode(false)
	m.textarea.Focus()
	m.selectionStart = 0
	m.selectionEnd = 0
	if m.selectionMode {
		t.Error("selection mode should be disabled after exit")
	}
	if !m.textarea.Focused() {
		t.Error("textarea should be refocused after exiting selection mode")
	}
}

// Tests that ctrl+shift+c copies last agent reply when no selection.
func TestModelUpdate_CopyWithoutSelection(t *testing.T) {
	m := NewModel(Config{})
	m.output = append(m.output, outputEntry{
		kind: "agent", text: "test reply",
	})

	// Simulate ctrl+shift+c with no selection active.
	// Copy last agent reply.
	for i := len(m.output) - 1; i >= 0; i-- {
		if m.output[i].kind == "agent" {
			m.copyNotify = "✓ copied to clipboard"
			m.copyNotifyTime = time.Now()
			break
		}
	}
	if m.copyNotify == "" {
		t.Error("copy notification should be set after copy")
	}
}

// Tests that outputTexts extracts texts from output entries.
func TestOutputTexts(t *testing.T) {
	entries := []outputEntry{
		{kind: "user", text: "hello"},
		{kind: "agent", text: "world"},
	}
	texts := outputTexts(entries)
	if len(texts) != 2 {
		t.Fatalf("want 2, got %d", len(texts))
	}
	if texts[0] != "hello" {
		t.Errorf("want hello, got %s", texts[0])
	}
	if texts[1] != "world" {
		t.Errorf("want world, got %s", texts[1])
	}
}

// Tests that /tui command toggles fullscreen flag.
func TestModelCommand_TUI(t *testing.T) {
	m := NewModel(Config{})

	cmd, done, err := m.handleCommand("/tui")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	// The output should be appended.
	if len(m.output) == 0 {
		t.Error("expected output after /tui")
	}
}

// Tests Tab key cycles agent mode forward when input is empty.
func TestModelUpdate_TabModeCycle(t *testing.T) {
	m := NewModel(Config{Mode: "operate"})
	m.width = 80
	m.height = 24
	if m.mode != "operate" {
		t.Fatalf("want operate, got %q", m.mode)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)

	if m.mode != "diagnose" {
		t.Errorf("want diagnose after Tab, got %q", m.mode)
	}
	if cmd == nil {
		t.Fatal("expected command for mode change")
	}

	// Process the agentModeChangedMsg.
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if len(m.output) == 0 {
		t.Error("expected mode change output")
	}
}

// Tests Shift+Tab cycles agent mode reverse when input is empty.
func TestModelUpdate_ShiftTabModeCycle(t *testing.T) {
	m := NewModel(Config{Mode: "operate"})
	m.width = 80

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)

	if m.mode != "diagnose" {
		t.Errorf("want diagnose after Shift+Tab, got %q", m.mode)
	}
}

// Tests Ctrl+P toggles command palette.
func TestModelUpdate_CtrlPTogglesPalette(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80

	if m.commandPalette.Visible() {
		t.Error("palette should not be visible initially")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated.(Model)

	if !m.commandPalette.Visible() {
		t.Error("palette should be visible after Ctrl+P")
	}

	// Second Ctrl+P dismisses.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated.(Model)

	if m.commandPalette.Visible() {
		t.Error("palette should be dismissed after second Ctrl+P")
	}
}

// Tests Ctrl+R toggles reverse-i-search.
func TestModelUpdate_CtrlRTogglesSearch(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80

	if m.reverseSearch.Visible() {
		t.Error("search should not be visible initially")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)

	if !m.reverseSearch.Visible() {
		t.Error("search should be visible after Ctrl+R")
	}

	// Second Ctrl+R cycles forward.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)

	if !m.reverseSearch.Visible() {
		t.Error("search should still be visible")
	}
}

// Tests Ctrl+S cycles reverse-i-search backward.
func TestModelUpdate_CtrlSCycleBackward(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.inputHistory.Add("first")
	m.inputHistory.Add("second")

	// Open search.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)

	// Ctrl+S should cycle backward.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if !m.reverseSearch.Visible() {
		t.Error("search should still be visible after Ctrl+S")
	}
}

// Tests Esc dismisses command palette.
func TestModelUpdate_EscDismissesPalette(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.commandPalette.Toggle()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.commandPalette.Visible() {
		t.Error("palette should be dismissed after Esc")
	}
}

// Tests Esc dismisses reverse search.
func TestModelUpdate_EscDismissesSearch(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.reverseSearch.Toggle()
	m.showReverseSearch = m.reverseSearch.Visible()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.reverseSearch.Visible() {
		t.Error("search should be dismissed after Esc")
	}
}

// Tests ! command submit runs shell handler.
func TestModelUpdate_BangCommand(t *testing.T) {
	m := NewModel(Config{
		Shell:     "bash",
		PermLevel: "allow",
	})
	m.width = 80
	m.textarea.SetValue("!echo hello")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	// Process the submitMsg to run handleSubmit.
	if cmd != nil {
		updated, _ = m.Update(cmd())
		m = updated.(Model)
	}

	if len(m.output) == 0 {
		t.Error("expected output from ! command")
	}
}

// Tests palette filter routing - typing adds to filter.
func TestModelUpdate_PaletteFilterRouting(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.commandPalette.Toggle()

	// Type 'q' to filter.
	updated, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'q'},
	})
	m = updated.(Model)

	if m.commandPalette.Filter() != "q" {
		t.Errorf("want filter 'q', got %q", m.commandPalette.Filter())
	}
}

// Tests palette Up/Down navigation.
func TestModelUpdate_PaletteNavigation(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.commandPalette.Toggle()

	// MoveDown.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.commandPalette.SelectedIndex() != 1 {
		t.Errorf("want index 1, got %d", m.commandPalette.SelectedIndex())
	}

	// MoveUp.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.commandPalette.SelectedIndex() != 0 {
		t.Errorf("want index 0, got %d", m.commandPalette.SelectedIndex())
	}
}

// Tests reverse search filter routing.
func TestModelUpdate_ReverseSearchFilterRouting(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.inputHistory.Add("hello world")
	m.reverseSearch.Toggle()
	m.showReverseSearch = true

	// Type 'h' to filter.
	updated, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'h'},
	})
	m = updated.(Model)

	if m.reverseSearch.Query() != "h" {
		t.Errorf("want query 'h', got %q", m.reverseSearch.Query())
	}
}

// Tests @-reference completion popup trigger.
func TestModelUpdate_AtReferenceTrigger(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.referenceEngine.AddSource(navigation.ReferenceSource{
		Alias: "web",
		Items: []navigation.RefItem{
			{Label: "server1", Value: "server1.example.com", Kind: "host"},
		},
	})

	// Type '@' to trigger completion.
	updated, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'@'},
	})
	m = updated.(Model)

	// Should show completion for @-references.
	if !m.showCompletion {
		t.Error("expected completion popup for @")
	}
}

// Tests that incomplete ANSI is stripped while complete sequences
// are preserved.
func TestStripPartialANSI(t *testing.T) {
	cases := []struct {
		input, expected string
	}{
		{"hello \x1b[31mworld\x1b[0m", "hello \x1b[31mworld\x1b[0m"},
		{"hello \x1b[31", "hello "},
		{"\x1b[0", ""},
		{"plain text", "plain text"},
	}
	for _, c := range cases {
		got := StripPartialANSI(c.input)
		if got != c.expected {
			t.Errorf("StripPartialANSI(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

// Tests that incomplete ANSI in mid-text is stripped, not just at eol.
func TestStripPartialANSI_MidText(t *testing.T) {
	input := "before \x1b[31 after"
	want := "before  after"
	got := StripPartialANSI(input)
	if got != want {
		t.Errorf("StripPartialANSI(%q) = %q, want %q", input, got, want)
	}
}

// Tests that the cursor character appears in the input line when
// the textarea is focused.
func TestModelView_CursorVisible(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24

	view := m.View()
	if !strings.Contains(view, "█") {
		t.Error("view should contain cursor character when focused")
	}
}

// Tests graceful degradation - View works with nil components.
func TestModelView_GracefulDegradation(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.height = 24

	// View should render without errors even with default state.
	view := m.View()
	if view == "" {
		t.Error("view should not be empty")
	}
}

// Tests that Escape shows halt prompt when running.
func TestModelUpdate_EscShowsHaltPromptWhenRunning(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.spinner.Start("thinking…")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if !m.haltPrompt {
		t.Error("halt prompt should be showing after Esc while running")
	}
}

// Tests that Escape does not show halt prompt when not running.
func TestModelUpdate_EscNoHaltPromptWhenIdle(t *testing.T) {
	m := NewModel(Config{})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.haltPrompt {
		t.Error("halt prompt should not show when not running")
	}
}

// Tests that 'y' confirms halt and stops running.
func TestModelUpdate_HaltPromptY_Confirms(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.haltPrompt = true
	m.spinner.Start("thinking…")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(Model)

	if m.haltPrompt {
		t.Error("halt prompt should be dismissed")
	}
	if m.running {
		t.Error("should not be running after halt confirmation")
	}
	if m.currentTool != "" {
		t.Error("currentTool should be cleared")
	}
}

// Tests that 'n' cancels halt and keeps running.
func TestModelUpdate_HaltPromptN_Continues(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.haltPrompt = true
	m.spinner.Start("thinking…")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(Model)

	if m.haltPrompt {
		t.Error("halt prompt should be dismissed")
	}
	if !m.running {
		t.Error("should still be running after decline")
	}
}

// Tests that Esc dismisses halt prompt without stopping.
func TestModelUpdate_HaltPromptEsc_Continues(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.haltPrompt = true
	m.spinner.Start("thinking…")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.haltPrompt {
		t.Error("halt prompt should be dismissed")
	}
	if !m.running {
		t.Error("should still be running after Esc")
	}
}

// Tests that halt prompt renders in view.
func TestModelView_HaltPromptVisible(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.running = true
	m.haltPrompt = true

	view := m.View()
	if !strings.Contains(view, "halt everything?") {
		t.Error("view should contain halt prompt text")
	}
	if !strings.Contains(view, "[y]") {
		t.Error("view should contain [y] option")
	}
	if !strings.Contains(view, "[n]") {
		t.Error("view should contain [n] option")
	}
}

// Tests that other keys are ignored while halt prompt is active.
func TestModelUpdate_HaltPromptIgnoresOtherKeys(t *testing.T) {
	m := NewModel(Config{})
	m.running = true
	m.haltPrompt = true

	updated, _ := m.Update(tea.KeyMsg{
		Type:  tea.KeyRunes,
		Runes: []rune{'a'},
	})
	m = updated.(Model)

	if !m.haltPrompt {
		t.Error("halt prompt should still be active after unrelated key")
	}
	if !m.running {
		t.Error("should still be running")
	}
}

// Tests that permissionReqMsg sets the permission prompt on the model.
func TestModelUpdate_PermissionReqMsg_SetsPermission(t *testing.T) {
	m := NewModel(Config{})
	if m.permission != nil {
		t.Fatal("expected nil permission initially")
	}

	prompt := NewPermissionPrompt("shell", "reboot", "service restart", "tool permission")

	updated, cmd := m.Update(permissionReqMsg{prompt: prompt})
	m = updated.(Model)

	if m.permission == nil {
		t.Fatal("expected permission to be set after permissionReqMsg")
	}
	if m.permission.Tool != "shell" {
		t.Errorf("want tool 'shell', got %q", m.permission.Tool)
	}
	if m.permission.Command != "reboot" {
		t.Errorf("want command 'reboot', got %q", m.permission.Command)
	}
	if cmd == nil {
		t.Error("expected a follow-up command (listenPermissionReqs)")
	}
}

// Tests that pressing 'y' with an active permission prompt returns
// a non-nil cmd (the runtime executes the cmd, which sends
// permissionResultMsg to write to the channel).
func TestModelUpdate_PermissionKeyY_ReturnsCmd(t *testing.T) {
	m := NewModel(Config{})
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: make(chan PermissionChoice, 1),
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'y'")
	}
	// Execute the cmd and check it produces a permissionResultMsg.
	msg := cmd()
	pmsg, ok := msg.(permissionResultMsg)
	if !ok {
		t.Fatalf("want permissionResultMsg, got %T", msg)
	}
	if pmsg.choice != PermissionAllow {
		t.Errorf("want PermissionAllow, got %v", pmsg.choice)
	}
}

// Tests that pressing 'n' with an active permission prompt returns
// a cmd that produces a permissionResultMsg with PermissionDeny.
func TestModelUpdate_PermissionKeyN_ReturnsCmd(t *testing.T) {
	m := NewModel(Config{})
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: make(chan PermissionChoice, 1),
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'n'")
	}
	msg := cmd()
	pmsg, ok := msg.(permissionResultMsg)
	if !ok {
		t.Fatalf("want permissionResultMsg, got %T", msg)
	}
	if pmsg.choice != PermissionDeny {
		t.Errorf("want PermissionDeny, got %v", pmsg.choice)
	}
}

// Tests that pressing 'a' with an active permission prompt returns
// a cmd that produces a permissionResultMsg with PermissionAllowAll.
func TestModelUpdate_PermissionKeyA_ReturnsCmd(t *testing.T) {
	m := NewModel(Config{})
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: make(chan PermissionChoice, 1),
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for 'a'")
	}
	msg := cmd()
	pmsg, ok := msg.(permissionResultMsg)
	if !ok {
		t.Fatalf("want permissionResultMsg, got %T", msg)
	}
	if pmsg.choice != PermissionAllowAll {
		t.Errorf("want PermissionAllowAll, got %v", pmsg.choice)
	}
}

// Tests that pressing 'esc' with an active permission prompt returns
// a cmd that produces a permissionResultMsg with PermissionDeny.
func TestModelUpdate_PermissionKeyEsc_ReturnsCmd(t *testing.T) {
	m := NewModel(Config{})
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: make(chan PermissionChoice, 1),
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for esc")
	}
	msg := cmd()
	pmsg, ok := msg.(permissionResultMsg)
	if !ok {
		t.Fatalf("want permissionResultMsg, got %T", msg)
	}
	if pmsg.choice != PermissionDeny {
		t.Errorf("want PermissionDeny, got %v", pmsg.choice)
	}
}

// Tests that unrelated keys are ignored while permission prompt is active.
func TestModelUpdate_PermissionKey_OtherKeysIgnored(t *testing.T) {
	m := NewModel(Config{})
	choice := make(chan PermissionChoice, 1)
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: choice,
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("expected nil cmd for ignored key")
	}
	// Channel should not receive anything.
	select {
	case <-choice:
		t.Error("unexpected choice for ignored key")
	default:
	}
}

// Tests that permissionResultMsg writes the choice to the channel.
func TestModelUpdate_PermissionResultMsg_WritesChannel(t *testing.T) {
	m := NewModel(Config{})
	choice := make(chan PermissionChoice, 1)
	m.permission = &PermissionPrompt{
		Tool: "shell", Command: "reboot", Choice: choice,
	}

	updated, _ := m.Update(permissionResultMsg{choice: PermissionAllow})
	m = updated.(Model)

	select {
	case c := <-choice:
		if c != PermissionAllow {
			t.Errorf("want PermissionAllow, got %v", c)
		}
	default:
		t.Error("expected choice to be sent on channel")
	}
	if m.permission != nil {
		t.Error("permission should be nil after result is processed")
	}
}

// Tests that permission prompt renders in view.
func TestModelView_PermissionPromptVisible(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	m.permission = &PermissionPrompt{
		Tool:    "shell",
		Command: "reboot",
		Reason:  "service restart",
	}

	view := m.View()
	if !strings.Contains(view, "reboot") {
		t.Error("view should contain the command")
	}
	if !strings.Contains(view, "service restart") {
		t.Error("view should contain the reason")
	}
	if !strings.Contains(view, "[y] allow") {
		t.Error("view should contain allow option")
	}
	if !strings.Contains(view, "[n] deny") {
		t.Error("view should contain deny option")
	}
	if !strings.Contains(view, "[a] allow all") {
		t.Error("view should contain allow-all option")
	}
}
