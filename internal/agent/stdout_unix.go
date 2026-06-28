//go:build !windows

package agent

import (
	"syscall"
	"unsafe"
)

func getTermSize(fd uintptr) (rows, cols int, err error) {
	var ws struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL, fd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.Row), int(ws.Col), nil
}
