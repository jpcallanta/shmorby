package context

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
)

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

func TestHasImportantSuffix(t *testing.T) {
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
