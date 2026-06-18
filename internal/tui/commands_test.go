package tui

import (
	"path/filepath"
	"strings"
	"testing"

	ctxcomp "shmorby/internal/context"
	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
)

// newModelWithMemory creates a TUI model backed by a temp memory store
// and seeded with sample entries.
func newModelWithMemory(t *testing.T) (*Model, memory.Store) {
	t.Helper()

	dir := t.TempDir()
	cfg := memory.Config{
		Enabled:     true,
		DBPath:      filepath.Join(dir, "memory.db"),
		MaxEntries:  100,
		AutoCapture: true,
	}

	store, err := memory.NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Seed some entries.
	entries := []memory.MemoryEntry{
		{
			ID: "mem1", SessionID: "s1",
			Tool: "shell", Command: "systemctl restart nginx",
			Result: "ok", ExitCode: 0,
		},
		{
			ID: "mem2", SessionID: "s1",
			Tool: "shell", Command: "apt update",
			Result: "done", ExitCode: 0,
		},
		{
			ID: "mem3", SessionID: "s1",
			Tool: "ssh", Command: "ssh admin@web01",
			Result: "connected", ExitCode: 0,
		},
	}

	for _, e := range entries {
		if err := store.Insert(e); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	m := NewModel(Config{})
	m.memoryStore = store
	m.retriever = memory.NewRetriever(store, 10)

	return &m, store
}

// TestMemoryCommand_NoArgs shows recent entries.
func TestMemoryCommand_NoArgs(t *testing.T) {
	m, _ := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /memory")
	}
	if !strings.Contains(m.output[0].text, "systemctl restart nginx") {
		t.Error("output should contain seeded entry")
	}
}

// TestMemoryCommand_Search returns matching entries.
func TestMemoryCommand_Search(t *testing.T) {
	m, _ := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory search nginx")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /memory search")
	}
	if !strings.Contains(m.output[0].text, "nginx") {
		t.Errorf(
			"output should contain search term 'nginx',"+
				" got %q", m.output[0].text,
		)
	}
}

// TestMemoryCommand_Forget deletes an entry by ID.
func TestMemoryCommand_Forget(t *testing.T) {
	m, _ := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory forget mem1")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /memory forget")
	}
	if !strings.Contains(m.output[0].text, "Deleted") {
		t.Errorf("output should confirm deletion, got %q", m.output[0].text)
	}
}

// TestMemoryCommand_Clear_shows_confirmation then clears on allow.
func TestMemoryCommand_Clear(t *testing.T) {
	m, store := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory clear")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.permission == nil {
		t.Fatal("expected permission prompt after /memory clear")
	}
	if !m.pendingClearMemory {
		t.Error("expected pendingClearMemory flag")
	}

	// Simulate user allowing the clear.
	_, _ = m.Update(
		permissionResultMsg{choice: PermissionAllow},
	)

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries after clear, got %d", count)
	}
}

// TestMemoryCommand_ClearCancelled does not delete on deny.
func TestMemoryCommand_ClearCancelled(t *testing.T) {
	m, store := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory clear")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.permission == nil {
		t.Fatal("expected permission prompt")
	}

	// Simulate user denying.
	_, _ = m.Update(
		permissionResultMsg{choice: PermissionDeny},
	)

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 entries after deny, got %d", count)
	}
}

// TestMemoryCommand_Clear_empty_store shows no clear prompt.
func TestMemoryCommand_ClearEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(memory.Config{
		Enabled: true,
		DBPath:  filepath.Join(dir, "empty.db"),
	}, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m := NewModel(Config{})
	m.memoryStore = store

	cmd, done, err := m.handleCommand("/memory clear")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if m.permission != nil {
		t.Error("no prompt expected for empty store")
	}
	if m.pendingClearMemory {
		t.Error("no pending action expected for empty store")
	}
}

// TestMemoryCommand_Stats shows entry count.
func TestMemoryCommand_Stats(t *testing.T) {
	m, _ := newModelWithMemory(t)

	cmd, done, err := m.handleCommand("/memory stats")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /memory stats")
	}
	if !strings.Contains(m.output[0].text, "Entries:") {
		t.Errorf("output should show entry count, got %q", m.output[0].text)
	}
}

// TestMemoryCommand_NoStore returns error for unavailable memory.
func TestMemoryCommand_NoStore(t *testing.T) {
	m := NewModel(Config{})
	m.memoryStore = nil

	cmd, done, err := m.handleCommand("/memory")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if !strings.Contains(m.output[0].text, "not available") {
		t.Errorf(
			"output should indicate memory unavailable,"+
				" got %q", m.output[0].text,
		)
	}
}

// TestMemoryCommand_UnknownSubcommand returns error.
func TestMemoryCommand_UnknownSubcommand(t *testing.T) {
	m := NewModel(Config{})

	cmd, done, err := m.handleCommand("/memory foobar")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if m.output[0].kind != "error" {
		t.Errorf("expected error kind, got %q", m.output[0].kind)
	}
}

// TestMemoryCommand_SearchNoArgs returns error.
func TestMemoryCommand_SearchNoArgs(t *testing.T) {
	m := NewModel(Config{})

	cmd, done, err := m.handleCommand("/memory search")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if m.output[0].kind != "error" {
		t.Errorf("expected error kind, got %q", m.output[0].kind)
	}
}

