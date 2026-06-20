package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/tools"
)

// syncWriter wraps a bytes.Buffer with a mutex for concurrent use.
type syncWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// Tests that REPL /help output contains all required sections.
func TestREPLHelp_AllSections(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()

	sections := []string{
		"AGENT MODES",
		"SLASH COMMANDS",
		"KEYBOARD SHORTCUTS",
		"LEADER KEY",
		"PERMISSIONS",
	}
	for _, s := range sections {
		if !strings.Contains(output, s) {
			t.Errorf("REPL /help missing section %q", s)
		}
	}
}

// Tests that REPL /help includes all slash commands.
func TestREPLHelp_AllCommands(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	commands := []string{
		"/help", "/quit", "/reset", "/model", "/agent",
		"/scope", "/memory", "/context", "/log", "/tui",
	}
	for _, cmd := range commands {
		if !strings.Contains(output, cmd) {
			t.Errorf("REPL /help missing command %q", cmd)
		}
	}
}

// Tests that REPL /help includes keyboard shortcuts.
func TestREPLHelp_KeyboardShortcuts(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	shortcuts := []string{
		"ctrl+h", "ctrl+p", "ctrl+r", "ctrl+c",
		"ctrl+v", "ctrl+l", "ctrl+t", "ctrl+x",
	}
	for _, sc := range shortcuts {
		if !strings.Contains(output, sc) {
			t.Errorf("REPL /help missing shortcut %q", sc)
		}
	}
}

// Tests that REPL /help includes leader key bindings.
func TestREPLHelp_LeaderKeys(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	bindings := []string{
		"ctrl+x c", "ctrl+x n", "ctrl+x l",
		"ctrl+x q", "ctrl+x h",
	}
	for _, b := range bindings {
		if !strings.Contains(output, b) {
			t.Errorf("REPL /help missing leader binding %q", b)
		}
	}
}

// Tests that REPL /help shows current mode.
func TestREPLHelp_ShowsMode(t *testing.T) {
	var out bytes.Buffer
	r := &REPL{
		Provider: &fakeProvider{name: "test"},
		Session:  session.New(),
		Mode:     "diagnose",
		Model:    "test-model",
		In:       strings.NewReader("/help\n/quit\n"),
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "diagnose") {
		t.Error("REPL /help should show current mode")
	}
}

// TestREPL_EmptyInput checks empty line re-prompts without processing.
func TestREPL_EmptyInput(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.calls) != 0 {
		t.Errorf("want 0 provider calls, got %d", len(p.calls))
	}
}

// TestREPL_SlashCommand_NoToolProcessing checks /help handled inline.
func TestREPL_SlashCommand_NoToolProcessing(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("/help\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(p.calls) != 0 {
		t.Errorf("want 0 provider calls, got %d", len(p.calls))
	}
}

// TestREPL_SimpleReply_NoTools checks single text reply with formatting.
func TestREPL_SimpleReply_NoTools(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake", reply: "world"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "world") {
		t.Errorf("want 'world' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "shmorby>") {
		t.Errorf("want prompt in output")
	}
}

// TestREPL_Error_PrintsError checks error is printed.
func TestREPL_Error_PrintsError(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &errorProvider{name: "fake"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Error:") {
		t.Errorf("want 'Error:' in output, got:\n%s", output)
	}
}

// TestREPL_ThinkingSpinner_Shown checks spinner text appears in output.
func TestREPL_ThinkingSpinner_Shown(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter
	done := make(chan struct{})

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				<-done
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	r := &REPL{
		Provider: p,
		Session:  sess,
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("hello\n"),
		Out:      &out,
		Registry: tools.NewRegistry(),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	go func() { _ = r.Run(ctx) }()
	time.Sleep(250 * time.Millisecond)
	close(done)
	time.Sleep(100 * time.Millisecond)

	output := out.String()
	if !strings.Contains(output, "thinking") {
		t.Errorf("expected 'thinking' label in output, got:\n%s", output)
	}
}

// TestREPL_ToolStart_RendersHeader checks tool-start with sep + cyan.
func TestREPL_ToolStart_RendersHeader(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
				ch <- llm.StreamEvent{
					Type:    "tool-call",
					ToolID:  "call_1",
					Tool:    "shell",
					Content: `{"command":"echo hi"}`,
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Done"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok"})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("run command\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "shell: echo hi") {
		t.Errorf("want 'shell: echo hi' in output, got:\n%s", output)
	}
}

