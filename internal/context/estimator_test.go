package context

import (
	"testing"

	"shmorby/internal/llm"
	"shmorby/internal/session"
)

func TestNewEstimator_Default(t *testing.T) {
	te := NewEstimator("gpt-4o")
	if te.encodingName != "o200k_base" {
		t.Errorf("want o200k_base, got %s", te.encodingName)
	}
}

func TestNewEstimator_Fallback(t *testing.T) {
	te := NewEstimator("unknown-model-v1")
	if te.encodingName != "cl100k_base" {
		t.Errorf("want cl100k_base, got %s", te.encodingName)
	}
}

func TestTiktokenEstimator_Estimate(t *testing.T) {
	te := NewEstimator("gpt-4")
	got := te.Estimate("hello world")
	if got <= 0 {
		t.Errorf("want positive token count, got %d", got)
	}
}

// Tests that image-bearing messages are counted above their text
// cost so a turn with images cannot silently overflow the budget.
func TestEstimateMessages_Images(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	refs := session.SaveImages("est-sess", []llm.ImageData{
		{MediaType: "image/png", Data: png},
	})
	if len(refs) != 1 {
		t.Fatalf("want 1 ref, got %d", len(refs))
	}

	te := NewEstimator("gpt-4")
	textOnly := te.EstimateMessages([]session.Message{
		{Role: "user", Content: "look at this"},
	})
	withImage := te.EstimateMessages([]session.Message{
		{Role: "user", Content: "look at this", Images: refs},
	})
	if withImage <= textOnly {
		t.Errorf("want image cost above text-only (%d), got %d",
			textOnly, withImage)
	}
}
