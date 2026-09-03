// Package xuuid provides a shared UUID v4 generator using crypto/rand.
// Consolidates identical bit-manipulation logic previously duplicated
// across internal/session, internal/context, and internal/memory.
package xuuid

import (
	"crypto/rand"
	"fmt"
)

// New generates a random UUID v4 string in standard 8-4-4-4-12 hex format.
// Uses crypto/rand for cryptographically secure randomness.
func New() (string, error) {
	b := make([]byte, 16)

	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	// Set version nibble (4) in byte 6 and variant bits (10xx) in byte 8.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			b[0:4], b[4:6], b[6:8], b[8:10], b[10:]),
		nil
}
