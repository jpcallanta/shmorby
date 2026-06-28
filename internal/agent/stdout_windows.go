//go:build windows

package agent

import (
	"errors"
)

func getTermSize(fd uintptr) (rows, cols int, err error) {
	return 0, 0, errors.New("not supported on windows")
}
