//go:build windows

package agent

import "syscall"

const sigTSTP = syscall.Signal(0)
const sigCONT = syscall.Signal(0)
