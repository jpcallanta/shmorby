package context

import (
	"context"
	"strings"
	"testing"

	"shmorby/internal/session"
)

func TestCompressor_Offload_Disabled(t *testing.T) {
	store := &mockStore{}
	c := NewCompressor(CompressorConfig{
		Enabled:         true,
		OffloadToMemory: false,
	}, store, NewEstimator("gpt-4"), nil)

	err := c.Offload(context.Background(),
		[]session.Message{{Role: "user", Content: "hi"}}, "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.entries) != 0 {
		t.Errorf("want 0 entries, got %d", len(store.entries))
	}
}

func TestCompressor_Offload_Enabled(t *testing.T) {
	store := &mockStore{}
	c := NewCompressor(CompressorConfig{
		Enabled:         true,
		OffloadToMemory: true,
	}, store, NewEstimator("gpt-4"), nil)

	err := c.Offload(context.Background(),
		[]session.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		}, "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.entries) != 2 {
		t.Errorf("want 2 entries, got %d", len(store.entries))
	}
}

func TestCompressor_Offload_NilStore(t *testing.T) {
	c := NewCompressor(CompressorConfig{
		Enabled:         true,
		OffloadToMemory: true,
	}, nil, NewEstimator("gpt-4"), nil)

	err := c.Offload(context.Background(),
		[]session.Message{{Role: "user", Content: "hi"}}, "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompressor_Compress_SessionShorter(t *testing.T) {
	sess := session.New()
	sess.AppendMessages([]session.Message{
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long response ", 20)},
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long response ", 20)},
		{Role: "user", Content: strings.Repeat("long message ", 20)},
		{Role: "assistant", Content: strings.Repeat("long response ", 20)},
	})

	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "aggressive",
		Threshold:             0.2,
		MinMessagesToCompress: 3,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	err := c.Compress(context.Background(), sess, struct {
		ContextWindow   int
		MaxOutputTokens int
		SupportsTools   bool
	}{ContextWindow: 100, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := sess.Messages()
	if len(msgs) >= 6 {
		t.Errorf("want session shorter than original 6, got %d", len(msgs))
	}
}

func TestCompressor_Compress_UnderThreshold(t *testing.T) {
	sess := session.New()
	sess.Append("user", "hello")

	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "aggressive",
		Threshold:             0.8,
		MinMessagesToCompress: 10,
		FallbackContextWindow: 100,
	}, nil, NewEstimator("gpt-4"), nil)

	err := c.Compress(context.Background(), sess, struct {
		ContextWindow   int
		MaxOutputTokens int
		SupportsTools   bool
	}{ContextWindow: 100, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Session should be unchanged since ShouldCompress returned false.
	msgs := sess.Messages()
	if len(msgs) != 1 {
		t.Errorf("want 1 message unchanged, got %d", len(msgs))
	}
}

func TestOffloadSummary_Short(t *testing.T) {
	input := "short content"
	got := offloadSummary(input)
	if got != input {
		t.Errorf("want %q, got %q", input, got)
	}
}

func TestOffloadSummary_Long(t *testing.T) {
	input := strings.Repeat("a", 300) + strings.Repeat("b", 300)
	got := offloadSummary(input)
	if !strings.Contains(got, "... (100 chars omitted) ...") {
		t.Errorf("want omission marker, got %s", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 250)) {
		t.Errorf("want head preserved, got %s", got)
	}
	if !strings.HasSuffix(got, strings.Repeat("b", 250)) {
		t.Errorf("want tail preserved, got %s", got)
	}
}

func TestOffloadSummary_ExactBoundary(t *testing.T) {
	input := strings.Repeat("a", 501)
	got := offloadSummary(input)
	if !strings.Contains(got, "(1 chars omitted)") {
		t.Errorf("expected head+tail with 1 omitted char, got %s", got)
	}
}
