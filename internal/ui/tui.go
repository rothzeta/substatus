package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rothzeta/substatus/internal/status"
	"github.com/rothzeta/substatus/internal/term"
)

// clearScreen clears the terminal and homes the cursor.
const clearScreen = esc + "2J" + esc + "H"

// Options configures the interactive TUI.
type Options struct {
	Interval time.Duration
	Palette  Palette
	In       *os.File
	Out      io.Writer
}

// TUI is a small interactive loop: the caller redraws on every snapshot and
// terminal resize; keypresses request a refresh (r) or quit (q / Ctrl-C).
type TUI struct {
	opts     Options
	refresh  chan struct{}
	quit     chan struct{}
	quitOnce sync.Once
}

// NewTUI builds an interactive TUI.
func NewTUI(opts Options) *TUI {
	return &TUI{opts: opts, refresh: make(chan struct{}, 1), quit: make(chan struct{})}
}

// Refresh returns a channel that receives a signal when the user presses "r".
func (t *TUI) Refresh() <-chan struct{} { return t.refresh }

// Quit returns a channel closed when the user presses q or input reaches EOF.
func (t *TUI) Quit() <-chan struct{} { return t.quit }

// Start enables raw terminal input where supported and starts the key reader.
// The returned function restores the terminal's previous settings.
func (t *TUI) Start() (restore func()) {
	restore, err := term.MakeRaw(t.opts.In)
	if err != nil {
		restore = func() {} // line-buffered input still works
	}
	go t.keys()
	return restore
}

// Draw clears and repaints the screen with snap.
func (t *TUI) Draw(snap status.Snapshot) {
	pal := t.opts.Palette
	var b strings.Builder
	b.WriteString(clearScreen)
	b.WriteString(header(snap, pal) + "\n")
	b.WriteString(pal.Dim(fmt.Sprintf("refresh every %s · r refresh · q quit", t.opts.Interval)) + "\n\n")
	for _, p := range snap.Providers {
		b.WriteString(card(p, pal) + "\n\n")
	}
	fmt.Fprint(t.opts.Out, b.String())
}

// Clear wipes the screen on exit.
func (t *TUI) Clear() {
	fmt.Fprint(t.opts.Out, clearScreen)
}

// keys reads single keystrokes until quit or end of input.
func (t *TUI) keys() {
	r := bufio.NewReader(t.opts.In)
	for {
		ch, err := r.ReadByte()
		if err != nil {
			t.quitOnce.Do(func() { close(t.quit) })
			return
		}
		switch ch {
		case 'q', 'Q', 3: // 3 = Ctrl-C
			t.quitOnce.Do(func() { close(t.quit) })
			return
		case 'r', 'R':
			select {
			case t.refresh <- struct{}{}:
			default: // a refresh request is already queued
			}
		}
	}
}
