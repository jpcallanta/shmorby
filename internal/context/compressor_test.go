package context

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
)

// captureHandler collects slog records for tests.
type captureHandler struct {
	records *[]slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	*h.records = append(*h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

type mockStore struct {
	entries []memory.MemoryEntry
}

func (m *mockStore) Insert(entry memory.MemoryEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockStore) Get(id string) (memory.MemoryEntry, error) {
	return memory.MemoryEntry{}, nil
}
func (m *mockStore) Delete(id string) error { return nil }
func (m *mockStore) List(limit, offset int) ([]memory.MemoryEntry, error) {
	return m.entries, nil
}
func (m *mockStore) Count() (int, error)        { return len(m.entries), nil }
func (m *mockStore) Close() error               { return nil }
func (m *mockStore) AutoCaptureEnabled() bool   { return false }
func (m *mockStore) TagRules() []memory.TagRule { return nil }

func TestCompressor_ShouldCompress_UnderThreshold(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if got {
		t.Errorf("want false, got true")
	}
}

func TestCompressor_ShouldCompress_OverThreshold(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	for i := range msgs {
		msgs[i].Content = strings.Repeat("this is a long message content ", 10)
	}
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if !got {
		t.Errorf("want true, got false")
	}
}

func TestCompressor_ShouldCompress_ModeOff(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "off",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if got {
		t.Errorf("want false, got true")
	}
}

func TestCompressor_ShouldCompress_TooFewMessages(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 10,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if got {
		t.Errorf("want false, got true")
	}
}

func TestAdaptThreshold_LargeWindow(t *testing.T) {
	got := adaptThreshold(100000, 0.8)
	if got != 0.9 {
		t.Errorf("want 0.9, got %f", got)
	}
}

func TestAdaptThreshold_SmallWindow(t *testing.T) {
	got := adaptThreshold(8192, 0.8)
	if got != 0.6 {
		t.Errorf("want 0.6, got %f", got)
	}
}

func TestAdaptThreshold_Default(t *testing.T) {
	got := adaptThreshold(32000, 0.8)
	if got != 0.8 {
		t.Errorf("want 0.8, got %f", got)
	}
}

func TestCompressor_CompressToolOutput_Short(t *testing.T) {
	c := &Compressor{}
	input := "line1\nline2\n"
	got := c.compressToolOutput(input)
	if got != input {
		t.Errorf("want %q, got %q", input, got)
	}
}

func TestCompressor_CompressToolOutput_Long(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 20}}
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "line")
	}
	input := strings.Join(lines, "\n")
	got := c.compressToolOutput(input)

	parts := strings.Split(got, "\n")
	if len(parts) != 21 {
		t.Errorf("want 21 lines, got %d", len(parts))
	}
	if !strings.Contains(got, "(10 lines omitted)") {
		t.Errorf("want omitted marker, got %s", got)
	}
}

func TestCompressor_CompressToolOutput_Unlimited(t *testing.T) {
	c := &Compressor{}
	input := "line1\nline2\nline3\n"
	got := c.compressToolOutput(input)
	if got != input {
		t.Errorf("want %q, got %q", input, got)
	}

	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line")
	}
	input = strings.Join(lines, "\n")
	got = c.compressToolOutput(input)
	if got != input {
		t.Errorf("want full output, got truncated")
	}
}

func TestCompressor_CompressToolOutput_ExactlyAtLimit(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 20}}
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	input := strings.Join(lines, "\n")
	got := c.compressToolOutput(input)
	if got != input {
		t.Errorf("expected no truncation at exactly limit, got truncated")
	}
}

func TestCompressor_CompressToolOutput_OddLimit(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 9}}
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, "line")
	}
	input := strings.Join(lines, "\n")
	got := c.compressToolOutput(input)
	parts := strings.Split(got, "\n")
	// kept = 9/2 = 4 head + 4 tail + 1 omitted line = 9 lines
	if len(parts) != 9 {
		t.Errorf("want 9 lines (odd limit), got %d", len(parts))
	}
	if !strings.Contains(got, "(12 lines omitted)") {
		t.Errorf("want omitted marker, got %s", got)
	}
}

func TestCompressor_CompressToolOutput_SingleLine(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 1}}
	input := "only line"
	got := c.compressToolOutput(input)
	if got != input {
		t.Errorf("expected no truncation for single line")
	}
}

