//go:build !linux

package ui

import "os"

// termSize is a no-op fallback on non-Linux platforms; callers use 80x24.
func termSize(_ *os.File) (int, int, error) { return 0, 0, nil }

// enableRawInput falls back to line-buffered input on non-Linux platforms.
func enableRawInput(_ *os.File) (func(), error) { return func() {}, nil }
