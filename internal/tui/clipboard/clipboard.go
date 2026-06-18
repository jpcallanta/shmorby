// Package clipboard provides system clipboard access for the TUI.
// Falls back to OSC-52 escape sequence when system clipboard is unavailable.
package clipboard

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"sync"

	cb "golang.design/x/clipboard"
)

var (
	once    sync.Once
	initErr error
)

// Init initializes the system clipboard. Must be called before Copy or Paste.
// Returns nil on success. Subsequent calls are no-ops.
func Init() error {
	once.Do(func() {
		initErr = cb.Init()
	})
	return initErr
}

// Copy writes text to the system clipboard. Falls back to OSC-52 when
// the system clipboard is unavailable.
func Copy(text string) {
	if initErr == nil {
		_ = cb.Write(cb.FmtText, []byte(text))
		return
	}
	// OSC-52 fallback: send escape sequence to terminal.
	if err := osc52Copy(text); err != nil {
		log.Printf("clipboard: copy failed: %v", err)
	}
}

// Paste reads text from the system clipboard. Returns empty string if
// clipboard is not initialized.
func Paste() string {
	if initErr != nil {
		return ""
	}
	bytes := cb.Read(cb.FmtText)
	return string(bytes)
}

// osc52Copy sends an OSC-52 escape sequence to stdout for terminal-level
// clipboard access. Supported by kitty, iTerm2, tmux, and other modern
// terminals.
func osc52Copy(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	// OSC-52 sequence: ESC ] 52 ; c ; <base64> BEL
	seq := fmt.Sprintf("\033]52;c;%s\007", encoded)
	_, err := os.Stdout.WriteString(seq)
	return err
}
