//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package ui

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// ResizeSignals delivers terminal resize notifications until ctx is cancelled.
func ResizeSignals(ctx context.Context) <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		<-ctx.Done()
		signal.Stop(ch)
	}()
	return ch
}
