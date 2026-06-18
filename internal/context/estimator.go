package context

import (
	"shmorby/internal/session"
	"github.com/pkoukk/tiktoken-go"
)

// Estimator estimates token counts for text and messages.
type Estimator interface {
	Estimate(text string) int
	EstimateMessages(messages []session.Message) int
}

// ~4 chars per token
type HeuristicEstimator struct{}

func (h *HeuristicEstimator) Estimate(text string) int {
	return (len(text) + 3) / 4
}

func (h *HeuristicEstimator) EstimateMessages(messages []session.Message) int {
	total := 0

	for _, m := range messages {
		total += h.Estimate(m.Content)
	}

	return total
}

// TiktokenEstimator uses tiktoken for precise per-model token counts.
type TiktokenEstimator struct {
	encodingName string
}

func NewTiktokenEstimator(model string) *TiktokenEstimator {
	return &TiktokenEstimator{
		encodingName: resolveEncoding(model),
	}
}

func (t *TiktokenEstimator) Estimate(text string) int {
	enc, err := tiktoken.GetEncoding(t.encodingName)
	if err != nil {
		return (len(text) + 3) / 4
	}

	tokens := enc.Encode(text, nil, nil)

	return len(tokens)
}

func (t *TiktokenEstimator) EstimateMessages(messages []session.Message) int {
	total := 0

	for _, m := range messages {
		total += t.Estimate(m.Content)
	}

	return total
}

func resolveEncoding(model string) string {
	switch model {
	case "gpt-4", "gpt-4-32k", "gpt-3.5-turbo",
		"gpt-3.5-turbo-16k", "gpt-4-turbo", "gpt-4-1106-preview":
		return "cl100k_base"
	case "gpt-4o", "gpt-4o-mini", "gpt-4o-2024-05-13":
		return "o200k_base"
	default:
		return "cl100k_base"
	}
}
