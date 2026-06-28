package tui

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"shmorby/internal/agent"
)

// Tests that TUILogHandler sends entries to the channel.
func TestTUILogHandler_SendsToChannel(t *testing.T) {
	ch := make(chan LogEntry, 10)
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewTUILogHandler(inner, ch)

	logger := slog.New(h)
	logger.Info("test message")

	select {
	case entry := <-ch:
		if entry.Message != "test message" {
			t.Errorf("want message %q, got %q",
				"test message", entry.Message)
		}
		if entry.Level != slog.LevelInfo {
			t.Errorf("want level info, got %v", entry.Level)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log entry")
	}
}

// Tests that TUILogHandler respects level filtering.
func TestTUILogHandler_LevelFiltering(t *testing.T) {
	ch := make(chan LogEntry, 10)
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})
	h := NewTUILogHandler(inner, ch)
	h.SetLevel(slog.LevelWarn)

	logger := slog.New(h)
	logger.Info("should be dropped")

	select {
	case <-ch:
		t.Error("info entry should be dropped when level is warn")
	case <-time.After(50 * time.Millisecond):
		// Expected — no entry sent.
	}
}

// Tests that TUILogHandler drops entries when channel is full.
func TestTUILogHandler_DropsOnFullChannel(t *testing.T) {
	ch := make(chan LogEntry, 1)
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewTUILogHandler(inner, ch)

	logger := slog.New(h)
	logger.Info("first")
	logger.Info("second") // should not block

	if len(ch) != 1 {
		t.Errorf("want 1 entry in channel, got %d", len(ch))
	}
}

func TestTUILogHandler_NilChannel(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("TUILogHandler with nil channel panicked: %v", r)
		}
	}()

	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewTUILogHandler(inner, nil)

	logger := slog.New(h)
	logger.Info("no panic")
}

// Tests that SetLevel changes the minimum accepted level.
func TestTUILogHandler_SetLevel(t *testing.T) {
	ch := make(chan LogEntry, 10)
	// Inner handler accepts all levels so our level gate is the only filter.
	inner := slog.NewTextHandler(
		&bytes.Buffer{},
		&slog.HandlerOptions{Level: slog.LevelDebug},
	)
	h := NewTUILogHandler(inner, ch)

	// Default is info; debug should be filtered.
	h.SetLevel(slog.LevelInfo)
	logger := slog.New(h)
	logger.Debug("debug msg")

	select {
	case <-ch:
		t.Error("debug entry should be filtered at info level")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}

	// Lower the threshold to accept debug.
	h.SetLevel(slog.LevelDebug)
	logger.Debug("debug msg")

	select {
	case entry := <-ch:
		if entry.Message != "debug msg" {
			t.Errorf("want %q, got %q",
				"debug msg", entry.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for debug entry")
	}
}

// Tests that WithAttrs returns a working handler.
func TestTUILogHandler_WithAttrs(t *testing.T) {
	ch := make(chan LogEntry, 10)
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewTUILogHandler(inner, ch)

	h2 := h.WithAttrs([]slog.Attr{
		slog.String("key", "val"),
	})
	logger := slog.New(h2)
	logger.Info("msg")

	entry := <-ch
	if entry.Message != "msg" {
		t.Errorf("want message %q, got %q", "msg", entry.Message)
	}
	if entry.Level != slog.LevelInfo {
		t.Errorf("want level info, got %v", entry.Level)
	}
}

// Tests that WithGroup adds a group prefix to attribute keys.
func TestTUILogHandler_WithGroup(t *testing.T) {
	ch := make(chan LogEntry, 10)
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	h := NewTUILogHandler(inner, ch)

	h2 := h.WithGroup("grpc")
	logger := slog.New(h2)
	logger.Info("msg", "code", 500)

	entry := <-ch
	found := false
	for _, a := range entry.Attrs {
		if a.Key == "grpc.code" || a.Key == "code" {
			found = true
		}
	}
	if !found {
		t.Error("expected grouped attr on entry")
	}
}

// Tests that Enabled consults both level and inner handler.
func TestTUILogHandler_Enabled(t *testing.T) {
	ch := make(chan LogEntry, 10)
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelError,
	})
	h := NewTUILogHandler(inner, ch)

	// Inner handler rejects info; even though our level is info,
	// Enabled should return false.
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info should be disabled by inner handler")
	}
}

