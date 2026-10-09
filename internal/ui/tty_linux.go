package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// enableRawInput puts a terminal into single-character mode without echo.
func enableRawInput(f *os.File) (func(), error) {
	return setTermios(f, func(t *syscall.Termios) {
		t.Lflag &^= syscall.ECHO | syscall.ICANON
		t.Cc[syscall.VMIN] = 1
		t.Cc[syscall.VTIME] = 0
	})
}

// disableEcho keeps line editing but hides typed characters.
func disableEcho(f *os.File) (func(), error) {
	return setTermios(f, func(t *syscall.Termios) { t.Lflag &^= syscall.ECHO })
}

// setTermios applies change to a terminal's settings and returns a function
// restoring the originals. Non-terminal files are left alone.
func setTermios(f *os.File, change func(*syscall.Termios)) (func(), error) {
	noop := func() {}
	if !IsTerminal(f) {
		return noop, nil
	}
	var original syscall.Termios
	if err := ioctlTermios(f, syscall.TCGETS, &original); err != nil {
		return noop, err
	}
	modified := original
	change(&modified)
	if err := ioctlTermios(f, syscall.TCSETS, &modified); err != nil {
		return noop, err
	}
	return func() { _ = ioctlTermios(f, syscall.TCSETS, &original) }, nil
}

func ioctlTermios(f *os.File, req uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
