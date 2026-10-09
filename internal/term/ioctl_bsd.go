//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package term

import "syscall"

const (
	ioctlGet = syscall.TIOCGETA
	ioctlSet = syscall.TIOCSETA
)