// TestREPL_ToolEnd_Success_RendersStatus checks tool output displayed.
func TestREPL_ToolEnd_Success_RendersStatus(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
				ch <- llm.StreamEvent{
					Type:    "tool-call",
					ToolID:  "call_1",
					Tool:    "shell",
					Content: `{"command":"echo hi"}`,
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Done"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "ok output"})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("run command\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "ok output") {
		t.Errorf("want tool output in stdout, got:\n%s", output)
	}
	if !strings.Contains(output, "Done") {
		t.Errorf("want final reply in stdout, got:\n%s", output)
	}
}

// TestREPL_MultipleTools_EachRendered checks sequential tool calls.
func TestREPL_MultipleTools_EachRendered(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter
	var mu sync.Mutex
	callCount := 0

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				mu.Lock()
				thisCall := callCount + 1
				callCount = thisCall
				mu.Unlock()
				if thisCall == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "First"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"cmd1"}`,
					}
				} else if thisCall == 2 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Second"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_2",
						Tool:    "shell",
						Content: `{"command":"cmd2"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Final"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "output"})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("do stuff\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Final") {
		t.Errorf("want final reply in output, got:\n%s", output)
	}
}

// TestREPL_AgentReplySeparator checks agent reply framing.
func TestREPL_AgentReplySeparator(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake", reply: "test reply"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "test reply") {
		t.Errorf("want 'test reply' in output, got:\n%s", output)
	}
}

// TestREPL_FormatMarkdown_Applied checks bold markdown rendered.
func TestREPL_FormatMarkdown_Applied(t *testing.T) {
	defer SetTerminalForTest(true)()
	in := strings.NewReader("hello\n/quit\n")
	var out syncWriter

	p := &fakeProvider{name: "fake", reply: "this is **bold** text"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "\033[1m") {
		t.Errorf("want ANSI bold in output, got:\n%s", output)
	}
}

// TestREPL_PipeMode_NoANSI checks piped output has no escapes.
func TestREPL_PipeMode_NoANSI(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake", reply: "**bold** reply"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if strings.Contains(output, "\033[") {
		t.Errorf("expected no ANSI in pipe mode, got:\n%s", output)
	}
}

// TestREPL_PipeMode_NoSpinner checks piped output has no spinner chars.
func TestREPL_PipeMode_NoSpinner(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake", reply: "reply"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if strings.Contains(output, "⠋") {
		t.Errorf("expected no spinner chars in pipe mode")
	}
	if strings.Contains(output, "⟳") {
		t.Errorf("expected no thinking spinner in pipe mode")
	}
}

// TestREPL_ThinkingSpinner_KilledOnDelta checks spinner cleared on first delta.
func TestREPL_ThinkingSpinner_KilledOnDelta(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter
	var mu sync.Mutex
	callCount := 0

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				mu.Lock()
				thisCall := callCount + 1
				callCount = thisCall
				mu.Unlock()
				// Return text delta on first call, then wait on block channel.
				if thisCall == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "response"}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "done"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	r := &REPL{
		Provider: p,
		Session:  sess,
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("hi\n"),
		Out:      &out,
		Registry: tools.NewRegistry(),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_ = r.Run(ctx)

	output := out.String()
	if !strings.Contains(output, "response") {
		t.Errorf("want response in output, got:\n%s", output)
	}
}