func TestCompressor_CompressToolOutput_TwoLinesOverLimit(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 2}}
	input := "line1\nline2\nline3"
	got := c.compressToolOutput(input)
	// keep = 2/2 = 1, so 1 head + 1 omitted + 1 tail = 3 lines
	parts := strings.Split(got, "\n")
	if len(parts) != 3 {
		t.Errorf("want 3 lines, got %d", len(parts))
	}
	if !strings.Contains(got, "(1 lines omitted)") {
		t.Errorf("want '(1 lines omitted)', got %s", got)
	}
}

func TestCompressor_CompressToolOutput_EmptyOutput(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 20}}
	got := c.compressToolOutput("")
	if got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestCompressor_CompressToolOutput_TrailingNewline(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 4}}
	// 5 lines with trailing newline = 6 elements when split
	input := "a\nb\nc\nd\ne\n"
	got := c.compressToolOutput(input)
	// keep = 4/2 = 2, so 2 head + omitted + 2 tail = 5 visible lines
	parts := strings.Split(got, "\n")
	if len(parts) != 5 {
		t.Errorf("want 5 lines, got %d", len(parts))
	}
	if !strings.Contains(got, "(2 lines omitted)") {
		t.Errorf("want '(2 lines omitted)', got %s", got)
	}
}

func TestCompressor_CompressToolOutput_PublicMethod(t *testing.T) {
	c := &Compressor{config: CompressorConfig{MaxToolOutputLines: 3}}
	input := "1\n2\n3\n4"
	got := c.CompressToolOutput(input)
	if got == input {
		t.Errorf("expected truncation through public method")
	}
}

func TestCompressor_SummarizeMessages_WithFunc(t *testing.T) {
	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"),
		func(ctx context.Context, text string) (string, error) {
			return "mock summary", nil
		})

	msgs := []session.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	got, err := c.summarizeMessages(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mock summary" {
		t.Errorf("want mock summary, got %s", got)
	}
}

func TestCompressor_SummarizeMessages_FallbackOnLLMError(t *testing.T) {
	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"),
		func(ctx context.Context, text string) (string, error) {
			return "", fmt.Errorf("provider unavailable")
		})

	msgs := []session.Message{
		{Role: "user", Content: "test message"},
	}
	got, err := c.summarizeMessages(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to extractive, not propagate the LLM error.
	if !strings.Contains(got, "test message") {
		t.Errorf("want extractive fallback containing message, got %s", got)
	}
}

func TestCompressor_SummarizeMessages_WarnOnceOnFallback(t *testing.T) {
	// A persistently failing summarizer must not spam the log: the
	// first fallback logs at WARN, repeat fallbacks at DEBUG.
	var records []slog.Record
	old := slog.Default()
	slog.SetDefault(slog.New(&captureHandler{records: &records}))
	defer slog.SetDefault(old)

	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"),
		func(ctx context.Context, text string) (string, error) {
			return "", fmt.Errorf("provider unavailable")
		})

	msgs := []session.Message{
		{Role: "user", Content: "test message"},
	}
	for i := 0; i < 3; i++ {
		if _, err := c.summarizeMessages(context.Background(), msgs); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	var warns, debugs int
	for _, r := range records {
		switch r.Level {
		case slog.LevelWarn:
			warns++
		case slog.LevelDebug:
			debugs++
		}
	}
	if warns != 1 {
		t.Errorf("want exactly 1 WARN log, got %d", warns)
	}
	if debugs != 2 {
		t.Errorf("want 2 DEBUG logs, got %d", debugs)
	}
}

func TestCompressor_SummarizeMessages_FallbackOnEmptyLLMResult(t *testing.T) {
	// Empty/whitespace-only generation (refusal, content filter) must
	// fall back to extractive instead of replacing the older half
	// with "[compressed] " (silent data loss).
	for _, empty := range []string{"", "   ", "\n\t\n"} {
		c := NewCompressor(CompressorConfig{Enabled: true}, nil,
			NewEstimator("gpt-4"),
			func(ctx context.Context, text string) (string, error) {
				return empty, nil
			})

		msgs := []session.Message{
			{Role: "user", Content: "test message"},
		}
		got, err := c.summarizeMessages(context.Background(), msgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "test message") {
			t.Errorf("want extractive fallback containing message, got %s", got)
		}
	}
}

