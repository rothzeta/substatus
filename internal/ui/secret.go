package ui

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
)

// IsTerminal reports whether f is an interactive terminal; nil is not.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ReadSecret reads one line from f, hiding the input when f is a terminal.
func ReadSecret(f *os.File) (string, error) {
	restore, err := disableEcho(f)
	if err != nil {
		return "", err
	}
	defer restore()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
