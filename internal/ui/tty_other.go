//go:build !linux

package ui

import "os"

// enableRawInput falls back to line-buffered input on non-Linux platforms.
func enableRawInput(*os.File) (func(), error) { return func() {}, nil }

// disableEcho is unsupported on non-Linux platforms; input stays visible.
func disableEcho(*os.File) (func(), error) { return func() {}, nil }