func TestCompressor_SummarizeMessages_CapsOversizedInput(t *testing.T) {
	var gotText string
	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"),
		func(ctx context.Context, text string) (string, error) {
			gotText = text
			return "mock summary", nil
		})

	big := strings.Repeat("x", 150_000)
	msgs := []session.Message{
		{Role: "user", Content: big},
	}
	got, err := c.summarizeMessages(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "mock summary" {
		t.Fatalf("want mock summary, got %s", got)
	}
	if len(gotText) >= len(big) {
		t.Errorf("want capped prompt, got %d chars", len(gotText))
	}
	if !strings.Contains(gotText, big[:100]) {
		t.Error("want prompt head preserved")
	}
	if !strings.Contains(gotText, big[len(big)-100:]) {
		t.Error("want prompt tail preserved")
	}
}

func TestCapSummary_Input(t *testing.T) {
	short := strings.Repeat("a", 1000)
	if got := capSummaryInput(short); got != short {
		t.Errorf("want pass-through for short input, got %d chars", len(got))
	}

	long := strings.Repeat("a", 5000) + strings.Repeat("b", 150_000) +
		strings.Repeat("c", 5000)
	got := capSummaryInput(long)
	if len(got) >= len(long) {
		t.Error("want truncation for oversized input")
	}
	if !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "c") {
		t.Error("want head and tail preserved")
	}
	if !strings.Contains(got, "(60000 chars omitted)") {
		t.Errorf("want omission marker, got %s", got[:50])
	}
}

func TestCompressor_SummarizeMessages_NilFuncExtractive(t *testing.T) {
	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"), nil)

	msgs := []session.Message{
		{Role: "user", Content: "hello"},
	}
	got, err := c.summarizeMessages(context.Background(), msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("want content in summary, got %s", got)
	}
}

func TestTruncateToolOutputLines_AlwaysTruncates(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	input := strings.Join(lines, "\n")

	got := truncateToolOutputLines(input, 20)
	parts := strings.Split(got, "\n")
	if len(parts) != 21 {
		t.Errorf("want 21 lines (10 head + 1 omitted + 10 tail), got %d", len(parts))
	}
	if !strings.Contains(got, "(30 lines omitted)") {
		t.Errorf("want '(40 lines omitted)', got %s", got)
	}
	if !strings.HasPrefix(got, "line 0") {
		t.Errorf("want first line preserved, got %s", got)
	}
	if !strings.HasSuffix(got, "line 49") {
		t.Errorf("want last line preserved, got %s", got)
	}
}

func TestTruncateToolOutputLines_UnderLimit(t *testing.T) {
	input := "a\nb\nc"
	got := truncateToolOutputLines(input, 20)
	if got != input {
		t.Errorf("expected pass-through when under limit")
	}
}

func TestTruncateToolOutputLines_ZeroLimit(t *testing.T) {
	input := "a\nb\nc\nd\ne"
	got := truncateToolOutputLines(input, 0)
	if got != input {
		t.Errorf("expected pass-through when limit is 0")
	}
}

func TestTruncateToolOutputLines_NegativeLimit(t *testing.T) {
	input := "a\nb\nc\nd\ne"
	got := truncateToolOutputLines(input, -1)
	if got != input {
		t.Errorf("expected pass-through when limit is negative")
	}
}

func TestCompressor_Compress_AlwaysTruncatesAssistantMessages(t *testing.T) {
	// Session compression must truncate assistant messages even when
	// MaxToolOutputLines is 0 (unlimited per-turn).
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "long line content here")
	}
	bigOutput := strings.Join(lines, "\n")

	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		MaxToolOutputTokens:   1, // trigger compression on any content
		MaxToolOutputLines:    0, // per-turn unlimited
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	// Verify compressToolOutput passes through (per-turn behavior)
	// while truncateToolOutputLines truncates (session compression).
	perTurn := c.compressToolOutput(bigOutput)
	if perTurn != bigOutput {
		t.Error("compressToolOutput should pass through when MaxToolOutputLines=0")
	}

	sessionTrunc := truncateToolOutputLines(bigOutput, 20)
	sessionParts := strings.Split(sessionTrunc, "\n")
	if len(sessionParts) >= len(lines) {
		t.Error("session compression truncation should reduce output")
	}
	if !strings.Contains(sessionTrunc, "(80 lines omitted)") {
		t.Errorf("want '(80 lines omitted)', got %s", sessionTrunc)
	}
}

