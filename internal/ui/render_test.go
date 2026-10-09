package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/local/substatus/internal/status"
)

func sample() status.Snapshot {
	return status.Snapshot{
		CheckedAt: time.Date(2026, 10, 8, 10, 30, 0, 0, time.UTC),
		Providers: []status.Provider{
			{
				Name: "Claude", State: status.StateUnavailable,
				Source: "Claude Code status-line JSON", Quality: status.QualityCLI,
				Note: "configure status-line bridge",
			},
			{
				Name: "Codex", State: status.StateOK,
				Source: "codex app-server: account/rateLimits/read", Quality: status.QualityCLI,
				Windows: []status.Window{{Label: "codex 5h", Percent: 25}},
			},
			{
				Name: "Gemini (agy)", State: status.StateOK,
				Source: "agy /usage CLI", Quality: status.QualityCLI,
				Windows: []status.Window{{Label: "weekly", Percent: 40, ResetsAt: time.Date(2026, 10, 14, 9, 59, 59, 0, time.UTC)}},
			},
			{
				Name: "OpenCode", State: status.StateNotInstalled, Note: "missing API key",
				Source: "first-party API", Quality: status.QualityPrivate,
			},
		},
	}
}

func TestRenderOnceIncludesAllProviders(t *testing.T) {
	var buf bytes.Buffer
	RenderOnce(&buf, sample(), Palette{Enabled: false})
	out := buf.String()
	for _, name := range []string{"Claude", "Codex", "Gemini", "OpenCode"} {
		if !strings.Contains(out, name) {
			t.Errorf("output missing provider %q\n%s", name, out)
		}
	}
}

func TestRenderOnceShowsStates(t *testing.T) {
	var buf bytes.Buffer
	RenderOnce(&buf, sample(), Palette{Enabled: false})
	out := buf.String()
	for _, want := range []string{"ok", "unavailable", "missing", "configure status-line bridge"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
}

func TestRenderOnceNoColorEmitsNoEscapes(t *testing.T) {
	var buf bytes.Buffer
	snap := sample()
	RenderOnce(&buf, snap, Palette{Enabled: false})
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("disabled palette emitted ANSI escapes:\n%q", buf.String())
	}
}

func TestRenderOnceColorEmitsEscapes(t *testing.T) {
	var buf bytes.Buffer
	RenderOnce(&buf, sample(), Palette{Enabled: true})
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Fatal("enabled palette emitted no ANSI escapes")
	}
}

func TestRenderOnceShowsPercentAndSource(t *testing.T) {
	var buf bytes.Buffer
	RenderOnce(&buf, sample(), Palette{Enabled: false})
	out := buf.String()
	if !strings.Contains(out, "40%") {
		t.Errorf("missing percentage\n%s", out)
	}
	if !strings.Contains(out, "provider CLI") {
		t.Errorf("missing supported CLI source quality\n%s", out)
	}
}

func TestProgressBarClamps(t *testing.T) {
	if got := progressBar(-1, 10); got != "░░░░░░░░░░" {
		t.Errorf("unknown = %q", got)
	}
	if got := progressBar(0, 10); got != "░░░░░░░░░░" {
		t.Errorf("0%% = %q", got)
	}
	if got := progressBar(100, 10); got != "██████████" {
		t.Errorf("100%% = %q", got)
	}
	if got := progressBar(250, 10); got != "██████████" {
		t.Errorf("over 100%% = %q", got)
	}
	if got := progressBar(50, 10); got != "█████░░░░░" {
		t.Errorf("50%% = %q", got)
	}
}

func TestColorEnabledHonorsNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if ColorEnabled(false) {
		t.Error("NO_COLOR set but color enabled")
	}
	if ColorEnabled(true) {
		t.Error("explicit flag should disable color")
	}
}

func TestUnavailableSourceShowsActionableNote(t *testing.T) {
	var out bytes.Buffer
	RenderOnce(&out, status.Snapshot{Providers: []status.Provider{{Name: "Claude", State: status.StateUnavailable, Note: "configure status-line"}}}, Palette{})
	if !strings.Contains(out.String(), "unavailable Claude") || !strings.Contains(out.String(), "configure status-line") || strings.Contains(out.String(), "unsupported") {
		t.Fatalf("output = %q", out.String())
	}
}
