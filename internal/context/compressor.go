package context

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
	OffloadToMemory       bool
	MinMessagesToCompress int // default 6
	FallbackContextWindow int // default 8192
}

type Compressor struct {
	// The same Compressor is shared across concurrent subagents
	// (task tool) and the main REPL thread. config, the cached
	// estimates and summaryFallbackLogged are guarded by mu; the
	// counters are atomic so increments on the hot path never need
	// the decision lock.
	CompressionCount atomic.Int64
	OffloadCount     atomic.Int64

	config                CompressorConfig
	store                 memory.Store
	estimator             *TiktokenEstimator
	summaryFunc           func(ctx context.Context, text string) (string, error)
	summaryFallbackLogged bool

	mu sync.Mutex

	// Cached token estimates for the current turn's system prompt and
	// tool definitions. Set via SetRequestContext before Compress to
	// include these in the ShouldCompress estimate (F1: full request
	// estimation).
	cachedSystemTokens int
	cachedToolTokens   int
}

func NewCompressor(
	config CompressorConfig,
	store memory.Store,
	estimator *TiktokenEstimator,
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
		estimator = NewEstimator("gpt-4")
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
	c.mu.Lock()
	cfg := c.config
	c.mu.Unlock()

	return cfg
}

// SetMode updates the compression mode at runtime.
func (c *Compressor) SetMode(mode string) {
	c.mu.Lock()
	c.config.Mode = mode
	c.mu.Unlock()
}

// SetThreshold updates the compression threshold at runtime.
func (c *Compressor) SetThreshold(t float64) {
	c.mu.Lock()
	c.config.Threshold = t
	c.mu.Unlock()
}

// Returns a consistent snapshot of the config plus the cached
// request-context estimates under one lock, so a caller never mixes
// values from different mutation points.
func (c *Compressor) snapshot() (CompressorConfig, int, int) {
	c.mu.Lock()
	cfg := c.config
	sysTokens := c.cachedSystemTokens
	toolTokens := c.cachedToolTokens
	c.mu.Unlock()

	return cfg, sysTokens, toolTokens
}

// SetRequestContext caches token estimates for the system prompt and
// tool definitions so ShouldCompress can include them in the total
// request estimate. Call before Compress in each turn. The tools JSON
// is byte-identical per turn, so the estimate is cached and reused.
// The tool cache is reset unconditionally so a turn without tools
// (or a marshal failure) never reuses a stale estimate from a
// previous turn.
func (c *Compressor) SetRequestContext(
	systemPrompt string, toolDefsJSON []byte,
) {
	sysTokens := c.estimator.Estimate(systemPrompt)
	toolTokens := 0
	if len(toolDefsJSON) > 0 {
		toolTokens = c.estimator.Estimate(string(toolDefsJSON))
	}

	c.mu.Lock()
	c.cachedSystemTokens = sysTokens
	// Reset unconditionally so a turn without tools (or a marshal
	// failure) never reuses a stale estimate from a previous turn.
	c.cachedToolTokens = toolTokens
	c.mu.Unlock()
}

// EstimateMessages returns the estimated token count for a set of messages.
func (c *Compressor) EstimateMessages(messages []session.Message) int {
	return c.estimator.EstimateMessages(messages)
}

func (c *Compressor) ShouldCompress(
	sessionMessages []session.Message, modelInfo llm.ModelInfo,
) bool {
	cfg, sysTokens, toolTokens := c.snapshot()

	return c.shouldCompress(cfg, sysTokens, toolTokens, sessionMessages,
		modelInfo)
}

// Decides from a caller-supplied config snapshot and cached estimates
// (see snapshot) so a compression decision uses one consistent view
// and Compress can apply a per-call mode override without mutating
// shared config.
func (c *Compressor) shouldCompress(
	cfg CompressorConfig, sysTokens, toolTokens int,
	sessionMessages []session.Message, modelInfo llm.ModelInfo,
) bool {
	if cfg.Mode == "off" || !cfg.Enabled {
		return false
	}
	if len(sessionMessages) < cfg.MinMessagesToCompress {
		return false
	}

	limit := modelInfo.ContextWindow
	if limit == 0 {
		limit = cfg.FallbackContextWindow
	}

	threshold := cfg.Threshold
	if cfg.Mode == "auto" {
		threshold = adaptThreshold(modelInfo.ContextWindow, threshold)
	}

	// Estimate full request: system + tools + session messages.
	// System and tool estimates are set via SetRequestContext; when
	// absent (backward compat) the total equals message-only.
	tokens := c.estimator.EstimateMessages(sessionMessages) +
		sysTokens + toolTokens

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
	return truncateToolOutputLines(output, c.Config().MaxToolOutputLines)
}

