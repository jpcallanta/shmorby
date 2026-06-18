package context

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"shmorby/internal/memory"
	"shmorby/internal/session"
)

func (c *Compressor) Offload(
	ctx context.Context, messages []session.Message, sessionID string,
) error {
	if !c.config.OffloadToMemory || c.store == nil {
		return nil
	}

	for _, msg := range messages {
		id, err := newUUID()
		if err != nil {
			return fmt.Errorf("generate id: %w", err)
		}

		c.OffloadCount++

		entry := memory.MemoryEntry{
			ID:        id,
			Timestamp: time.Now(),
			SessionID: sessionID,
			Tool:      "offload",
			Summary:   fmt.Sprintf("[%s] %s", msg.Role, truncate(msg.Content, 500)),
			Tags:      []string{"offloaded", string(msg.Role)},
		}

		if err := c.store.Insert(entry); err != nil {
			return fmt.Errorf("offload insert: %w", err)
		}
	}

	return nil
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:],
	), nil
}
