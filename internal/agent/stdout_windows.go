//go:build windows

package agent

import (
	"golang.org/x/term"
)

// Returns terminal size on Windows via x/term.
func getTermSize(fd uintptr) (rows, cols int, err error) {
	w, h, err := term.GetSize(int(fd))
	if err != nil {
		return 0, 0, err
	}

	return h, w, nil
}
