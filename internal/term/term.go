// Package term provides the few terminal controls substatus needs: detecting
// a terminal, single-key input, and reading a secret without echo.
package term

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
)

// ReadSecret reads one line from f, hiding the input when f is a terminal.
// Echo is restored before it returns, including when ctx ends first (for
// example on a signal).
func ReadSecret(ctx context.Context, f *os.File) (string, error) {
	restore, err := setTermios(f, func(t *termios) { t.Lflag &^= echo })
	if err != nil {
		return "", err
	}
	defer restore()
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(f).ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil && !errors.Is(r.err, io.EOF) {
			return "", r.err
		}
		return strings.TrimSpace(r.line), nil
	}
}

// MakeRaw switches a terminal to single-character input without echo and
// returns a function restoring the previous settings. Non-terminals are left
// alone.
func MakeRaw(f *os.File) (restore func(), err error) {
	return setTermios(f, func(t *termios) {
		t.Lflag &^= echo | icanon
		t.Cc[vmin] = 1
		t.Cc[vtime] = 0
	})
}