func TestSummarizeExtractive_Basic(t *testing.T) {
	msgs := []session.Message{
		{Role: "user", Content: "short"},
		{Role: "assistant", Content: "ok"},
	}
	got, err := summarizeExtractive(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "short") || !strings.Contains(got, "ok") {
		t.Errorf("want content preserved, got %s", got)
	}
}

func TestSummarizeExtractive_LongWithTail(t *testing.T) {
	long := strings.Repeat("a", 250) + " output: exit code 0 " +
		strings.Repeat("b", 100)
	msgs := []session.Message{
		{Role: "user", Content: long},
	}
	got, err := summarizeExtractive(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "exit code") {
		t.Errorf("want exit code preserved, got %s", got)
	}
}

func TestSummarizeExtractive_LongWithoutTail(t *testing.T) {
	long := strings.Repeat("a", 500)
	msgs := []session.Message{
		{Role: "user", Content: long},
	}
	got, err := summarizeExtractive(msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("want truncation marker, got %s", got)
	}
}

func TestSummarizeExtractive_Empty(t *testing.T) {
	got, err := summarizeExtractive(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("want empty, got %s", got)
	}
}

func TestHasImportant_Suffix(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"error: something failed", true},
		{"exit code 1", true},
		{"result: success", true},
		{"✓ nginx restarted", true},
		{"✗ deployment failed", true},
		{"everything is fine", false},
		{"just some regular text", false},
	}
	for _, tt := range tests {
		got := hasImportantSuffix(tt.input)
		if got != tt.want {
			t.Errorf("hasImportantSuffix(%q): want %v, got %v",
				tt.input, tt.want, got)
		}
	}
}

// --- Full request estimation tests ---

func TestShouldCompress_IncludesSystemAndTools(t *testing.T) {
	// Without SetRequestContext: message-only estimation.
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	// Without system/tools, should NOT compress (under threshold).
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if got {
		t.Error("want false without system/tools, got true")
	}

	// With a large system prompt and tools that exceeds the threshold,
	// should compress even though messages alone are under threshold.
	system := strings.Repeat(
		"this is a very long system prompt that takes up tokens ", 10)
	toolsJSON := []byte(`[{"name":"shell","description":"run shell"},` +
		`{"name":"ssh","description":"connect to remote hosts"}]`)
	c.SetRequestContext(system, toolsJSON)
	got = c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 100})
	if !got {
		t.Error("want true with system+tools, got false")
	}
}

func TestShouldCompress_BackwardCompat_NoSystemTools(t *testing.T) {
	// When SetRequestContext is never called, behavior is identical
	// to the old message-only estimation.
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 1000,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	got := c.ShouldCompress(msgs, llm.ModelInfo{ContextWindow: 1000})
	if got {
		t.Error("want false for small messages with large window, got true")
	}
}

func TestSetRequestContext_CachesToolTokens(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled: true, Mode: "conservative",
		Threshold: 0.8, MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	toolsJSON := []byte(`[{"name":"shell","description":"run shell commands"}]`)
	c.SetRequestContext("system prompt", toolsJSON)

	// Cached tool tokens should be non-zero.
	if c.cachedToolTokens == 0 {
		t.Error("want non-zero cached tool tokens")
	}
	if c.cachedSystemTokens == 0 {
		t.Error("want non-zero cached system tokens")
	}

	// A turn without tools (or a marshal failure) must reset the
	// tool cache so a stale estimate is never reused (F1).
	c.SetRequestContext("new system prompt", nil)
	if c.cachedToolTokens != 0 {
		t.Error("want tool cache reset when tools absent")
	}
	if c.cachedSystemTokens == 0 {
		t.Error("want system tokens re-estimated")
	}
}

// TestSetRequestContext_ConcurrentNoRace exercises SetRequestContext
// and ShouldCompress from many goroutines to verify the mutex guards
// the cached fields (data race fix).
func TestSetRequestContext_ConcurrentNoRace(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	modelInfo := llm.ModelInfo{ContextWindow: 100}

	// Warm up the estimator so the test measures the mutex, not
	// the one-time tiktoken BPE table construction.
	c.SetRequestContext("warmup", []byte(`[{"name":"w"}]`))
	_ = c.ShouldCompress(msgs, modelInfo)

	const goroutines = 5
	const iterations = 10

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			sys := strings.Repeat("x", 100+id)
			tools := []byte(fmt.Sprintf(
				`[{"name":"tool%d","description":"d"}]`, id))
			for i := 0; i < iterations; i++ {
				c.SetRequestContext(sys, tools)
				_ = c.ShouldCompress(msgs, modelInfo)
				// Interleave a no-tools call to exercise
				// the reset path concurrently.
				c.SetRequestContext(sys, nil)
				_ = c.ShouldCompress(msgs, modelInfo)
			}
		}(g)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestLogSummaryFallback_ConcurrentNoRace exercises logSummaryFallback