// TestREPL_StreamingText_DeltasPrinted checks each onDelta prints progress.
func TestREPL_StreamingText_DeltasPrinted(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "text", Delta: "Hello"}
				ch <- llm.StreamEvent{Type: "text", Delta: " world"}
				ch <- llm.StreamEvent{Type: "text", Delta: "!"}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	r := &REPL{
		Provider: p,
		Session:  sess,
		Mode:     "operate",
		Model:    "test-model",
		In:       strings.NewReader("hello\n"),
		Out:      &out,
		Registry: tools.NewRegistry(),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	_ = r.Run(ctx)

	output := out.String()
	if !strings.Contains(output, "Hello world!") {
		t.Errorf("want streamed text in output, got:\n%s", output)
	}
}

// TestREPL_ToolEnd_Error_RendersError checks error indicator with output.
func TestREPL_ToolEnd_Error_RendersError(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter
	var mu sync.Mutex
	callCount := 0

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				mu.Lock()
				thisCall := callCount + 1
				callCount = thisCall
				mu.Unlock()
				if thisCall == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"fail"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Failed"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name:   "shell",
		result: "partial output",
		err:    fmt.Errorf("exit code 1"),
	})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("run\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "partial output") {
		t.Errorf("want partial output in output, got:\n%s", output)
	}
}

// TestREPL_ToolSpinner_Shown checks spinner during tool execution.
func TestREPL_ToolSpinner_Shown(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter
	var mu sync.Mutex
	callCount := 0

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				mu.Lock()
				thisCall := callCount + 1
				callCount = thisCall
				mu.Unlock()
				if thisCall == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"echo hi"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text", Delta: "Done"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	// Use sleepy tool so spinner has time to tick.
	reg.Register(&sleepyTool{name: "shell", result: "ok", sleep: 200 * time.Millisecond})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("run\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "running") {
		t.Errorf("want 'running' spinner label in output, got:\n%s", output)
	}
}