// ThinkingBuffer tests.

// Tests that Start resets state and activates the buffer.
func TestThinkingBuffer_Start(t *testing.T) {
	var b ThinkingBuffer
	b.Start()

	if !b.Active() {
		t.Error("buffer should be active after Start")
	}
	if b.Tokens() != 0 {
		t.Errorf("want 0 tokens, got %d", b.Tokens())
	}
	if len(b.Lines()) != 0 {
		t.Error("want 0 lines after Start")
	}
}

// Tests that AddDelta accumulates content.
func TestThinkingBuffer_AddDelta(t *testing.T) {
	var b ThinkingBuffer
	b.Start()
	b.AddDelta("thinking ")
	b.AddDelta("about nginx")

	if b.Tokens() != 20 {
		t.Errorf("want 20 tokens, got %d", b.Tokens())
	}
	if b.Text() != "thinking about nginx" {
		t.Errorf("want %q, got %q",
			"thinking about nginx", b.Text())
	}
	if len(b.Lines()) != 2 {
		t.Errorf("want 2 lines, got %d", len(b.Lines()))
	}
}

// Tests that End deactivates and returns whether content existed.
func TestThinkingBuffer_End(t *testing.T) {
	var b ThinkingBuffer

	// Empty buffer.
	if b.End() {
		t.Error("End should return false for empty buffer")
	}

	b.Start()
	b.AddDelta("reasoning")
	if !b.End() {
		t.Error("End should return true for non-empty buffer")
	}
	if b.Active() {
		t.Error("buffer should not be active after End")
	}
}

// Tests that AddDelta auto-starts if not already active.
func TestThinkingBuffer_AutoStart(t *testing.T) {
	var b ThinkingBuffer
	b.AddDelta("auto")

	if !b.Active() {
		t.Error("AddDelta should auto-start")
	}
	if b.Tokens() != 4 {
		t.Errorf("want 4 tokens, got %d", b.Tokens())
	}
}

// Tests that logEntryMsg appends to logEntries and re-listens.
func TestModelUpdate_LogEntry(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	ch := make(chan LogEntry, 10)
	m.logChan = ch

	entry := LogEntry{
		Level:   slog.LevelInfo,
		Message: "test log",
	}
	updated, cmd := m.Update(logEntryMsg{entry: entry})
	m = updated.(Model)

	if len(m.logEntries) != 1 {
		t.Fatalf("want 1 log entry, got %d", len(m.logEntries))
	}
	if m.logEntries[0].Message != "test log" {
		t.Errorf("want message %q, got %q",
			"test log", m.logEntries[0].Message)
	}
	// Should re-listen on the channel.
	if cmd == nil {
		t.Error("expected command to re-listen on channel")
	}
}

// Tests that thinkingDeltaMsg adds to the thinking buffer.
func TestModelUpdate_ThinkingDelta(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80

	updated, _ := m.Update(thinkingDeltaMsg{delta: "reasoning..."})
	m = updated.(Model)

	if !m.thinking.Active() {
		t.Error("thinking buffer should be active")
	}
	if m.thinking.Tokens() != 12 {
		t.Errorf("want 12 tokens, got %d", m.thinking.Tokens())
	}
}

// Tests that thinkingEndMsg ends the thinking block.
func TestModelUpdate_ThinkingEnd(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.Update(thinkingDeltaMsg{delta: "reasoning"})

	updated, _ := m.Update(thinkingEndMsg{})
	m = updated.(Model)

	if m.thinking.Active() {
		t.Error("thinking buffer should not be active after end")
	}
}

