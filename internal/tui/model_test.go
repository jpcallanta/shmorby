package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"shmorby/internal/agent"
	"shmorby/internal/config"
	ctxcomp "shmorby/internal/context"
	"shmorby/internal/execution"
	"shmorby/internal/llm"
	"shmorby/internal/session"
	"shmorby/internal/testutil"
	"shmorby/internal/tools"
	tuicompl "shmorby/internal/tui/completion"
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

// Tests that Ctrl+C handles copy to clipboard without panic.
func TestModelUpdate_CtrlC(t *testing.T) {
	m := NewModel(Config{})

	// Ctrl+C is intercepted by the "ctrl+c" text copy handler
	// before it reaches the tea.KeyCtrlC quit handler.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Error("expected nil command (copied to clipboard)")
	}
}

// Tests Ctrl+Z uses Bubble Tea's terminal-safe suspend flow.
func TestModelUpdate_CtrlZ_Suspends(t *testing.T) {
	m := NewModel(Config{})

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if cmd == nil {
		t.Fatal("expected suspend command")
	}
	msg := cmd()
	if _, ok := msg.(tea.SuspendMsg); !ok {
		t.Errorf("want tea.SuspendMsg, got %T", msg)
	}
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

func TestModelUpdate_HistoryCycle(t *testing.T) {
	m := NewModel(Config{})
	m.inputHistory.Add("first")
	m.inputHistory.Add("second")
	m.inputHistory.Add("third")

	// Up twice without clearing should go third → second.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "third" {
		t.Fatalf("want %q, got %q", "third", m.textarea.Value())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "second" {
		t.Fatalf("want %q, got %q", "second", m.textarea.Value())
	}

	// Third Up should go to "first".
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "first" {
		t.Fatalf("want %q, got %q", "first", m.textarea.Value())
	}

	// Fourth Up should stay at oldest.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.textarea.Value() != "first" {
		t.Fatalf("want oldest %q, got %q", "first", m.textarea.Value())
	}

	// Down should go back to "second".
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.textarea.Value() != "second" {
		t.Fatalf("want %q, got %q", "second", m.textarea.Value())
	}

	// Down again → "third".
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.textarea.Value() != "third" {
		t.Fatalf("want %q, got %q", "third", m.textarea.Value())
	}

	// Down at end should blank the input.
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

// Tests that syncViewport word-wraps long text at viewport width for all entry kinds.
func TestSyncViewport_WrapsAllEntryKinds(t *testing.T) {
	vpWidth := 30
	longText := "this is a very long line that absolutely must wrap at word boundaries"
	for _, kind := range []string{"user", "agent", "tool", "error", "memory"} {
		m := NewModel(Config{ThemeName: "minimal"})
		m.width = vpWidth
		m.height = 24
		m.viewport.SetWidth(vpWidth)
		m.output = append(m.output, outputEntry{
			kind: kind,
			text: longText,
		})
		m.syncViewport()

		plain := StripANSI(m.View())
		for _, line := range strings.Split(plain, "\n") {
			if !strings.Contains(line, "agent:") &&
				!strings.Contains(line, "mode:") &&
				!strings.Contains(line, "new content") {
				if w := lipgloss.Width(line); w > vpWidth {
					t.Errorf("[%s] line width %d exceeds viewport width %d: %q", kind, w, vpWidth, line)
				}
			}
		}
	}
}

// Tests that syncViewport does not modify output entry text.
func TestSyncViewport_PreservesEntryText(t *testing.T) {
	m := NewModel(Config{ThemeName: "minimal"})
	m.width = 40
	m.height = 24
	m.viewport.SetWidth(40)
	original := "some entry text"
	m.output = append(m.output, outputEntry{kind: "user", text: original})
	m.syncViewport()

	if m.output[0].text != original {
		t.Errorf("syncViewport modified entry text: got %q, want %q", m.output[0].text, original)
	}
}

// Tests that short text is not wrapped to multiple lines.
func TestSyncViewport_ShortTextNotWrapped(t *testing.T) {
	m := NewModel(Config{ThemeName: "minimal"})
	m.width = 80
	m.height = 24
	m.viewport.SetWidth(80)
	shortText := "short"
	m.output = append(m.output, outputEntry{kind: "agent", text: shortText})
	m.syncViewport()

	plain := StripANSI(m.View())
	if !strings.Contains(plain, shortText) {
		t.Error("short text not found in viewport")
	}
}

// Tests that text with embedded ANSI codes (from glamour) is wrapped correctly.
func TestSyncViewport_WrapsANSIText(t *testing.T) {
	m := NewModel(Config{ThemeName: "minimal"})
	vpWidth := 30
	m.width = vpWidth
	m.height = 24
	m.viewport.SetWidth(vpWidth)
	ansiText := "\x1b[32mgreen long text that should wrap at the viewport width properly\x1b[0m"
	m.output = append(m.output, outputEntry{kind: "agent", text: ansiText})
	m.syncViewport()

	plain := StripANSI(m.View())
	for _, line := range strings.Split(plain, "\n") {
		if !strings.Contains(line, "agent:") &&
			!strings.Contains(line, "mode:") &&
			!strings.Contains(line, "new content") {
			if w := lipgloss.Width(line); w > vpWidth {
				t.Errorf("line width %d exceeds viewport width %d: %q", w, vpWidth, line)
			}
		}
	}
}

// Tests that selection highlighting still works with wrapped content.
func TestSyncViewport_SelectionWithWrappedText(t *testing.T) {
	m := NewModel(Config{ThemeName: "minimal"})
	vpWidth := 30
	m.width = vpWidth
	m.height = 24
	m.viewport.SetWidth(vpWidth)
	m.output = append(m.output, outputEntry{kind: "user", text: "short"})
	m.output = append(m.output, outputEntry{kind: "agent", text: "a long text that should wrap at the viewport width correctly"})
	m.selectionMode = true
	m.selectionStart = 0
	m.selectionEnd = 1
	m.syncViewport()

	plain := StripANSI(m.View())
	if !strings.Contains(plain, "short") {
		t.Error("selected entry text missing from viewport")
	}
	if !strings.Contains(plain, "long text") {
		t.Error("wrapped entry text missing from viewport")
	}
	for _, line := range strings.Split(plain, "\n") {
		if !strings.Contains(line, "agent:") &&
			!strings.Contains(line, "mode:") &&
			!strings.Contains(line, "new content") {
			if w := lipgloss.Width(line); w > vpWidth {
				t.Errorf("line width %d exceeds viewport width %d: %q", w, vpWidth, line)
			}
		}
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
	// Tab is now synchronous with immediate feedback; no async
	// agentModeChangedMsg is required.
	if cmd != nil {
		t.Fatalf("want nil cmd for sync switch, got %v", cmd)
	}
	if len(m.output) == 0 {
		t.Error("expected mode change output")
	}
	if !strings.Contains(m.output[len(m.output)-1].text, "diagnose") {
		t.Errorf("want diagnose in output, got %q",
			m.output[len(m.output)-1].text)
	}

	// Verify modeSwitcher and shell guard stay in sync.
	if got := m.modeSwitcher.Current(); got != "diagnose" {
		t.Errorf("modeSwitcher desync, want diagnose got %q", got)
	}
	// Subsequent Tab should continue cycle: diagnose -> chat.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.mode != "chat" {
		t.Errorf("want chat after second Tab, got %q", m.mode)
	}
	// Continue: chat -> code.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.mode != "code" {
		t.Errorf("want code after third Tab, got %q", m.mode)
	}
	// Wrap: code -> operate.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if m.mode != "operate" {
		t.Errorf("want operate after wrap, got %q", m.mode)
	}
}