// from many goroutines to verify the mutex guards summaryFallbackLogged
// (data race fix).
func TestLogSummaryFallback_ConcurrentNoRace(t *testing.T) {
	c := NewCompressor(CompressorConfig{Enabled: true}, nil,
		NewEstimator("gpt-4"), nil)

	const goroutines = 10
	const iterations = 20

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < iterations; i++ {
				c.logSummaryFallback("test message")
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// After all goroutines complete, the flag must be set.
	if !c.summaryFallbackLogged {
		t.Error("want summaryFallbackLogged to be true after calls")
	}
}

// --- Shared compressor data races ---

// TestCompressor_SharedMutation_ConcurrentNoRace reproduces the issue
// scenario: subagent goroutines read config (ShouldCompress,
// Compress, counters) while another goroutine mutates it via
// SetMode/SetThreshold. Detectable with go test -race.
func TestCompressor_SharedMutation_ConcurrentNoRace(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "auto",
		Threshold:             0.8,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := make([]session.Message, 5)
	for i := range msgs {
		msgs[i].Content = strings.Repeat("payload ", 20)
	}
	modelInfo := llm.ModelInfo{ContextWindow: 100}

	// Warm the tiktoken cache so the test measures the mutex, not
	// one-time BPE table construction.
	_ = c.ShouldCompress(msgs, modelInfo)

	const goroutines = 8
	const iterations = 50

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			// Each goroutine owns its session, mirroring the
			// per-subagent child sessions.
			sess := session.New()
			sess.AppendMessages(msgs)
			for i := 0; i < iterations; i++ {
				switch (id + i) % 6 {
				case 0:
					c.SetMode("aggressive")
				case 1:
					c.SetThreshold(float64(i%9+1) / 10)
				case 2:
					_ = c.Config()
				case 3:
					_ = c.ShouldCompress(msgs, modelInfo)
					_ = c.CompressToolOutput(strings.Repeat("line\n", 40))
				case 4:
					_ = c.Compress(context.Background(), sess, modelInfo)
				case 5:
					_ = c.CompressForced(context.Background(), sess, modelInfo)
				}
			}
		}(g)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

// TestCompressor_CompressionCount_Concurrent_ExactCount runs
// one full compression per call from many goroutines and verifies the
// atomic counter sums every increment exactly (plain int++ lost
// updates under the old code).
func TestCompressor_CompressionCount_Concurrent_ExactCount(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "aggressive",
		Threshold:             0.1,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	const goroutines = 8
	const iterations = 10

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < iterations; i++ {
				// Fresh over-threshold session per call so
				// every Compress must increment the counter.
				sess := session.New()
				sess.AppendMessages([]session.Message{
					{Role: "user", Content: strings.Repeat("long message ", 20)},
					{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
					{Role: "user", Content: strings.Repeat("long message ", 20)},
					{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
					{Role: "user", Content: strings.Repeat("long message ", 20)},
					{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
				})
				err := c.Compress(context.Background(), sess,
					llm.ModelInfo{ContextWindow: 100})
				if err != nil {
					t.Errorf("Compress: %v", err)
				}
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	want := int64(goroutines * iterations)
	if got := c.CompressionCount.Load(); got != want {
		t.Errorf("want CompressionCount %d, got %d", want, got)
	}
}

// lockedStore wraps mockStore with a mutex so concurrent Offload calls
// measure the compressor's counter, not the mock's append.
type lockedStore struct {
	mockStore
	mu sync.Mutex
}

func (s *lockedStore) Insert(entry memory.MemoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.mockStore.Insert(entry)
}

// TestCompressor_OffloadCount_Concurrent_ExactCount verifies the
// atomic OffloadCount sums exactly across concurrent Offloads.
func TestCompressor_OffloadCount_Concurrent_ExactCount(t *testing.T) {
	store := &lockedStore{}
	c := NewCompressor(CompressorConfig{
		Enabled:         true,
		OffloadToMemory: true,
	}, store, NewEstimator("gpt-4"), nil)

	msgs := []session.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}

	const goroutines = 8
	const iterations = 20

	done := make(chan struct{}, goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < iterations; i++ {
				err := c.Offload(context.Background(), msgs, "s1")
				if err != nil {
					t.Errorf("Offload: %v", err)
				}
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	want := int64(goroutines * iterations * len(msgs))
	if got := c.OffloadCount.Load(); got != want {
		t.Errorf("want OffloadCount %d, got %d", want, got)
	}
}

// TestCompressForced_ModeOff_KeepsConfigUnchanged checks the emergency
// path forces one aggressive pass even with mode "off" and then leaves
// the shared config untouched (no SetMode mutation).
func TestCompressForced_ModeOff_KeepsConfigUnchanged(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "off",
		Threshold:             0.2,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	modelInfo := llm.ModelInfo{ContextWindow: 100}
	newSess := func() *session.Session {
		sess := session.New()
		sess.AppendMessages([]session.Message{
			{Role: "user", Content: strings.Repeat("long message ", 20)},
			{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
			{Role: "user", Content: strings.Repeat("long message ", 20)},
			{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
			{Role: "user", Content: strings.Repeat("long message ", 20)},
			{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
		})
		return sess
	}

	// Normal pass respects mode "off".
	sess := newSess()
	if err := c.Compress(context.Background(), sess, modelInfo); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.CompressionCount.Load(); got != 0 {
		t.Errorf("want CompressionCount 0 with mode off, got %d", got)
	}
	if len(sess.Messages()) != 6 {
		t.Errorf("want 6 messages unchanged, got %d", len(sess.Messages()))
	}

	// Forced pass compresses without mutating the configured mode.
	sess = newSess()
	if err := c.CompressForced(context.Background(), sess, modelInfo); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.CompressionCount.Load(); got != 1 {
		t.Errorf("want CompressionCount 1 after forced pass, got %d", got)
	}
	if len(sess.Messages()) >= 6 {
		t.Errorf("want session compressed, got %d messages",
			len(sess.Messages()))
	}
	if mode := c.Config().Mode; mode != "off" {
		t.Errorf("want config mode unchanged \"off\", got %q", mode)
	}
}

// TestCompressForced_ConcurrentSetMode_NeverAggressive
// verifies a forced emergency pass running alongside runtime mode
// changes never leaks "aggressive" into the shared config: neither
// may a concurrent reader observe it, nor may it remain set at the
// end. The old SetMode/restore dance in the emergency path violated
// both under -race.
func TestCompressForced_ConcurrentSetMode_NeverAggressive(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "conservative",
		Threshold:             0.1,
		MinMessagesToCompress: 2,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	msgs := []session.Message{
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long answer ", 20)},
	}
	modelInfo := llm.ModelInfo{ContextWindow: 100}

	// Warm the tiktoken cache so the test measures the mutex, not
	// one-time BPE table construction.
	_ = c.ShouldCompress(msgs, modelInfo)

	var sawAggressive atomic.Bool
	done := make(chan struct{}, 4)

	// Forced emergency passes, each on its own session (subagent
	// pattern).
	for g := 0; g < 2; g++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 10; i++ {
				sess := session.New()
				sess.AppendMessages(msgs)
				err := c.CompressForced(context.Background(), sess, modelInfo)
				if err != nil {
					t.Errorf("CompressForced: %v", err)
				}
			}
		}()
	}
	// Mode churn via legitimate runtime setters (never writes
	// "aggressive"; that is what the emergency path used to do).
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 100; i++ {
			if i%2 == 0 {
				c.SetMode("conservative")
			} else {
				c.SetMode("auto")
			}
		}
	}()
	// Concurrent reader sampling the shared mode during the churn.
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 500; i++ {
			if c.Config().Mode == "aggressive" {
				sawAggressive.Store(true)
			}
		}
	}()
	for i := 0; i < 4; i++ {
		<-done
	}

	if sawAggressive.Load() {
		t.Error("forced pass leaked \"aggressive\" into shared config")
	}
	if mode := c.Config().Mode; mode == "aggressive" {
		t.Errorf("want final mode non-aggressive, got %q", mode)
	}
}