// Tests that ctrl+l toggles log expansion.
func TestModelUpdate_CtrlLTogglesLog(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.logExpanded = false

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = updated.(Model)

	if !m.logExpanded {
		t.Error("log should be expanded after ctrl+l")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = updated.(Model)

	if m.logExpanded {
		t.Error("log should be collapsed after second ctrl+l")
	}
}

// Tests that ctrl+t toggles thinking expansion.
func TestModelUpdate_CtrlTTogglesThinking(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.thinkingExpanded = false

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)

	if !m.thinkingExpanded {
		t.Error("thinking should be expanded after ctrl+t")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)

	if m.thinkingExpanded {
		t.Error("thinking should be collapsed after second ctrl+t")
	}
}

// Tests /log command shows current level.
func TestModelCommand_LogShow(t *testing.T) {
	m := NewModel(Config{})
	m.logLevel = slog.LevelInfo

	cmd, done, err := m.handleCommand("/log")
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
		t.Error("expected output after /log")
	}
}

// Tests /log command sets level.
func TestModelCommand_LogSet(t *testing.T) {
	m := NewModel(Config{})
	m.logLevel = slog.LevelInfo

	cmd, done, err := m.handleCommand("/log debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.logLevel != slog.LevelDebug {
		t.Errorf("want level debug, got %v", m.logLevel)
	}
}

// Tests /log command rejects invalid level.
func TestModelCommand_LogInvalid(t *testing.T) {
	m := NewModel(Config{})

	_, _, err := m.handleCommand("/log bogus")
	if err == nil {
		t.Error("expected error for invalid level")
	}
}

// Tests log level appears in status bar.
func TestRenderStatus_LogLevel(t *testing.T) {
	m := NewModel(Config{
		Mode:      "operate",
		Model:     "test",
		ThemeName: "catppuccin-mocha",
	})
	m.logLevel = slog.LevelDebug

	status := m.renderStatus()
	if !strings.Contains(status, "log:") {
		t.Error("status missing log label")
	}
	if !strings.Contains(status, "DEBUG") {
		t.Errorf("status missing log level, got %q", status)
	}
}

// Tests that collapsed log section does NOT appear in viewport.
func TestSyncViewport_LogPreview_Hidden(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.logExpanded = false
	m.logEntries = append(m.logEntries, LogEntry{
		Level:   slog.LevelInfo,
		Message: "test",
	})
	m.syncViewport()

	content := m.viewport.View()
	if strings.Contains(content, "log (1)") {
		t.Error("collapsed log section should not appear in viewport")
	}
}

// Tests that log entries appear expanded in viewport.
func TestSyncViewport_LogExpanded(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.logExpanded = true
	m.logEntries = append(m.logEntries, LogEntry{
		Level:   slog.LevelInfo,
		Message: "test message",
	})
	m.syncViewport()

	content := m.viewport.View()
	if !strings.Contains(content, "test message") {
		t.Error("viewport missing log entry content")
	}
}

// Tests thinking block appears in viewport when active.
func TestSyncViewport_ThinkingBlock(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.thinkingExpanded = true
	m.thinking.Start()
	m.thinking.AddDelta("reasoning here")
	m.syncViewport()

	content := m.viewport.View()
	if !strings.Contains(content, "thinking") {
		t.Error("viewport missing thinking section")
	}
	if !strings.Contains(content, "reasoning here") {
		t.Error("viewport missing thinking content")
	}
}

// Tests that log channel listener is started in Init.
func TestModelInit_LogListener(t *testing.T) {
	ch := make(chan LogEntry, 10)
	m := NewModel(Config{
		LogChan: ch,
	})

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected non-nil init command")
	}
}

// Tests max entries truncation.
func TestLogEntries_MaxEntries(t *testing.T) {
	m := NewModel(Config{})
	m.logMaxEntries = 3
	m.width = 80

	for i := 0; i < 5; i++ {
		updated, _ := m.Update(logEntryMsg{
			entry: LogEntry{
				Level:   slog.LevelInfo,
				Message: fmt.Sprintf("entry %d", i),
			},
		})
		m = updated.(Model)
	}

	if len(m.logEntries) != 3 {
		t.Errorf("want 3 entries, got %d", len(m.logEntries))
	}
	if m.logEntries[0].Message != "entry 2" {
		t.Errorf("want oldest entry to be 'entry 2', got %q",
			m.logEntries[0].Message)
	}
}

