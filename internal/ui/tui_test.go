package ui

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

func TestStartReadsRefreshAndQuitKeys(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	tui := NewTUI(Options{In: reader, Out: io.Discard})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	restore := tui.Start(ctx)
	defer restore()

	if _, err := writer.Write([]byte("rq")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tui.Refresh():
	case <-time.After(time.Second):
		t.Fatal("r did not trigger refresh")
	}
	select {
	case <-tui.Quit():
	case <-time.After(time.Second):
		t.Fatal("q did not signal quit")
	}
}