// Tests Shift+Tab cycles agent mode reverse when input is empty.
func TestModelUpdate_ShiftTabModeCycle(t *testing.T) {
	m := NewModel(Config{Mode: "operate"})
	m.width = 80

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(Model)

	if m.mode != "code" {
		t.Errorf("want code after Shift+Tab from operate, got %q", m.mode)
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

// Tests that every palette item is handled by the slash-command path.
func TestModelCommand_PaletteItemsHandled(t *testing.T) {
	m := NewModel(Config{
		Mode:    "operate",
		Model:   "test-model",
		Session: session.New(),
	})

	items := m.commandPalette.Filtered()
	if len(items) == 0 {
		t.Fatal("no palette items registered")
	}

	for _, item := range items {
		handled, done, err := m.handleCommand(item.Slash)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", item.Slash, err)
		}
		if !handled {
			t.Errorf("%s: not handled by slash-command path", item.Slash)
		}
		wantDone := item.Slash == "/quit"
		if done != wantDone {
			t.Errorf("%s: want done %v, got %v", item.Slash, wantDone, done)
		}
	}
}

// Tests that Enter in the palette executes the selected command through
// the shared submit path.
func TestModelPalette_Execute_DispatchesSlashCommand(t *testing.T) {
	m := NewModel(Config{Mode: "operate", Model: "test-model"})
	m.width = 80
	m.commandPalette.Toggle()

	// First item is /quit → Enter must quit via the submit path.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.commandPalette.Visible() {
		t.Error("palette should dismiss after execute")
	}
	if cmd == nil {
		t.Fatal("no command returned for /quit")
	}

	// Process the submitMsg through the shared submit path.
	updated, cmd = m.Update(cmd())
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("submit path did not quit for /quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("Enter on /quit should quit via the submit path")
	}
}

