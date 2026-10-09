//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package term

import (
	"os"
	"syscall"
	"unsafe"
)

type termios = syscall.Termios

const (
	echo   = syscall.ECHO
	icanon = syscall.ICANON
	vmin   = syscall.VMIN
	vtime  = syscall.VTIME
)

// IsTerminal reports whether f is a terminal; nil is not.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	var t termios
	return ioctl(f, ioctlGet, &t) == nil
}

// setTermios applies change to a terminal's settings and returns a function
// restoring the originals. Non-terminals are left alone.
func setTermios(f *os.File, change func(*termios)) (func(), error) {
	noop := func() {}
	if !IsTerminal(f) {
		return noop, nil
	}
	var original termios
	if err := ioctl(f, ioctlGet, &original); err != nil {
		return noop, err
	}
	modified := original
	change(&modified)
	if err := ioctl(f, ioctlSet, &modified); err != nil {
		return noop, err
	}
	return func() { _ = ioctl(f, ioctlSet, &original) }, nil
}

func ioctl(f *os.File, req uintptr, t *termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
