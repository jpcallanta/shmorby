package context

import (
	"context"
	"testing"

	"shmorby/internal/session"
)

func TestCompressor_Offload_Disabled(t *testing.T) {
	store := &mockStore{}
	c := NewCompressor(CompressorConfig{
		Enabled:         true,
		OffloadToMemory: false,
	}, store, &HeuristicEstimator{}, nil)

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
	}, store, &HeuristicEstimator{}, nil)

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
	}, nil, &HeuristicEstimator{}, nil)

	err := c.Offload(context.Background(),
		[]session.Message{{Role: "user", Content: "hi"}}, "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompressor_Compress_SessionShorter(t *testing.T) {
	sess := session.New()
	sess.AppendMessages([]session.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
		{Role: "user", Content: "e"},
		{Role: "assistant", Content: "f"},
	})

	c := NewCompressor(CompressorConfig{
		Enabled:               true,
		Mode:                  "aggressive",
		Threshold:             0.2,
		MinMessagesToCompress: 3,
		FallbackContextWindow: 100,
	}, nil, &fixedEstimator{perMsg: 30}, nil)

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
	}, nil, &HeuristicEstimator{}, nil)

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