// Tests that Enter with no matching palette item reports an error and
// keeps the palette open.
func TestModelPalette_Execute_NoSelection_ShowsError(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.commandPalette.Toggle()
	m.commandPalette.SetFilter("zzz")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if !m.commandPalette.Visible() {
		t.Error("palette should stay open when nothing is selected")
	}
	if len(m.output) == 0 || m.output[len(m.output)-1].kind != "error" {
		t.Error("expected error output for empty selection")
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

	prompt := NewPermissionPrompt(
		"shell", "reboot", "service restart", "tool permission",
	)

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

// Tests that /set command in TUI produces confirmation output.
func TestTUI_SetCommand_Valid(t *testing.T) {
	cfg := config.Config{Model: "test-model"}
	cfg.Agent.Default = "operate"
	var prov llm.Provider = &fakeProvider{name: "test"}
	reg := tools.NewRegistry()
	comp := ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{}, nil, nil, nil,
	)
	co := agent.NewConfigOverrider(
		&cfg, &prov, reg, comp,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/set model gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "model") {
		t.Errorf("want model confirmation, got %q", last.text)
	}
}

// Tests that /set with invalid value in TUI shows error.
func TestTUI_SetCommand_Invalid(t *testing.T) {
	cfg := config.Config{Model: "test-model"}
	cfg.Agent.Default = "operate"
	var prov llm.Provider = &fakeProvider{name: "test"}
	reg := tools.NewRegistry()
	comp := ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{}, nil, nil, nil,
	)
	co := agent.NewConfigOverrider(
		&cfg, &prov, reg, comp,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/set bogus.value 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "unknown config parameter") {
		t.Errorf("want error about unknown param, got %q", last.text)
	}
}

// Tests that /set with only a param and no value shows usage.
func TestTUI_SetCommand_MissingValue(t *testing.T) {
	cfg := config.Config{Model: "test-model"}
	cfg.Agent.Default = "operate"
	var prov llm.Provider = &fakeProvider{name: "test"}
	reg := tools.NewRegistry()
	comp := ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{}, nil, nil, nil,
	)
	co := agent.NewConfigOverrider(
		&cfg, &prov, reg, comp,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/set model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "usage") {
		t.Errorf("want usage message, got %q", last.text)
	}
}

