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

	"github.com/rothzeta/substatus/internal/provider"
	"github.com/rothzeta/substatus/internal/runner"
	"github.com/rothzeta/substatus/internal/status"
	"github.com/rothzeta/substatus/internal/ui"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
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
func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("substatus", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		refresh = fs.Duration("refresh", runner.DefaultInterval, "interval between automatic refreshes (e.g. 30s, 2m)")
		once    = fs.Bool("once", false, "print one snapshot and exit (for scripting)")
		noColor = fs.Bool("no-color", false, "disable ANSI color (also honors NO_COLOR)")
		showVer = fs.Bool("version", false, "print version and exit")
		setKey  = fs.Bool("set-opencode-key", false, "read an OpenCode API key from stdin (hidden prompt on a terminal) and save it")
	)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "substatus — subscription & quota status for local AI CLIs\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  substatus [flags]\n\n")
		fmt.Fprintf(fs.Output(), "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nProviders: Codex, Claude, Gemini (agy), OpenCode.\n")
		fmt.Fprintf(fs.Output(), "Claude and Gemini use their CLIs' /usage; Codex uses app-server status.\n")
		fmt.Fprintf(fs.Output(), "No provider credential files are read. OpenCode uses OPENCODE_API_KEY or --set-opencode-key.\n")
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
	if *setKey {
		return saveOpenCodeKey(stdin, stdout, stderr)
	}
	if *refresh <= 0 {
		fmt.Fprintln(stderr, "substatus: --refresh must be positive")
		return 2
	}

	out, _ := stdout.(*os.File) // nil (no color) for non-file writers
	pal := ui.Palette{Enabled: ui.ColorEnabled(*noColor, out)}
	r := runner.New(*refresh, configuredProviders()...)

	if *once {
		ui.RenderOnce(stdout, r.Refresh(context.Background()), pal)
		return 0
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	tui := ui.NewTUI(ui.Options{Interval: *refresh, Palette: pal, Out: stdout})
	defer tui.Clear()
	restoreInput := tui.Start()
	defer restoreInput()

	// Fetching happens in the runner, so this loop only draws and stays
	// responsive to quit keys and resizes while providers are slow.
	snaps := r.Watch(ctx, tui.Refresh())
	resizes := ui.ResizeSignals(ctx)
	var last status.Snapshot
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
			last = snap
			tui.Draw(last)
		case <-resizes:
			tui.Draw(last)
		}
	}
}

// saveOpenCodeKey prompts for (or reads piped) OpenCode API key and stores it.
func saveOpenCodeKey(stdin *os.File, stdout, stderr io.Writer) int {
	interactive := ui.IsTerminal(stdin)
	if interactive {
		fmt.Fprint(stderr, "OpenCode API key (input hidden): ")
	}
	key, err := ui.ReadSecret(stdin)
	if interactive {
		fmt.Fprintln(stderr)
	}
	if err == nil {
		err = provider.SaveOpenCodeKey(key)
	}
	if err != nil {
		fmt.Fprintln(stderr, "substatus: could not save OpenCode API key:", err)
		return 1
	}
	path, _ := provider.OpenCodeKeyPath()
	fmt.Fprintln(stdout, "saved OpenCode API key to", path)
	return 0
}
