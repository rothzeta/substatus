//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package term

import "os"

// termios is a stand-in on platforms without termios support; changes to it
// have no effect, so input stays line-buffered and visible.
type termios struct {
	Lflag uint32
	Cc    [32]uint8
}

const (
	echo   = 0
	icanon = 0
	vmin   = 0
	vtime  = 1
)

// IsTerminal reports whether f is a character device; nil is not.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func setTermios(*os.File, func(*termios)) (func(), error) { return func() {}, nil }
