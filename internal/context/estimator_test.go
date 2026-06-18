package context

import (
	"testing"

	"shmorby/internal/session"
)

func TestHeuristicEstimator_Estimate_ShortString(t *testing.T) {
	e := &HeuristicEstimator{}
	got := e.Estimate("hello")
	if got != 2 {
		t.Errorf("want 2, got %d", got)
	}
}

func TestHeuristicEstimator_Estimate_EmptyString(t *testing.T) {
	e := &HeuristicEstimator{}
	got := e.Estimate("")
	if got != 0 {
		t.Errorf("want 0, got %d", got)
	}
}

func TestHeuristicEstimator_EstimateMessages_Sum(t *testing.T) {
	e := &HeuristicEstimator{}
	msgs := []session.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	got := e.EstimateMessages(msgs)
	if got != 4 {
		t.Errorf("want 4, got %d", got)
	}
}

func TestNewTiktokenEstimator_ValidModel(t *testing.T) {
	te := NewTiktokenEstimator("gpt-4")
	if te.encodingName != "cl100k_base" {
		t.Errorf("want cl100k_base, got %s", te.encodingName)
	}
}

func TestNewTiktokenEstimator_GPT4o(t *testing.T) {
	te := NewTiktokenEstimator("gpt-4o")
	if te.encodingName != "o200k_base" {
		t.Errorf("want o200k_base, got %s", te.encodingName)
	}
}

func TestTiktokenEstimator_Estimate_FallbackOnError(t *testing.T) {
	te := &TiktokenEstimator{encodingName: "nonexistent"}
	got := te.Estimate("hello world")
	if got == 0 {
		t.Fatal("want non-zero fallback, got 0")
	}
}

func TestTiktokenEstimator_Estimate_WithGPT4(t *testing.T) {
	te := NewTiktokenEstimator("gpt-4")
	got := te.Estimate("hello world")
	if got <= 0 {
		t.Errorf("want positive token count, got %d", got)
	}
}
