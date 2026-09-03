//go:build !windows

package agent

import "syscall"

const sigTSTP = syscall.SIGTSTP
const sigCONT = syscall.SIGCONT
