package context

import (
	"strings"

	"github.com/pkoukk/tiktoken-go"
	"shmorby/internal/session"
)

// TiktokenEstimator uses tiktoken for precise per-model token counts.
type TiktokenEstimator struct {
	encodingName string
}

// NewEstimator creates a tiktoken-based estimator for the given model.
// Falls back to cl100k_base (GPT-4/GPT-3.5 encoding) if the model is
// unknown — this is a reasonable default for all OpenAI-compatible models
// and is still tokenizer-based, not a heuristic.
func NewEstimator(model string) *TiktokenEstimator {
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
	switch {
	case strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "o"):
		return "o200k_base"
	case strings.HasPrefix(model, "gpt-4") || strings.HasPrefix(model, "gpt-3.5"):
		return "cl100k_base"
	case strings.Contains(model, "deepseek") || strings.Contains(model, "llama"):
		return "o200k_base"
	default:
		return "cl100k_base"
	}
}