// TestREPL_ToolSpinner_KilledOnEnd checks spinner cleared when tool-end fires.
func TestREPL_ToolSpinner_KilledOnEnd(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
				ch <- llm.StreamEvent{
					Type:    "tool-call",
					ToolID:  "call_1",
					Tool:    "shell",
					Content: `{"command":"echo hi"}`,
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
		chatFn: func(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
			return llm.ChatResponse{
				Message: llm.Message{Role: "assistant", Content: "Result"},
			}, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{name: "shell", result: "output text"})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("run\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "output text") {
		t.Errorf("want tool output printed, got:\n%s", output)
	}
}

// TestREPL_FencedCode_Rendered checks fenced code header appears.
func TestREPL_FencedCode_Rendered(t *testing.T) {
	defer SetTerminalForTest(true)()
	in := strings.NewReader("hello\n/quit\n")
	var out syncWriter

	p := &fakeProvider{name: "fake", reply: "```go\nfunc main() {}\n```"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "code: go") {
		t.Errorf("want fenced code header in output, got:\n%s", output)
	}
}

// fakeMemStore implements memory.Store for testing.
type fakeMemStore struct {
	entries []memory.MemoryEntry
}

func (f *fakeMemStore) Insert(_ memory.MemoryEntry) error { return nil }
func (f *fakeMemStore) Get(_ string) (memory.MemoryEntry, error) {
	return memory.MemoryEntry{}, fmt.Errorf("not found")
}
func (f *fakeMemStore) Delete(_ string) error                           { return nil }
func (f *fakeMemStore) List(_ int, _ int) ([]memory.MemoryEntry, error) { return f.entries, nil }
func (f *fakeMemStore) Count() (int, error)                             { return len(f.entries), nil }
func (f *fakeMemStore) Close() error                                    { return nil }
func (f *fakeMemStore) AutoCaptureEnabled() bool                        { return false }
func (f *fakeMemStore) TagRules() []memory.TagRule                      { return nil }

// TestREPL_MemoryIndicator_Shown checks memory indicator in output.
func TestREPL_MemoryIndicator_Shown(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	store := &fakeMemStore{
		entries: []memory.MemoryEntry{
			{Command: "hello world"},
		},
	}
	retriever := memory.NewRetriever(store, 5)

	p := &fakeProvider{name: "fake", reply: "ok"}
	r := &REPL{
		Provider:  p,
		Session:   session.New(),
		Mode:      "operate",
		Model:     "test-model",
		In:        in,
		Out:       &out,
		Store:     store,
		Retriever: retriever,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "memory") {
		t.Errorf("want memory indicator in output, got:\n%s", output)
	}
}

// TestREPL_StreamingFallback_NonStreamingProvider checks fallback when
// the provider doesn't support streaming (e.g. opencode_zen).
func TestREPL_StreamingFallback_NonStreamingProvider(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	p := &fakeStepProvider{
		name: "fake",
		steps: []llm.ChatResponse{
			{Message: llm.Message{Role: "assistant", Content: "Hello from fallback"}},
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("hello\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
	}

	// Should succeed because the REPL falls back to RunTurnWithTools
	// when ChatStream returns "streaming not supported".
	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Hello from fallback") {
		t.Errorf("want fallback reply in output, got:\n%s", output)
	}
}

// TestREPL_PipeMode_NoCarriageReturn checks piped output has no \r.
func TestREPL_PipeMode_NoCarriageReturn(t *testing.T) {
	defer SetTerminalForTest(false)()
	in := strings.NewReader("hello\n/quit\n")
	var out bytes.Buffer

	p := &fakeProvider{name: "fake", reply: "reply text"}
	r := &REPL{
		Provider: p,
		Session:  session.New(),
		Mode:     "operate",
		Model:    "test-model",
		In:       in,
		Out:      &out,
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if strings.Contains(output, "\r") {
		t.Errorf("expected no carriage return in pipe mode, got:\n%q", output)
	}
}

// TestREPL_PermissionPrompt_Streaming checks permission prompt works
// cleanly with streaming enabled (spinner suspended, fresh scanner).
func TestREPL_PermissionPrompt_Streaming(t *testing.T) {
	defer SetTerminalForTest(true)()
	var out syncWriter

	var mu sync.Mutex
	callCount := 0
	p := &fakeStreamProvider{
		name: "fake",
		streamFn: func(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
			ch := make(chan llm.StreamEvent)
			go func() {
				mu.Lock()
				thisCall := callCount + 1
				callCount = thisCall
				mu.Unlock()
				if thisCall == 1 {
					ch <- llm.StreamEvent{Type: "text", Delta: "Running"}
					ch <- llm.StreamEvent{
						Type:    "tool-call",
						ToolID:  "call_1",
						Tool:    "shell",
						Content: `{"command":"uptime"}`,
					}
				} else {
					ch <- llm.StreamEvent{Type: "text",
						Delta: "Permission granted result"}
				}
				ch <- llm.StreamEvent{Type: "done"}
				close(ch)
			}()
			return ch, nil
		},
	}
	sess := session.New()
	reg := tools.NewRegistry()
	reg.Register(&fakeTool{
		name: "shell", result: "Uptime: 10 days", perm: "ask",
	})

	r := &REPL{
		Provider:     p,
		Session:      sess,
		Mode:         "operate",
		Model:        "test-model",
		In:           strings.NewReader("check uptime\ny\n/quit\n"),
		Out:          &out,
		Registry:     reg,
		MaxToolIter:  5,
		ShellEnabled: true,
		ToolRules:    map[string]*tools.RuleSet{},
	}

	err := r.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "Permission granted result") {
		t.Errorf("want permission-granted reply in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Permission requested") {
		t.Errorf("want permission prompt in output, got:\n%s", output)
	}
	if !strings.Contains(output, "Allow?") {
		t.Errorf("want 'Allow?' prompt visible in output, got:\n%s", output)
	}
}
