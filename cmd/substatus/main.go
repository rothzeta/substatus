// Command substatus shows colored subscription/quota status for Codex, Claude,
// Gemini (via the local `agy` CLI), and OpenCode.
//
// It is read-only: Claude and Gemini usage come from their CLIs' local /usage
// commands, Codex from documented app-server status methods, and OpenCode from
// its first-party API. It never runs model prompts to measure status or reads
// provider credential files.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/local/substatus/internal/provider"
	"github.com/local/substatus/internal/runner"
	"github.com/local/substatus/internal/status"
	"github.com/local/substatus/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// configuredProviders lists the providers in display order.
func configuredProviders() []runner.Provider {
	return []runner.Provider{
		provider.Claude{},
		provider.Codex{},
		provider.Gemini{},
		provider.OpenCode{},
	}
}

// run is the whole program except process exit, so tests can exercise the CLI
// without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("substatus", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		refresh = fs.Duration("refresh", runner.DefaultInterval, "interval between automatic refreshes (e.g. 30s, 2m)")
		once    = fs.Bool("once", false, "print one snapshot and exit (for scripting)")
		noColor = fs.Bool("no-color", false, "disable ANSI color (also honors NO_COLOR)")
		showVer = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "substatus — subscription & quota status for local AI CLIs\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  substatus [flags]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nProviders: Codex, Claude, Gemini (agy), OpenCode.\n")
		fmt.Fprintf(fs.Output(), "Claude and Gemini use their CLIs' /usage; Codex uses app-server status.\n")
		fmt.Fprintf(fs.Output(), "No provider credential files are read. OpenCode uses OPENCODE_API_KEY.\n")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVer {
		fmt.Fprintln(stdout, "substatus", provider.Version)
		return 0
	}
	if *refresh <= 0 {
		fmt.Fprintln(stderr, "substatus: --refresh must be positive")
		return 2
	}

	pal := ui.Palette{Enabled: ui.ColorEnabled(*noColor)}
	r := runner.New(configuredProviders()...)
	r.Interval = *refresh

	if *once {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		ui.RenderOnce(stdout, r.Refresh(ctx), pal)
		return 0
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tui := ui.NewTUI(ui.Options{Interval: *refresh, Palette: pal, Out: stdout})
	defer tui.Clear()
	restoreInput := tui.Start(ctx)
	defer restoreInput()

	snaps := r.Watch(ctx)
	resizes := ui.ResizeSignals(ctx)

	// The first snapshot draws immediately; later snapshots, manual refresh
	// requests, and terminal resizes all trigger a redraw. The latest snapshot
	// is retained so a resize can repaint without re-querying providers.
	var last status.Snapshot
	haveSnapshot := false
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-tui.Quit():
			return 0
		case snap, ok := <-snaps:
			if !ok {
				return 0
			}
			last, haveSnapshot = snap, true
			tui.Draw(snap, *refresh)
		case <-tui.Refresh():
			ctx2, cancel2 := context.WithTimeout(ctx, 25*time.Second)
			snap := r.Refresh(ctx2)
			cancel2()
			last, haveSnapshot = snap, true
			tui.Draw(snap, *refresh)
		case <-resizes:
			if haveSnapshot {
				tui.Draw(last, *refresh)
			}
		}
	}
}