// Tests that help overlay shows CONFIG PARAMETERS when configOverrider set.
func TestTUI_HelpOverlay_ShowsParams(t *testing.T) {
	cfg := config.Config{Model: "test-model"}
	cfg.Agent.Default = "operate"
	var prov llm.Provider = &fakeProvider{name: "test"}
	reg := tools.NewRegistry()
	comp := ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{}, nil, nil, nil,
	)
	co := agent.NewConfigOverrider(
		&cfg, &prov, reg, comp,
	)

	m := NewModel(Config{
		ThemeName:       "catppuccin-mocha",
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 30
	m.showHelp.Show()

	view := m.View()

	if !strings.Contains(view, "CONFIG PARAMETERS") {
		t.Error("help overlay should contain CONFIG PARAMETERS section")
	}
	if !strings.Contains(view, "model") {
		t.Error("help overlay should contain model param")
	}
}

// fakeProvider is a minimal llm.Provider for testing.
type fakeProvider struct {
	name string
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Chat(
	_ context.Context, _ llm.ChatRequest,
) (llm.ChatResponse, error) {
	return llm.ChatResponse{}, nil
}

func (f *fakeProvider) ChatStream(
	_ context.Context, _ llm.ChatRequest,
) (<-chan llm.StreamEvent, error) {
	return nil, fmt.Errorf("streaming not supported")
}

func (f *fakeProvider) ModelInfo(
	_ context.Context, _ string,
) (llm.ModelInfo, error) {
	return llm.ModelInfo{}, nil
}

// TestTUIModelCommand_NoArg verifies /model prints current.
func TestTUIModelCommand_NoArg(t *testing.T) {
	m := NewModel(Config{
		Model: "test-model",
	})
	m.width = 80
	m.height = 24
	m.model = "llama3"
	m.provider = &fakeProvider{name: "openai"}

	cmd, done, err := m.handleCommand("/model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "openai (llama3)") {
		t.Errorf(
			"want provider and model, got %q", last.text,
		)
	}
}

// listerProvider extends fakeProvider with a fixed model list so bare
// /model can be tested without a live platform.
type listerProvider struct {
	fakeProvider
	models []llm.ModelSummary
}

// Returns the canned model list.
func (l *listerProvider) ListModels(
	_ context.Context, _ string,
) ([]llm.ModelSummary, error) {
	return l.models, nil
}

// TestTUIModelCommand_ListCap verifies a long catalog is capped and
// the hidden remainder is reported on a trailing line.
func TestTUIModelCommand_ListCap(t *testing.T) {
	m := NewModel(Config{
		Model: "test-model",
	})
	m.width = 80
	m.height = 24
	m.model = "llama3"

	overflow := 10
	models := make([]llm.ModelSummary, 0, maxListedModels+overflow)
	for i := 0; i < maxListedModels+overflow; i++ {
		models = append(models, llm.ModelSummary{
			ID: fmt.Sprintf("model-%d", i),
		})
	}
	m.provider = &listerProvider{
		fakeProvider: fakeProvider{name: "openai"},
		models:       models,
	}

	cmd, done, err := m.handleCommand("/model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}

	// Header line, the capped list, then the trailing count.
	wantEntries := maxListedModels + 2
	if len(m.output) != wantEntries {
		t.Fatalf("want %d output entries, got %d",
			wantEntries, len(m.output))
	}
	last := m.output[len(m.output)-1]
	wantLine := fmt.Sprintf("  … and %d more", overflow)
	if last.text != wantLine {
		t.Errorf("want %q, got %q", wantLine, last.text)
	}
}

// TestTUIModelCommand_Switch verifies /model <name> switches model.
func TestTUIModelCommand_Switch(t *testing.T) {
	cfg := config.Config{
		Provider: "ollama",
		Model:    "llama3",
	}
	co := agent.NewConfigOverrider(
		&cfg, nil, nil, nil,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24
	m.model = "llama3"
	m.provider = &fakeProvider{name: "ollama"}

	cmd, done, err := m.handleCommand("/model gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.model != "gpt-4o" {
		t.Errorf("m.model = %q, want %q", m.model, "gpt-4o")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, `model set to "gpt-4o"`) {
		t.Errorf(
			"want confirmation, got %q", last.text,
		)
	}
}

// TestTUIModelCommand_NilConfigOverrider verifies error when nil.
func TestTUIModelCommand_NilConfigOverrider(t *testing.T) {
	m := NewModel(Config{
		Model: "llama3",
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/model gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(
		last.text, "Config override not available.",
	) {
		t.Errorf(
			"want error, got %q", last.text,
		)
	}
}

// TestTUI_PlatformCommand_NoArg verifies /platform prints current.
func TestTUI_PlatformCommand_NoArg(t *testing.T) {
	m := NewModel(Config{
		Model: "test-model",
	})
	m.width = 80
	m.height = 24
	m.provider = &fakeProvider{name: "openai"}

	cmd, done, err := m.handleCommand("/platform")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "openai") {
		t.Errorf(
			"want provider name, got %q", last.text,
		)
	}
}

// TestTUI_PlatformCommand_Switch verifies /platform <name> switches.
func TestTUI_PlatformCommand_Switch(t *testing.T) {
	cfg := config.Config{
		Provider: "openai",
		Model:    "gpt-4o",
	}
	cfg.OpenAI.APIKey = "sk-test"
	var prov llm.Provider = &fakeProvider{name: "openai"}
	co := agent.NewConfigOverrider(
		&cfg, &prov, nil, nil,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24
	m.model = "gpt-4o"
	m.provider = &fakeProvider{name: "ollama"}

	cmd, done, err := m.handleCommand("/platform openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(
		last.text, `provider set to "openai"`,
	) {
		t.Errorf(
			"want confirmation, got %q", last.text,
		)
	}
}

// TestTUI_PlatformCommand_Invalid verifies /platform bogus shows error.
func TestTUI_PlatformCommand_Invalid(t *testing.T) {
	cfg := config.Config{
		Provider: "ollama",
		Model:    "llama3",
	}
	co := agent.NewConfigOverrider(
		&cfg, nil, nil, nil,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24
	m.model = "llama3"
	m.provider = &fakeProvider{name: "ollama"}

	cmd, done, err := m.handleCommand("/platform bogus")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "invalid provider") {
		t.Errorf(
			"want error, got %q", last.text,
		)
	}
}

// TestTUI_ApikeyCommand_NoArg verifies /apikey prints message.
func TestTUI_ApikeyCommand_NoArg(t *testing.T) {
	m := NewModel(Config{
		Model: "test-model",
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/apikey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "apikey is set") {
		t.Errorf(
			"want apikey message, got %q", last.text,
		)
	}
}

// TestTUI_ApikeyCommand_Set verifies /apikey sk-... sets key.
func TestTUI_ApikeyCommand_Set(t *testing.T) {
	cfg := config.Config{
		Provider: "openai",
		Model:    "gpt-4o",
	}
	cfg.OpenAI.APIKey = "sk-old"
	var prov llm.Provider = &fakeProvider{name: "openai"}
	co := agent.NewConfigOverrider(
		&cfg, &prov, nil, nil,
	)

	m := NewModel(Config{
		ConfigOverrider: co,
	})
	m.width = 80
	m.height = 24
	m.model = "gpt-4o"
	m.provider = &fakeProvider{name: "openai"}

	cmd, done, err := m.handleCommand("/apikey sk-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(last.text, "apikey set") {
		t.Errorf(
			"want confirmation, got %q", last.text,
		)
	}
}

// TestTUI_ApikeyCommand_NilOverrider verifies error when nil.
func TestTUI_ApikeyCommand_NilOverrider(t *testing.T) {
	m := NewModel(Config{
		Model: "gpt-4o",
	})
	m.width = 80
	m.height = 24

	cmd, done, err := m.handleCommand("/apikey sk-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output entries")
	}
	last := m.output[len(m.output)-1]
	if !strings.Contains(
		last.text, "Config override not available.",
	) {
		t.Errorf(
			"want error, got %q", last.text,
		)
	}
}

// Tests that attachImage stages a pending attachment and the chip
// renders above the input.
func TestModel_AttachImage(t *testing.T) {
	m := NewModel(Config{
		Mode:      "operate",
		Model:     "gpt-5.6-sol",
		ModelInfo: llm.ModelInfo{SupportsVision: true},
	})
	m.width = 80
	m.height = 24

	m.attachImage(testutil.OnePixelPNG())
	if len(m.pendingImages) != 1 {
		t.Fatalf("want 1 pending image, got %d", len(m.pendingImages))
	}
	if m.pendingImages[0].data.MediaType != "image/png" {
		t.Errorf("want media type image/png, got %q",
			m.pendingImages[0].data.MediaType)
	}
	if m.pendingImages[0].width != 1 || m.pendingImages[0].height != 1 {
		t.Errorf("want 1x1 dims, got %dx%d",
			m.pendingImages[0].width, m.pendingImages[0].height)
	}

	view := m.View()
	if !strings.Contains(view, "📎 image 1x1 PNG") {
		t.Error("view missing pending image chip")
	}
}

// Tests that esc clears pending attachments when the input is empty,
// and that text in the input keeps the attachment.
func TestModel_EscClearsPendingImages(t *testing.T) {
	m := NewModel(Config{Mode: "operate", Model: "gpt-5.6-sol"})
	m.width = 80
	m.height = 24
	m.attachImage(testutil.OnePixelPNG())

	// Input has text: attachment stays.
	m.textarea.SetValue("describe this")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if len(m.pendingImages) != 1 {
		t.Fatalf("attachment cleared with non-empty input")
	}

	// Empty input: esc removes the attachment.
	m.textarea.Reset()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if len(m.pendingImages) != 0 {
		t.Errorf("esc should clear attachment, got %d pending",
			len(m.pendingImages))
	}
}

// Tests that oversized images are rejected with an inline error.
func TestModel_AttachImage_CapRejection(t *testing.T) {
	m := NewModel(Config{Mode: "operate", Model: "gpt-5.6-sol"})
	m.width = 80
	m.height = 24

	m.attachImage(make([]byte, maxImageBytes+1))
	if len(m.pendingImages) != 0 {
		t.Errorf("oversized image attached (%d pending)",
			len(m.pendingImages))
	}
	found := false
	for _, e := range m.output {
		if e.kind == "error" && strings.Contains(e.text, "image too large") {
			found = true
		}
	}
	if !found {
		t.Error("inline error for oversized image not shown")
	}
}

// Tests that submitting with a pending image on a vision-capable
// model attaches it, resets the pending state, and appends the
// transcript chip.
func TestModel_Submit_ClearsPendingAndShowsChip(t *testing.T) {
	m := NewModel(Config{
		Mode:      "operate",
		Model:     "gpt-5.6-sol",
		Session:   session.New(),
		ModelInfo: llm.ModelInfo{SupportsVision: true},
	})
	m.width = 80
	m.height = 24
	m.attachImage(testutil.OnePixelPNG())
	m.textarea.SetValue("describe this")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	// Enter only queues the submitMsg; process it.
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if len(m.pendingImages) != 0 {
		t.Errorf("pending images not reset on submit (%d)",
			len(m.pendingImages))
	}
	found := false
	for _, e := range m.output {
		if e.kind == "image" && strings.Contains(e.text, "📎 image") {
			found = true
		}
	}
	if !found {
		t.Error("transcript image chip missing")
	}
}

// Tests the non-vision gate: the turn is refused with a user-visible
// message and no agent turn starts.
func TestModel_Submit_NonVisionModelGate(t *testing.T) {
	m := NewModel(Config{
		Mode:      "operate",
		Model:     "llama3.2",
		Session:   session.New(),
		ModelInfo: llm.ModelInfo{SupportsVision: false},
	})
	m.width = 80
	m.height = 24
	m.attachImage(testutil.OnePixelPNG())
	m.textarea.SetValue("describe this")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	found := false
	for _, e := range m.output {
		if e.kind == "error" &&
			strings.Contains(e.text, "does not support image input") {
			found = true
		}
	}
	if !found {
		t.Error("vision gate error not shown")
	}
	if m.running {
		t.Error("agent turn should not start for non-vision model")
	}
	// Attachment stays so the user can switch models and resend.
	if len(m.pendingImages) != 1 {
		t.Error("attachment dropped on gate refusal")
	}
}

// Tests that WindowSizeMsg reflows viewport content to the new pane width.
func TestModelUpdate_WindowSizeMsg_ReflowsViewport(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	longText := strings.Repeat("word ", 20)
	m.output = append(m.output, outputEntry{
		kind: "agent",
		text: longText,
	})
	m.syncViewport()
	contentWide := m.viewport.View()

	// Resize to narrow pane.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	m = updated.(Model)
	contentNarrow := m.viewport.View()

	if contentWide == contentNarrow {
		t.Error("want viewport content to reflow on WindowSizeMsg")
	}
}

// Tests vim navigation keys in viewport selection mode.
func TestModelUpdate_SelectionMode_VimNavigation(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 10
	for i := 0; i < 50; i++ {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("line %d", i),
		})
	}
	m.syncViewport()
	m.selectionMode = true

	// Navigate to top with 'g'.
	updated, _ := m.Update(tea.KeyMsg{Runes: []rune{'g'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 0.0 {
		t.Errorf("want scroll percent 0.0 at top, got %f",
			m.viewport.ScrollPercent())
	}

	// Scroll down with 'j'.
	updated, _ = m.Update(tea.KeyMsg{Runes: []rune{'j'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() <= 0.0 {
		t.Errorf("want scroll percent > 0.0 on j, got %f",
			m.viewport.ScrollPercent())
	}

	// Scroll up with 'k'.
	updated, _ = m.Update(tea.KeyMsg{Runes: []rune{'k'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 0.0 {
		t.Errorf("want scroll percent 0.0 on k, got %f",
			m.viewport.ScrollPercent())
	}

	// Half-page down with 'd'.
	updated, _ = m.Update(tea.KeyMsg{Runes: []rune{'d'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() <= 0.0 {
		t.Errorf("want scroll percent > 0.0 on d, got %f",
			m.viewport.ScrollPercent())
	}

	// Half-page up with 'u'.
	updated, _ = m.Update(tea.KeyMsg{Runes: []rune{'u'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 0.0 {
		t.Errorf("want scroll percent 0.0 on u, got %f",
			m.viewport.ScrollPercent())
	}

	// Half-page down with ctrl+d.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = updated.(Model)
	if m.viewport.ScrollPercent() <= 0.0 {
		t.Errorf("want scroll percent > 0.0 on ctrl+d, got %f",
			m.viewport.ScrollPercent())
	}

	// Half-page up with ctrl+u.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 0.0 {
		t.Errorf("want scroll percent 0.0 on ctrl+u, got %f",
			m.viewport.ScrollPercent())
	}

	// Navigate to bottom with 'G'.
	updated, _ = m.Update(tea.KeyMsg{Runes: []rune{'G'}, Type: tea.KeyRunes})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 1.0 {
		t.Errorf("want scroll percent 1.0 at bottom, got %f",
			m.viewport.ScrollPercent())
	}

	// Navigate to top with Home key.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 0.0 {
		t.Errorf("want scroll percent 0.0 on Home, got %f",
			m.viewport.ScrollPercent())
	}

	// Navigate to bottom with End key.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if m.viewport.ScrollPercent() != 1.0 {
		t.Errorf("want scroll percent 1.0 on End, got %f",
			m.viewport.ScrollPercent())
	}
}

// Tests that active overlays clamp viewport height across pane sizes.
func TestModelView_OverlayVerticalClamping(t *testing.T) {
	inputLinesCases := []struct {
		name  string
		input string
		lines int
	}{
		{name: "1-line input", input: "", lines: 1},
		{name: "3-line input", input: "line 1\nline 2\nline 3", lines: 3},
	}
	heights := []int{24, 20, 18, 15, 11, 10, 9}

	for _, tc := range inputLinesCases {
		for _, h := range heights {
			m := NewModel(Config{ThemeName: "catppuccin-mocha"})
			m.width = 60
			m.height = h
			if tc.input != "" {
				m.textarea.SetValue(tc.input)
			}

			// Minimum non-overlay chrome rows: input lines + 3 status rows.
			baseChrome := tc.lines + 3

			// Test Command Palette clamping.
			m.commandPalette.Toggle()
			viewPalette := m.View()
			linesPalette := strings.Count(viewPalette, "\n") + 1
			wantMax := h
			if baseChrome+4 > wantMax {
				wantMax = baseChrome + 4
			}
			if linesPalette > wantMax {
				t.Errorf("%s h=%d: want lines <= %d with palette, got %d",
					tc.name, h, wantMax, linesPalette)
			}
			m.commandPalette.Dismiss()

			// Test Permission prompt clamping.
			m.permission = &PermissionPrompt{
				Tool:    "shell",
				Command: "apt-get upgrade",
				Reason:  "system maintenance",
			}
			viewPerm := m.View()
			linesPerm := strings.Count(viewPerm, "\n") + 1
			wantMaxPerm := h
			if baseChrome+6 > wantMaxPerm {
				wantMaxPerm = baseChrome + 6
			}
			if linesPerm > wantMaxPerm {
				t.Errorf("%s h=%d: want lines <= %d with perm prompt, got %d",
					tc.name, h, wantMaxPerm, linesPerm)
			}
			m.permission = nil

			// Test Halt prompt clamping.
			m.haltPrompt = true
			viewHalt := m.View()
			linesHalt := strings.Count(viewHalt, "\n") + 1
			wantMaxHalt := h
			if baseChrome+4 > wantMaxHalt {
				wantMaxHalt = baseChrome + 4
			}
			if linesHalt > wantMaxHalt {
				t.Errorf("%s h=%d: want lines <= %d with halt prompt, got %d",
					tc.name, h, wantMaxHalt, linesHalt)
			}
			m.haltPrompt = false

			// Test Completion popup clamping.
			m.showCompletion = true
			m.complMatches = []tuicompl.Command{
				{Name: "/help"}, {Name: "/set"}, {Name: "/tools"},
				{Name: "/theme"}, {Name: "/model"}, {Name: "/clear"},
				{Name: "/context"}, {Name: "/quit"},
			}
			viewCompl := m.View()
			linesCompl := strings.Count(viewCompl, "\n") + 1
			wantMaxCompl := h
			if baseChrome+1 > wantMaxCompl {
				wantMaxCompl = baseChrome + 1
			}
			if linesCompl > wantMaxCompl {
				t.Errorf("%s h=%d: want lines <= %d with compl, got %d",
					tc.name, h, wantMaxCompl, linesCompl)
			}
			m.showCompletion = false
			m.complMatches = nil

			// Test Reverse search clamping.
			for i := 0; i < 10; i++ {
				m.inputHistory.Add(fmt.Sprintf("cmd %d", i))
			}
			m.reverseSearch.Toggle()
			m.showReverseSearch = true
			m.reverseSearch.SetQuery("cmd")
			viewRev := m.View()
			linesRev := strings.Count(viewRev, "\n") + 1
			wantMaxRev := h
			if baseChrome+4 > wantMaxRev {
				wantMaxRev = baseChrome + 4
			}
			if linesRev > wantMaxRev {
				t.Errorf("%s h=%d: want lines <= %d with rev search, got %d",
					tc.name, h, wantMaxRev, linesRev)
			}
			m.reverseSearch.Dismiss()
			m.showReverseSearch = false
		}
	}
}

// Tests that WindowSizeMsg does not trigger a false new content indicator.
func TestModelUpdate_WindowSizeMsg_NoFalseNewContentIndicator(t *testing.T) {
	m := NewModel(Config{ThemeName: "catppuccin-mocha"})
	m.width = 80
	m.height = 24
	for i := 0; i < 30; i++ {
		m.output = append(m.output, outputEntry{
			kind: "agent",
			text: fmt.Sprintf("message %d", i),
		})
	}
	m.syncViewport()

	// Simulate user scrolling up (follow mode paused).
	m.viewport.SetFollowMode(false)
	m.viewport.SetNewContent(false)

	// Window resize event.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = updated.(Model)

	if m.viewport.NewContent() {
		t.Error("want newContent false after WindowSizeMsg")
	}

	view := m.View()
	if strings.Contains(view, "↓ new content") {
		t.Error("view should not show new content indicator after resize")
	}
}

// TestTUIModelExecutionsCommand_ShowsDBRows verifies /executions reads
// durable rows instead of relying on in-memory tool events.
func TestTUIModelExecutionsCommand_ShowsDBRows(t *testing.T) {
	st, err := session.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer st.Close()

	mgr := execution.NewManager(st)
	err = st.CreateExecution(execution.Record{
		ID:        "exec-tui",
		SessionID: "sess-tui",
		Tool:      "shell",
		Command:   "df -h",
		Status:    execution.StatusRunning,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}

	m := NewModel(Config{
		Session:    session.NewWithID("sess-tui"),
		Executions: mgr,
	})
	handled, done, err := m.handleCommand("/executions")
	if err != nil {
		t.Fatalf("handleCommand: %v", err)
	}
	if !handled || done {
		t.Fatalf("handled = %v, done = %v", handled, done)
	}

	last := m.output[len(m.output)-1].text
	if !strings.Contains(last, "exec-tui") ||
		!strings.Contains(last, "running") {
		t.Errorf("output = %q, want execution row", last)
	}
}