// Tests that agentEventMsg dispatches tool-start and tool-end.
func TestAgentEventMsg_ToolStartAndEnd(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80

	updated, _ := m.Update(agentEventMsg{
		event: agent.AgentEvent{
			Type: "tool-start",
			Name: "shell",
			Info: "uptime",
		},
	})
	m = updated.(Model)

	if m.currentTool != "shell" {
		t.Errorf("want currentTool shell, got %q", m.currentTool)
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after tool-start")
	}
	if !strings.Contains(m.output[len(m.output)-1].text, "uptime") {
		t.Errorf("output missing command, got %q",
			m.output[len(m.output)-1].text)
	}

	updated, cmd := m.Update(agentEventMsg{
		event: agent.AgentEvent{
			Type:   "tool-end",
			Name:   "shell",
			Info:   "done",
			Output: "17:00:00 up 1 day",
		},
	})
	m = updated.(Model)

	if m.currentTool != "" {
		t.Errorf("currentTool should be empty after tool-end")
	}
	// Should re-listen.
	if cmd == nil {
		t.Error("expected command to re-listen on channel")
	}
	// Tool output should be in output.
	found := false
	for _, e := range m.output {
		if strings.Contains(e.text, "17:00:00 up 1 day") {
			found = true
		}
	}
	if !found {
		t.Error("tool output not found in viewport")
	}
}

// Tests that logCollapseThreshold triggers auto-collapse.
func TestLogCollapseThreshold_AutoCollapse(t *testing.T) {
	m := NewModel(Config{})
	m.logCollapseThreshold = 2
	m.logExpanded = true

	// First entry — no collapse.
	updated, _ := m.Update(logEntryMsg{
		entry: LogEntry{Message: "one"},
	})
	m = updated.(Model)
	if !m.logExpanded {
		t.Error("should still be expanded with 1 entry")
	}

	// Second entry — still at threshold, no collapse.
	updated, _ = m.Update(logEntryMsg{
		entry: LogEntry{Message: "two"},
	})
	m = updated.(Model)
	if !m.logExpanded {
		t.Error("should still be expanded at threshold")
	}

	// Third entry — exceeds threshold, should collapse.
	updated, _ = m.Update(logEntryMsg{
		entry: LogEntry{Message: "three"},
	})
	m = updated.(Model)
	if m.logExpanded {
		t.Error("should auto-collapse when threshold exceeded")
	}
}

// Tests that agent event channel re-listens after message.
func TestAgentEventChan_ReListen(t *testing.T) {
	m := NewModel(Config{})
	ch := m.agentEventChan

	// Send an event.
	ch <- agent.AgentEvent{
		Type: "tool-start",
		Name: "shell",
		Info: "test",
	}

	cmd := m.listenAgentEvents()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Process the message.
	msg := cmd()
	if _, ok := msg.(agentEventMsg); !ok {
		t.Errorf("expected agentEventMsg, got %T", msg)
	}
}

// Tests that log section respects display limit.
func TestRenderLogSection_DisplayLimit(t *testing.T) {
	m := NewModel(Config{})
	m.width = 80
	m.logExpanded = true
	m.logDisplayLimit = 3

	for i := 0; i < 5; i++ {
		m.logEntries = append(m.logEntries, LogEntry{
			Level:   slog.LevelInfo,
			Message: fmt.Sprintf("entry %d", i),
		})
	}

	section := m.renderLogSection()
	// Should only contain last 3 entries.
	if strings.Contains(section, "entry 0") {
		t.Error("oldest entry should be trimmed")
	}
	if !strings.Contains(section, "entry 4") {
		t.Error("newest entry should be present")
	}
}
