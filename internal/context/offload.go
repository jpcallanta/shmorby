package context

import (
	"context"
	"fmt"
	"time"

	"shmorby/internal/memory"
	"shmorby/internal/session"
	"shmorby/internal/xuuid"
)

func (c *Compressor) Offload(
	ctx context.Context, messages []session.Message, sessionID string,
) error {
	return c.offload(ctx, c.Config(), messages, sessionID)
}

// Runs the offload against a caller-supplied config snapshot so a
// compression pass observes one consistent view even if /set flips
// the offload flag mid-flight.
func (c *Compressor) offload(
	ctx context.Context, cfg CompressorConfig,
	messages []session.Message, sessionID string,
) error {
	if !cfg.OffloadToMemory || c.store == nil {
		return nil
	}

	for _, msg := range messages {
		id, err := xuuid.New()
		if err != nil {
			return fmt.Errorf("generate id: %w", err)
		}

		c.OffloadCount.Add(1)

		entry := memory.MemoryEntry{
			ID:        id,
			Timestamp: time.Now(),
			SessionID: sessionID,
			Tool:      "offload",
			Summary:   fmt.Sprintf("[%s] %s", msg.Role, offloadSummary(msg.Content)),
			Tags:      []string{"offloaded", string(msg.Role)},
		}

		if err := c.store.Insert(entry); err != nil {
			return fmt.Errorf("offload insert: %w", err)
		}
	}

	return nil
}

// Returns a compact summary preserving head (250 chars) and tail
// (250 chars) of long content. Returns as-is when ≤ 500 chars.
func offloadSummary(content string) string {
	const maxLen = 500
	if len(content) <= maxLen {
		return content
	}

	head := 250
	tail := 250
	if head+tail >= len(content) {
		head = len(content) / 2
		tail = len(content) - head
	}

	omitted := len(content) - head - tail

	return content[:head] + fmt.Sprintf(
		"... (%d chars omitted) ...", omitted,
	) + content[len(content)-tail:]
}
