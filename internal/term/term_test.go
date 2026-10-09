package term

import (
	"context"
	"errors"
	"os"
	"testing"
)

func pipe(t *testing.T) (r, w *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}

func TestNilAndPipesAreNotTerminals(t *testing.T) {
	r, w := pipe(t)
	for _, f := range []*os.File{nil, r, w} {
		if IsTerminal(f) {
			t.Fatalf("IsTerminal(%v) = true", f)
		}
	}
}

func TestMakeRawLeavesNonTerminalsAlone(t *testing.T) {
	r, _ := pipe(t)
	restore, err := MakeRaw(r)
	if err != nil {
		t.Fatal(err)
	}
	restore()
}

func TestReadSecretReadsTheFirstPipedLineTrimmed(t *testing.T) {
	r, w := pipe(t)
	if _, err := w.WriteString("  sk-key \nsecond\n"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSecret(context.Background(), r)
	if err != nil || got != "sk-key" {
		t.Fatalf("ReadSecret = %q, %v; want %q", got, err, "sk-key")
	}
}

func TestReadSecretAcceptsInputWithoutNewline(t *testing.T) {
	r, w := pipe(t)
	if _, err := w.WriteString("sk-key"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	got, err := ReadSecret(context.Background(), r)
	if err != nil || got != "sk-key" {
		t.Fatalf("ReadSecret = %q, %v; want %q", got, err, "sk-key")
	}
}

func TestReadSecretReturnsWhenContextEnds(t *testing.T) {
	r, _ := pipe(t) // the writer stays open, so the read blocks
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadSecret(ctx, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadSecret error = %v; want context.Canceled", err)
	}
}
