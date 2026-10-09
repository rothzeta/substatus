package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// enableRawInput puts a terminal into single-character mode and returns a
// restoration function. Non-terminal inputs remain line/buffer based.
func enableRawInput(f *os.File) (func(), error) {
	noop := func() {}
	if f == nil {
		return noop, nil
	}
	info, err := f.Stat()
	if err != nil {
		return noop, err
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		return noop, nil
	}
	var original syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&original)))
	if errno != 0 {
		return noop, errno
	}
	raw := original
	raw.Lflag &^= syscall.ECHO | syscall.ICANON
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&raw)))
	if errno != 0 {
		return noop, errno
	}
	return func() {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&original)))
	}, nil
}