// TestMemoryCommand_ForgetNoArgs returns error.
func TestMemoryCommand_ForgetNoArgs(t *testing.T) {
	m := NewModel(Config{})

	cmd, done, err := m.handleCommand("/memory forget")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if m.output[0].kind != "error" {
		t.Errorf("expected error kind, got %q", m.output[0].kind)
	}
}

// newModelWithContext creates a TUI model backed by a session and compressor.
func newModelWithContext(t *testing.T) *Model {
	t.Helper()

	sess := session.New()
	sess.Append("user", "Hello")
	sess.Append("assistant", "Hi there, how can I help?")

	dir := t.TempDir()
	store, err := memory.NewStore(memory.Config{
		Enabled: true,
		DBPath:  filepath.Join(dir, "ctx.db"),
	}, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m := NewModel(Config{})
	m.session = sess
	m.memoryStore = store
	m.model = "llama3.2"
	m.modelInfo = llm.ModelInfo{
		ContextWindow:   128000,
		MaxOutputTokens: 4096,
		SupportsTools:   true,
	}
	m.compressor = ctxcomp.NewCompressor(
		ctxcomp.CompressorConfig{
			Enabled:               true,
			Mode:                  "auto",
			Threshold:             0.8,
			MaxToolOutputTokens:   4096,
			MinMessagesToCompress: 6,
			FallbackContextWindow: 8192,
			OffloadToMemory:       true,
		},
		store,
		&ctxcomp.HeuristicEstimator{},
		nil,
	)

	return &m
}

// TestContextCommand_NoArgs shows token usage and compression stats.
func TestContextCommand_NoArgs(t *testing.T) {
	m := newModelWithContext(t)

	cmd, done, err := m.handleCommand("/context")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /context")
	}
	if !strings.Contains(m.output[0].text, "Context status") {
		t.Errorf("output should contain 'Context status', got %q", m.output[0].text)
	}
	if !strings.Contains(m.output[0].text, "llama3.2") {
		t.Errorf("output should contain model name, got %q", m.output[0].text)
	}
	if !strings.Contains(m.output[0].text, "128k") {
		t.Errorf("output should contain context window size, got %q", m.output[0].text)
	}
}

// TestContextCommand_Compress manually triggers compression.
func TestContextCommand_Compress(t *testing.T) {
	m := newModelWithContext(t)

	cmd, done, err := m.handleCommand("/context compress")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /context compress")
	}
	if !strings.Contains(m.output[0].text, "Context compressed") {
		t.Errorf("output should confirm compression, got %q", m.output[0].text)
	}
	if m.ctxStats == nil {
		t.Fatal("expected ctxStats after compression")
	}
}

// TestContextCommand_Stats shows offloaded messages and storage.
func TestContextCommand_Stats(t *testing.T) {
	m := newModelWithContext(t)

	cmd, done, err := m.handleCommand("/context stats")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /context stats")
	}
	if !strings.Contains(m.output[0].text, "Memory offloading") {
		t.Errorf("output should contain 'Memory offloading', got %q", m.output[0].text)
	}
	if !strings.Contains(m.output[0].text, "offloaded messages") {
		t.Errorf("output should mention offloaded messages, got %q", m.output[0].text)
	}
}

// TestContextCommand_Model shows model info.
func TestContextCommand_Model(t *testing.T) {
	m := newModelWithContext(t)

	cmd, done, err := m.handleCommand("/context model")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output after /context model")
	}
	if !strings.Contains(m.output[0].text, "Model info") {
		t.Errorf("output should contain 'Model info', got %q", m.output[0].text)
	}
	if !strings.Contains(m.output[0].text, "llama3.2") {
		t.Errorf("output should contain model name, got %q", m.output[0].text)
	}
}

// TestContextCommand_UnknownSubcommand returns error.
func TestContextCommand_UnknownSubcommand(t *testing.T) {
	m := newModelWithContext(t)

	cmd, done, err := m.handleCommand("/context foobar")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if m.output[0].kind != "error" {
		t.Errorf("expected error kind, got %q", m.output[0].kind)
	}
}

// TestContextCommand_NoCompressor returns error.
func TestContextCommand_NoCompressor(t *testing.T) {
	m := NewModel(Config{})

	cmd, done, err := m.handleCommand("/context")
	if err != nil {
		t.Fatalf("handleCommand: want nil, got %v", err)
	}
	if !cmd {
		t.Error("expected command handled")
	}
	if done {
		t.Error("should not quit")
	}
	if len(m.output) == 0 {
		t.Fatal("expected output")
	}
	if !strings.Contains(m.output[0].text, "not configured") {
		t.Errorf("output should indicate not configured, got %q", m.output[0].text)
	}
}

// TestStatusBar_CtxStats shows compression info in status bar.
func TestStatusBar_CtxStats(t *testing.T) {
	m := newModelWithContext(t)
	m.width = 80

	m.updateCtxStats()

	view := m.View()
	if !strings.Contains(view, "ctx:") {
		t.Error("status bar should show ctx: with stats")
	}
}

// TestFormatTokens formats token counts correctly.
func TestFormatTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{500, "500"},
		{1000, "1k"},
		{42000, "42k"},
		{128000, "128k"},
	}
	for _, tc := range tests {
		got := formatTokens(tc.input)
		if got != tc.want {
			t.Errorf("formatTokens(%d): want %q, got %q", tc.input, tc.want, got)
		}
	}
}
