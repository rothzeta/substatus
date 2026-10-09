package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeUsesOnlyStatusLineOutput(t *testing.T) {
	fakeCLI(t, "claude", "exit 99\n") // the application must never launch Claude
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	got := (Claude{}).Fetch(context.Background())
	if got.State != ResUnavailable || len(got.Windows) != 0 || !strings.Contains(got.Note, "status-line") {
		t.Fatalf("got %+v", got)
	}
	path, err := ClaudeUsagePath()
	if err != nil {
		t.Fatal(err)
	}
	input := `{"session_id":"private-id","transcript_path":"private-path","rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1893456000},"seven_day":{"used_percentage":41.2,"resets_at":1894060800}},"context_window":{"used_percentage":90},"unknown_secret":"private-token"}`
	if err := SaveClaudeStatusLine(strings.NewReader(input), path, time.Now()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-") || strings.Contains(string(data), "context_window") {
		t.Fatal("persisted non-quota session data")
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache permissions: %v, %v", fi, err)
	}
	got = (Claude{}).Fetch(context.Background())
	if got.State != ResOK || got.Quality != QualityCLI || len(got.Windows) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Windows[0].Label != "5h" || got.Windows[0].Percent != 23.5 || got.Windows[0].ResetsAt.Unix() != 1893456000 {
		t.Fatalf("5h = %+v", got.Windows[0])
	}
}

func TestClaudeDoesNotUseStaleOrExpiredQuotas(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	path, err := ClaudeUsagePath()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input   string
		at      time.Time
		state   ResultState
		windows int
	}{
		{`{"rate_limits":{"five_hour":{"used_percentage":0}}}`, time.Now(), ResOK, 1},
		{`{"rate_limits":{"five_hour":{"used_percentage":null}}}`, time.Now(), ResUnavailable, 0},
		{`{"context_window":{"used_percentage":70}}`, time.Now(), ResUnavailable, 0},
		{`{"rate_limits":{"five_hour":{"used_percentage":40}}}`, time.Now().Add(-10 * time.Minute), ResUnavailable, 0},
		{`{"rate_limits":{"five_hour":{"used_percentage":40,"resets_at":1}}}`, time.Now(), ResUnavailable, 0},
		// spend_limit is documented to exceed 100 once the limit is passed.
		{`{"rate_limits":{"spend_limit":{"used_percentage":112.5,"resets_at":1893456000,"used_usd":562.5,"limit_usd":500,"period":"monthly"}}}`, time.Now(), ResOK, 1},
	} {
		if err := SaveClaudeStatusLine(strings.NewReader(tc.input), path, tc.at); err != nil {
			t.Fatal(err)
		}
		got := (Claude{}).Fetch(context.Background())
		if got.State != tc.state || len(got.Windows) != tc.windows {
			t.Fatalf("got %+v; want %v, %d windows", got, tc.state, tc.windows)
		}
	}
}

func TestClaudeRejectsInvalidStatusLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	for _, input := range []string{
		`{`, `null`,
		`{"rate_limits":{"five_hour":{"used_percentage":101}}}`,
		`{"rate_limits":{"five_hour":{"used_percentage":-1}}}`,
		`{"rate_limits":{"seven_day":{"used_percentage":100.5}}}`,
		`{"rate_limits":{"spend_limit":{"used_percentage":-1}}}`,
		`{"rate_limits":{"five_hour":{"used_percentage":"secret"}}}`,
		`{}` + `{}`, strings.Repeat(" ", maxBody+1),
	} {
		if err := SaveClaudeStatusLine(strings.NewReader(input), path, time.Now()); err == nil {
			t.Fatalf("accepted invalid input %q", input[:min(len(input), 100)])
		}
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
