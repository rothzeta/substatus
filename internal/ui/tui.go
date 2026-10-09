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

// Screen control. The TUI draws on the alternate screen, so quitting restores
// whatever the terminal showed before.
const (
	enterAltScreen = esc + "?1049h" + esc + "?25l" // also hides the cursor
	leaveAltScreen = esc + "?25h" + esc + "?1049l"
	cursorHome     = esc + "H"
	eraseLine      = esc + "K" // to the end of the line
	eraseBelow     = esc + "J" // to the end of the screen
)

// Options configures the interactive TUI.
type Options struct {
	Interval time.Duration
	Palette  Palette
	In       *os.File
	Out      io.Writer
}

// TUI is a small interactive loop: the caller redraws on every snapshot and
// terminal resize; keypresses request a refresh (r) or quit (q). Raw mode
// keeps signal keys, so Ctrl-C reaches the caller as SIGINT.
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

// Start switches to the alternate screen, enables raw terminal input where
// supported, and starts the key reader. The returned function restores the
// terminal's previous input settings and screen.
func (t *TUI) Start() (restore func()) {
	restoreInput, err := term.MakeRaw(t.opts.In)
	if err != nil {
		restoreInput = func() {} // line-buffered input still works
	}
	fmt.Fprint(t.opts.Out, enterAltScreen)
	go t.keys()
	return func() {
		restoreInput()
		fmt.Fprint(t.opts.Out, leaveAltScreen)
	}
}

// Draw repaints the screen with snap. It overwrites the previous frame in
// place, erasing what each line and the rest of the screen held before,
// because clearing the whole screen first flickers.
func (t *TUI) Draw(snap status.Snapshot) {
	pal := t.opts.Palette
	var b strings.Builder
	b.WriteString(header(snap, pal) + "\n")
	b.WriteString(pal.Dim(fmt.Sprintf("refresh every %s · r refresh · q quit", t.opts.Interval)) + "\n\n")
	for _, p := range snap.Providers {
		b.WriteString(card(p, pal) + "\n\n")
	}
	fmt.Fprint(t.opts.Out, cursorHome+strings.ReplaceAll(b.String(), "\n", eraseLine+"\n")+eraseBelow)
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
		case 'q', 'Q':
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
