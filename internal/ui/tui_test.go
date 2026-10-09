package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

func TestStartReadsRefreshAndQuitKeys(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	tui := NewTUI(Options{In: reader, Out: io.Discard})
	restore := tui.Start()
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

func TestDrawsInPlaceOnTheAlternateScreen(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()

	var out bytes.Buffer
	tui := NewTUI(Options{In: reader, Out: &out})
	restore := tui.Start()
	if out.String() != enterAltScreen {
		t.Fatalf("Start wrote %q, want the alternate screen entered", out.String())
	}

	out.Reset()
	tui.Draw(status.Snapshot{Providers: []status.Provider{{Name: "Codex", State: status.StateOK}}})
	frame := out.String()
	if !strings.HasPrefix(frame, cursorHome) || !strings.HasSuffix(frame, eraseBelow) {
		t.Fatalf("frame %q does not redraw from home and erase below", frame)
	}
	if strings.Contains(frame, esc+"2J") {
		t.Fatalf("frame %q clears the whole screen", frame)
	}
	if strings.Count(frame, "\n") != strings.Count(frame, eraseLine+"\n") {
		t.Fatalf("frame %q leaves a line without erasing its old tail", frame)
	}
	if !strings.Contains(frame, "Codex") {
		t.Fatalf("frame %q misses the provider", frame)
	}

	out.Reset()
	restore()
	if out.String() != leaveAltScreen {
		t.Fatalf("restore wrote %q, want the alternate screen left", out.String())
	}
}
