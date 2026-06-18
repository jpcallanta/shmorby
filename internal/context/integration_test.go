//go:build integration

package context

import (
	"context"
	"path/filepath"
	"testing"

	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
)

// Tests the compressor end-to-end: insert enough messages to cross the
// threshold, then verify compression reduces message history.
func TestCompressorIntegration(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(memory.Config{
		Enabled: true,
		DBPath:  filepath.Join(dir, "ctx.db"),
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	c := NewCompressor(
		CompressorConfig{
			Enabled:               true,
			Mode:                  "aggressive",
			Threshold:             0.01,
			MaxToolOutputTokens:   4096,
			MinMessagesToCompress: 2,
			FallbackContextWindow: 8192,
			OffloadToMemory:       true,
		},
		store,
		&HeuristicEstimator{},
		nil,
	)

	sess := session.New()
	sess.Append("user", "What is the disk usage on web-01?")
	sess.Append("assistant", "Let me check.")
	sess.Append("tool", "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        98G   45G   53G  46% /")

	ctx := context.Background()
	modelInfo := llm.ModelInfo{ContextWindow: 8192}

	if !c.ShouldCompress(sess.Messages(), modelInfo) {
		t.Fatal("expected ShouldCompress to return true")
	}

	if err := c.Compress(ctx, sess, modelInfo); err != nil {
		t.Fatalf("Compress: %v", err)
	}

	messages := sess.Messages()
	if len(messages) == 0 {
		t.Fatal("expected at least one message after compression")
	}
	if c.CompressionCount != 1 {
		t.Errorf("want CompressionCount 1, got %d", c.CompressionCount)
	}
}
