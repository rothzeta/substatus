// Package ui renders provider status to a terminal.
//
// Rendering is plain escape-code based: no framework, and color is disabled
// when NO_COLOR is set, stdout is not a TTY, or --no-color is passed.
package ui

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/local/substatus/internal/status"
)

// ANSI codes.
const (
	esc     = "\x1b["
	reset   = esc + "0m"
	bold    = esc + "1m"
	dim     = esc + "2m"
	fgRed   = esc + "31m"
	fgGreen = esc + "32m"
	fgYel   = esc + "33m"
	fgBlue  = esc + "34m"
	fgCyan  = esc + "36m"
	fgGray  = esc + "90m"
)

// Palette holds the escape codes actually in use.
type Palette struct {
	Enabled bool
}

func (p Palette) wrap(code, s string) string {
	if !p.Enabled {
		return s
	}
	return code + s + reset
}

func (p Palette) Bold(s string) string   { return p.wrap(bold, s) }
func (p Palette) Dim(s string) string    { return p.wrap(dim, s) }
func (p Palette) Red(s string) string    { return p.wrap(fgRed, s) }
func (p Palette) Green(s string) string  { return p.wrap(fgGreen, s) }
func (p Palette) Yellow(s string) string { return p.wrap(fgYel, s) }
func (p Palette) Cyan(s string) string   { return p.wrap(fgCyan, s) }
func (p Palette) Gray(s string) string   { return p.wrap(fgGray, s) }
func (p Palette) Blue(s string) string   { return p.wrap(fgBlue, s) }

// ColorEnabled reports whether ANSI color should be used on out.
func ColorEnabled(noColorFlag bool, out *os.File) bool {
	if noColorFlag || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := out.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// RenderOnce writes a plain-text snapshot for scripting (no cursor control).
func RenderOnce(w io.Writer, snap status.Snapshot, pal Palette) {
	fmt.Fprintln(w, header(snap, pal))
	for _, p := range snap.Providers {
		fmt.Fprintln(w, card(p, pal))
	}
}

// header renders the top summary line.
func header(snap status.Snapshot, pal Palette) string {
	when := "checking…"
	if !snap.CheckedAt.IsZero() {
		when = snap.CheckedAt.Format("2006-01-02 15:04:05")
		if snap.Refreshing {
			when += " · refreshing…"
		}
	}
	return pal.Bold("Subscription status") + "  " + pal.Dim(when)
}

// card renders one provider as a two-line block.
func card(p status.Provider, pal Palette) string {
	var b strings.Builder
	b.WriteString(stateBadge(p, pal))
	b.WriteString(" ")
	b.WriteString(pal.Bold(p.Name))
	if p.Plan != "" {
		b.WriteString("  " + pal.Cyan("plan: "+p.Plan))
	}
	b.WriteString("\n")
	b.WriteString("    " + pal.Dim("source: "+srcLine(p)) + "\n")

	for _, wnd := range p.Windows {
		b.WriteString("    " + windowLine(wnd, pal) + "\n")
	}
	if p.Note != "" {
		note := pal.Gray
		if p.State == status.StateError {
			note = pal.Red
		}
		b.WriteString("    " + note(p.Note) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func srcLine(p status.Provider) string {
	if p.Source == "" {
		return p.Name
	}
	return p.Source + " [" + p.Quality.String() + "]"
}

func windowLine(w status.Window, pal Palette) string {
	name := fmt.Sprintf("%-22s", w.Label)
	bar := progressBar(w.Percent, 20)
	pct := status.FormatPercent(w.Percent)
	line := name + " " + colorByPercent(bar, w.Percent, pal) + " " + pct
	if !w.ResetsAt.IsZero() {
		line += "  " + pal.Dim("resets "+w.ResetsAt.Local().Format("15:04 Jan 2"))
	}
	return line
}

// progressBar renders a used-percent bar; unknown percentages render hollow.
func progressBar(percent float64, width int) string {
	if percent < 0 {
		return strings.Repeat("░", width)
	}
	filled := int(math.Round(min(percent, 100) / 100 * float64(width)))
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func colorByPercent(s string, percent float64, pal Palette) string {
	switch {
	case percent < 0:
		return pal.Gray(s)
	case percent >= 90:
		return pal.Red(s)
	case percent >= 70:
		return pal.Yellow(s)
	default:
		return pal.Green(s)
	}
}

func stateBadge(p status.Provider, pal Palette) string {
	switch p.State {
	case status.StateOK:
		return pal.Green("● ok")
	case status.StateLoading:
		return pal.Blue("… loading")
	case status.StateAuthMissing:
		return pal.Yellow("● auth")
	case status.StateNotInstalled:
		return pal.Gray("● missing")
	case status.StateUnsupported:
		return pal.Cyan("● unsupported")
	case status.StateUnavailable:
		return pal.Yellow("● unavailable")
	default:
		return pal.Red("● error")
	}
}