func (c *Compressor) summarizeMessages(
	ctx context.Context, messages []session.Message,
) (string, error) {
	if c.summaryFunc != nil {
		var buf strings.Builder
		for _, m := range messages {
			fmt.Fprintf(&buf, "[%s] %s\n", m.Role, m.Content)
		}

		prompt := fmt.Sprintf(
			"Summarize this conversation segment, keeping key decisions "+
				"and results:\n\n%s", capSummaryInput(buf.String()))

		summary, err := c.summaryFunc(ctx, prompt)
		if err != nil {
			c.logSummaryFallback(
				"LLM summarizer failed, falling back to extractive",
				"err", err)
		} else if strings.TrimSpace(summary) == "" {
			// Empty generation (refusal, content filter) would
			// silently replace the older half with "[compressed] ".
			c.logSummaryFallback(
				"LLM summarizer returned an empty summary, " +
					"falling back to extractive")
		} else {
			return summary, nil
		}
	}

	return summarizeExtractive(messages)
}

// logSummaryFallback logs the first summarizer fallback at WARN
// level and downgrades repeat occurrences to DEBUG so a persistently
// failing provider does not spam the log on every compression.
// Guarded by c.mu since the Compressor is shared across concurrent
// subagents (task tool).
func (c *Compressor) logSummaryFallback(msg string, args ...any) {
	c.mu.Lock()
	first := !c.summaryFallbackLogged
	c.summaryFallbackLogged = true
	c.mu.Unlock()

	if first {
		slog.Warn(msg, args...)
		return
	}
	slog.Debug(msg, args...)
}

// capSummaryInput bounds the LLM summarizer prompt to head/tail slices
// so a small summary model is not flooded with an oversized older half.
// Preserves both ends, like summarizeExtractive.
func capSummaryInput(s string) string {
	const capChars = 100_000 // head 50k + tail 50k
	if len(s) <= capChars {
		return s
	}
	half := capChars / 2
	return s[:half] + fmt.Sprintf("\n... (%d chars omitted) ...\n",
		len(s)-capChars) + s[len(s)-half:]
}

// Collapses messages into [compressed] format: keeps first 300 chars
// per message, preserving the tail when it contains important signal
// (exit codes, errors, status markers).
func summarizeExtractive(messages []session.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("[compressed] ")

	for i, m := range messages {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		content := m.Content
		if len(content) > 300 {
			head := content[:300]
			tail := content[len(content)-100:]
			if hasImportantSuffix(tail) {
				b.WriteString(head)
				b.WriteString("... (")
				b.WriteString(strconv.Itoa(len(content) - 300))
				b.WriteString(" chars) ...")
				b.WriteString(tail)
			} else {
				b.WriteString(head)
				b.WriteString("...")
			}
		} else {
			b.WriteString(content)
		}
	}

	return b.String(), nil
}

// Returns true if the tail of a message contains patterns worth
// preserving beyond the head-truncation boundary (errors, exit codes,
// status markers).
func hasImportantSuffix(s string) bool {
	lower := strings.ToLower(s)
	patterns := []string{"error:", "exit code", "✓", "✗", "result:",
		"success", "failed", "warning:", "status:"}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}

	return false
}

func (c *Compressor) Compress(
	ctx context.Context, sess *session.Session, modelInfo llm.ModelInfo,
) error {
	return c.compress(ctx, sess, modelInfo, "")
}

// CompressForced runs one emergency compression pass with the mode
// forced to "aggressive" for that call only, leaving the shared
// configuration (and concurrent subagents reading it) untouched.
func (c *Compressor) CompressForced(
	ctx context.Context, sess *session.Session, modelInfo llm.ModelInfo,
) error {
	return c.compress(ctx, sess, modelInfo, "aggressive")
}

// Runs a compression pass with the config mode overridden for this
// call only (empty override = use configured mode). Used by
// emergency context-window recovery so a forced pass never mutates
// shared config that concurrent subagents are reading.
func (c *Compressor) compress(
	ctx context.Context, sess *session.Session, modelInfo llm.ModelInfo,
	modeOverride string,
) error {
	cfg, sysTokens, toolTokens := c.snapshot()
	if modeOverride != "" {
		cfg.Mode = modeOverride
	}
	messages := sess.Messages()

	if !c.shouldCompress(cfg, sysTokens, toolTokens, messages, modelInfo) {
		return nil
	}

	c.CompressionCount.Add(1)

	// Offload using the same config snapshot the decision was made
	// from (no second lock acquisition).
	if err := c.offload(ctx, cfg, messages, sess.ID()); err != nil {
		return fmt.Errorf("offload: %w", err)
	}

	// Compress tool outputs in recent messages (always uses a
	// hardcoded line limit, independent of MaxToolOutputLines, so
	// session compression is predictable even when per-turn output
	// is configured as unlimited).
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.Content) > cfg.MaxToolOutputTokens*4 {
			messages[i].Content = truncateToolOutputLines(msg.Content, 20)
		}
	}
	sess.SetMessages(messages)

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
