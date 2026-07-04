package context

import (
	"testing"
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
