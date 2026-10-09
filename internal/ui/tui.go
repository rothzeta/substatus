package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/local/substatus/internal/status"
)

// Options configures the interactive TUI.
type Options struct {
	Interval time.Duration
	Palette  Palette
	In       *os.File
	Out      io.Writer
}

// TUI is a small interactive loop: it redraws on every snapshot, on terminal
// resize, and on keypresses (r = refresh now, q / Ctrl-C = quit).
type TUI struct {
	opts     Options
	width    int
	height   int
	mu       sync.Mutex
	snap     status.Snapshot
	started  bool
	refresh  chan struct{}
	quit     chan struct{}
	quitOnce sync.Once
}

const minWidth = 40

// NewTUI builds an interactive TUI.
func NewTUI(opts Options) *TUI {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	return &TUI{opts: opts, width: 80, height: 24, refresh: make(chan struct{}, 1), quit: make(chan struct{})}
}

// Refresh returns a channel that receives a signal when the user presses "r".
func (t *TUI) Refresh() <-chan struct{} { return t.refresh }

// Quit returns a channel closed when the user presses q or input reaches EOF.
func (t *TUI) Quit() <-chan struct{} { return t.quit }

// Start enables raw terminal input where supported and starts the key reader.
// The returned function restores the terminal's previous settings.
func (t *TUI) Start(ctx context.Context) func() {
	restore, err := enableRawInput(t.opts.In)
	if err != nil {
		restore = func() {}
	}
	go t.Keys(ctx)
	return restore
}

func (t *TUI) signalQuit() {
	t.quitOnce.Do(func() { close(t.quit) })
}

// Draw clears and repaints the current snapshot.
func (t *TUI) Draw(snap status.Snapshot, interval time.Duration) {
	t.mu.Lock()
	t.snap = snap
	t.mu.Unlock()

	var b strings.Builder
	b.WriteString(esc + "2J" + esc + "H") // clear + home
	b.WriteString(header(snap, t.opts.Palette) + "\n")
	b.WriteString(t.opts.Palette.Dim(fmt.Sprintf("refresh every %s · r refresh · q quit", interval)) + "\n\n")
	for _, p := range snap.Providers {
		b.WriteString(card(p, t.opts.Palette))
		b.WriteString("\n\n")
	}
	fmt.Fprint(t.opts.Out, b.String())
}

// Clear wipes the screen on exit.
func (t *TUI) Clear() {
	fmt.Fprint(t.opts.Out, esc+"2J"+esc+"H")
}

// Keys reads single keystrokes until ctx is cancelled.
func (t *TUI) Keys(ctx context.Context) {
	r := bufio.NewReader(t.opts.In)
	for {
		ch, err := r.ReadByte()
		if err != nil {
			t.signalQuit()
			return
		}
		switch ch {
		case 'q', 'Q', 3: // 3 = Ctrl-C
			t.signalQuit()
			return
		case 'r', 'R':
			select {
			case t.refresh <- struct{}{}:
			default:
			}
		case '\n', '\r':
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

// ResizeSignals delivers SIGWINCH notifications until ctx is cancelled.
func ResizeSignals(ctx context.Context) <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		<-ctx.Done()
		signal.Stop(ch)
		close(ch)
	}()
	return ch
}

// TerminalSize returns the current terminal size, falling back to 80x24.
func TerminalSize(out io.Writer) (width, height int) {
	width, height = 80, 24
	f, ok := out.(*os.File)
	if !ok {
		return
	}
	w, h, err := termSize(f)
	if err != nil || w <= 0 || h <= 0 {
		return
	}
	return w, h
}
