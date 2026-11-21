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
	"shmorby/internal/util"
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

// Reads image bytes from the system clipboard. Returns (data, true)
// when an image is present, (nil, false) otherwise. Data is
// PNG-encoded on all supported platforms; anything that does not
// sniff as PNG or JPEG is treated as absent.
func PasteImage() ([]byte, bool) {
	if initErr != nil {
		return nil, false
	}
	data := cb.Read(cb.FmtImage)
	if mime, _ := util.SniffImageType(data); mime == "" {
		return nil, false
	}

	return data, true
}

// Formats OSC-52 sequence with optional tmux DCS passthrough envelope.
func osc52Sequence(text string, isTmux bool) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))

	// Wrap in DCS passthrough when running inside tmux.
	if isTmux {
		return fmt.Sprintf("\033Ptmux;\033\033]52;c;%s\007\033\\", encoded)
	}

	return fmt.Sprintf("\033]52;c;%s\007", encoded)
}

// Sends an OSC-52 escape sequence to stdout for terminal clipboard access.
func osc52Copy(text string) error {
	seq := osc52Sequence(text, os.Getenv("TMUX") != "")

	_, err := os.Stdout.WriteString(seq)
	if err != nil {
		return fmt.Errorf("write OSC-52: %w", err)
	}

	return nil
}
