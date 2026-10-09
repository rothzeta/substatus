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
	"runtime/debug"
	"syscall"
	"time"

	"github.com/rothzeta/substatus/internal/provider"
	"github.com/rothzeta/substatus/internal/runner"
	"github.com/rothzeta/substatus/internal/status"
	"github.com/rothzeta/substatus/internal/term"
	"github.com/rothzeta/substatus/internal/ui"
)

// version is set at release build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

// currentVersion falls back to the module version recorded by `go install`.
func currentVersion() string {
	if version == "dev" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			return bi.Main.Version
		}
	}
	return version
}

// minRefresh keeps automatic refreshes from respawning every CLI back to back;
// one cycle costs several CPU-seconds.
const minRefresh = 15 * time.Second

// shutdownGrace bounds how long quitting waits for provider subprocesses to be
// killed and reaped.
const shutdownGrace = 3 * time.Second

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// configuredProviders lists the providers in display order.
func configuredProviders() []runner.Provider {
	return []runner.Provider{
		provider.Claude{},
		provider.Codex{ClientVersion: currentVersion()},
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
		refresh = fs.Duration("refresh", runner.DefaultInterval, "interval between automatic refreshes (minimum 15s)")
		once    = fs.Bool("once", false, "print one snapshot and exit (for scripting)")
		noColor = fs.Bool("no-color", false, "disable ANSI color (also honors NO_COLOR)")
		showVer = fs.Bool("version", false, "print version and exit")
		setKey  = fs.Bool("set-opencode-key", false, "read an OpenCode API key from stdin and save it")
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
		fmt.Fprintln(stdout, "substatus", currentVersion())
		return 0
	}

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer cancel()

	if *setKey {
		return saveOpenCodeKey(ctx, stdin, stdout, stderr)
	}
	if *refresh < minRefresh {
		fmt.Fprintf(stderr, "substatus: --refresh must be at least %s\n", minRefresh)
		return 2
	}

	out, _ := stdout.(*os.File) // nil (no color) for non-file writers
	pal := ui.Palette{Enabled: ui.ColorEnabled(*noColor, out)}
	r := runner.New(*refresh, configuredProviders()...)

	if *once {
		ui.RenderOnce(stdout, r.Refresh(ctx), pal)
		return 0
	}
	if !term.IsTerminal(stdin) || !term.IsTerminal(out) {
		fmt.Fprintln(stderr, "substatus: the interactive view needs a terminal; use --once for scripts")
		return 2
	}
	return interactive(ctx, r, stdin, out, *refresh, pal)
}

// interactive runs the TUI until quit, a signal, or end of input. Fetching
// happens in the runner, so this loop only draws and stays responsive to quit
// keys and resizes while providers are slow.
func interactive(ctx context.Context, r runner.Runner, in, out *os.File, interval time.Duration, pal ui.Palette) int {
	ctx, cancel := context.WithCancel(ctx)
	tui := ui.NewTUI(ui.Options{Interval: interval, Palette: pal, In: in, Out: out})
	restoreInput := tui.Start()
	snaps := r.Watch(ctx, tui.Refresh())
	resizes := ui.ResizeSignals(ctx)

	var last status.Snapshot
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-tui.Quit():
			break loop
		case snap, ok := <-snaps:
			if !ok {
				break loop
			}
			last = snap
			tui.Draw(last)
		case <-resizes:
			tui.Draw(last)
		}
	}

	// Cancel in-flight checks and wait (bounded) for their subprocesses to be
	// killed, so none outlive the program.
	cancel()
	grace := time.After(shutdownGrace)
drain:
	for {
		select {
		case _, ok := <-snaps:
			if !ok {
				break drain
			}
		case <-grace:
			break drain
		}
	}
	restoreInput()
	tui.Clear()
	return 0
}

// saveOpenCodeKey prompts for (or reads piped) OpenCode API key and stores it.
func saveOpenCodeKey(ctx context.Context, stdin *os.File, stdout, stderr io.Writer) int {
	tty := term.IsTerminal(stdin)
	if tty {
		fmt.Fprint(stderr, "OpenCode API key: ")
	}
	key, err := term.ReadSecret(ctx, stdin)
	if tty {
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
