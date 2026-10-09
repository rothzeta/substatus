//go:build !unix

package ui

import (
	"context"
	"os"
)

// ResizeSignals returns a channel that never fires: this platform has no
// resize signal.
func ResizeSignals(context.Context) <-chan os.Signal { return nil }
