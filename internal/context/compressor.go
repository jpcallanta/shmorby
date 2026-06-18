package context

import (
	"context"
	"fmt"
	"strings"

	"shmorby/internal/llm"
	"shmorby/internal/memory"
	"shmorby/internal/session"
)

type CompressorConfig struct {
	Enabled               bool
	Mode                  string  // auto, aggressive, conservative, off
	Threshold             float64 // default 0.8
	MaxToolOutputTokens   int     // default 4096
	MaxToolOutputLines    int     // 0 = unlimited
	SummaryModel          string
	SummaryProvider       string
	OffloadToMemory       bool
	MinMessagesToCompress int // default 6
	FallbackContextWindow int // default 8192
}

type Compressor struct {
	config           CompressorConfig
	store            memory.Store
	estimator        Estimator
	summaryFunc      func(ctx context.Context, text string) (string, error)
	CompressionCount int
	OffloadCount     int
}

func NewCompressor(
	config CompressorConfig,
	store memory.Store,
	estimator Estimator,
	summaryFunc func(ctx context.Context, text string) (string, error),
) *Compressor {
	if config.Threshold == 0 {
		config.Threshold = 0.8
	}
	if config.MaxToolOutputTokens == 0 {
		config.MaxToolOutputTokens = 4096
	}
	if config.MinMessagesToCompress == 0 {
		config.MinMessagesToCompress = 6
	}
	if config.FallbackContextWindow == 0 {
		config.FallbackContextWindow = 8192
	}
	if estimator == nil {
		estimator = &HeuristicEstimator{}
	}

	return &Compressor{
		config:      config,
		store:       store,
		estimator:   estimator,
		summaryFunc: summaryFunc,
	}
}

// Config returns a copy of the compressor configuration.
func (c *Compressor) Config() CompressorConfig {
	return c.config
}

// EstimateMessages returns the estimated token count for a set of messages.
func (c *Compressor) EstimateMessages(messages []session.Message) int {
	return c.estimator.EstimateMessages(messages)
}

func (c *Compressor) ShouldCompress(sessionMessages []session.Message, modelInfo llm.ModelInfo) bool {
	if c.config.Mode == "off" || !c.config.Enabled {
		return false
	}
	if len(sessionMessages) < c.config.MinMessagesToCompress {
		return false
	}

	limit := modelInfo.ContextWindow
	if limit == 0 {
		limit = c.config.FallbackContextWindow
	}

	threshold := c.config.Threshold
	if c.config.Mode == "auto" {
		threshold = adaptThreshold(modelInfo.ContextWindow, threshold)
	}

	tokens := c.estimator.EstimateMessages(sessionMessages)

	return float64(tokens) > float64(limit)*threshold
}

func adaptThreshold(contextWindow int, base float64) float64 {
	if contextWindow >= 100000 {
		return 0.9
	}
	if contextWindow <= 8192 {
		return 0.6
	}

	return base
}

func (c *Compressor) CompressToolOutput(output string) string {
	return c.compressToolOutput(output)
}

// truncateToolOutputLines always truncates output at limit lines
// (independent of any config). Used by session compression.
func truncateToolOutputLines(output string, limit int) string {
	if limit <= 0 {
		return output
	}

	lines := strings.Split(output, "\n")
	if len(lines) <= limit {
		return output
	}

	keep := limit / 2
	var result []string
	result = append(result, lines[:keep]...)
	result = append(result, fmt.Sprintf(
		"... (%d lines omitted) ...", len(lines)-keep*2))
	result = append(result, lines[len(lines)-keep:]...)

	return strings.Join(result, "\n")
}

func (c *Compressor) compressToolOutput(output string) string {
	return truncateToolOutputLines(output, c.config.MaxToolOutputLines)
}

func (c *Compressor) summarizeMessages(
	ctx context.Context, messages []session.Message,
) (string, error) {
	if c.summaryFunc == nil {
		return summarizeByTruncation(messages)
	}

	var buf strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&buf, "[%s] %s\n", m.Role, m.Content)
	}

	prompt := fmt.Sprintf(
		"Summarize this conversation segment, keeping key decisions "+
			"and results:\n\n%s", buf.String())

	return c.summaryFunc(ctx, prompt)
}

func summarizeByTruncation(messages []session.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("[compressed] ")

	for i, m := range messages {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(fmt.Sprintf("%s: %s", m.Role, truncate(m.Content, 200)))
	}

	return b.String(), nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

func (c *Compressor) Compress(
	ctx context.Context, sess *session.Session, modelInfo llm.ModelInfo,
) error {
	messages := sess.Messages()

	if !c.ShouldCompress(messages, modelInfo) {
		return nil
	}

	c.CompressionCount++

	// Offload to memory
	if err := c.Offload(ctx, messages, sess.ID()); err != nil {
		return fmt.Errorf("offload: %w", err)
	}

	// Compress tool outputs in recent messages (always uses a
	// hardcoded line limit, independent of MaxToolOutputLines, so
	// session compression is predictable even when per-turn output
	// is configured as unlimited).
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.Content) > c.config.MaxToolOutputTokens*4 {
			messages[i].Content = truncateToolOutputLines(msg.Content, 20)
		}
	}

	// Summarize older messages
	split := len(messages) / 2
	older := messages[:split]
	recent := messages[split:]

	summary, err := c.summarizeMessages(ctx, older)
	if err != nil {
		return fmt.Errorf("summarize: %w", err)
	}

	compressed := session.Message{
		Role:    "assistant",
		Content: fmt.Sprintf("[compressed] %s", summary),
	}
	sess.SetMessages(append([]session.Message{compressed}, recent...))

	return nil
}
