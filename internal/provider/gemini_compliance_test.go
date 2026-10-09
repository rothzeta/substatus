package provider

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

func TestGeminiUsesTheDocumentedAgyUsageCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fake agy executable uses a POSIX shell")
	}
	binDir := t.TempDir()
	argsPath := filepath.Join(t.TempDir(), "args")
	inheritedKeyPath := filepath.Join(t.TempDir(), "inherited-key")
	fixture := `{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[{"id":"weekly","name":"Weekly","window":"weekly","remaining_fraction":0.6,"reset_time":"2030-01-03T00:00:00Z"}]}]}}}`
	script := "#!/bin/sh\nprintf '%s' \"$*\" > \"$SUBSTATUS_TEST_ARGS\"\nprintf '%s' \"${OPENCODE_API_KEY-}\" > \"$SUBSTATUS_TEST_KEY\"\nprintf '%s' '" + fixture + "'\n"
	agyPath := filepath.Join(binDir, "agy")
	if err := os.WriteFile(agyPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("HOME", t.TempDir()) // no CLI credential file for the provider to read
	t.Setenv("SUBSTATUS_TEST_ARGS", argsPath)
	t.Setenv("SUBSTATUS_TEST_KEY", inheritedKeyPath)
	t.Setenv("OPENCODE_API_KEY", "synthetic-opencode-key")

	got := (&Gemini{}).Fetch(context.Background())
	if got.State != status.StateOK {
		t.Fatalf("state = %v, note = %q", got.State, got.Note)
	}
	if got.Quality != status.QualityCLI {
		t.Fatalf("quality = %v, want provider CLI", got.Quality)
	}
	if len(got.Windows) != 1 || got.Windows[0].Label != "Gemini Models weekly" || got.Windows[0].Percent != 40 {
		t.Fatalf("windows = %+v", got.Windows)
	}
	if got.Windows[0].ResetsAt.IsZero() || !got.Windows[0].ResetsAt.Equal(time.Date(2030, 1, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("reset = %+v", got.Windows[0])
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("agy CLI was not invoked: %v", err)
	}
	if string(args) != "--print /usage --output-format json" {
		t.Fatalf("agy args = %q", args)
	}
	inheritedKey, err := os.ReadFile(inheritedKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(inheritedKey) != 0 {
		t.Fatal("OpenCode API key leaked into agy CLI environment")
	}
}

// Output captured from `agy --print /usage --output-format json` (agy 1.3.2,
// 2026-10-08). Zero token usage confirms /usage runs without an agent turn.
const agyLiveUsageOutput = `{"conversation_id":"","status":"SUCCESS","response":"Gemini Models\tWeekly Limit Remaining\t67%\t2026-10-10T18:32:51Z\nGemini Models\tFive Hour Limit Remaining\t100%\t2026-10-09T03:51:29Z\nClaude and GPT models\tWeekly Limit Remaining\t100%\t2026-10-15T22:51:29Z\nClaude and GPT models\tFive Hour Limit Remaining\t100%\t2026-10-09T03:51:29Z\n","duration_seconds":0,"num_turns":0,"usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},"command":{"name":"usage","data":{"description":"Within each group, models share a weekly limit and a 5-hour limit.","groups":[{"name":"Gemini Models","description":"Models within this group: Gemini Flash, Gemini Pro","buckets":[{"id":"gemini-weekly","name":"Weekly Limit Remaining","description":"You have used some of your weekly limit, it will fully refresh in 1 day, 19 hours.","window":"weekly","remaining_fraction":0.6723319888114929,"reset_time":"2026-10-10T18:32:51Z"},{"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":1,"reset_time":"2026-10-09T03:51:29Z"}]},{"name":"Claude and GPT models","description":"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS","buckets":[{"id":"3p-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":1,"reset_time":"2026-10-15T22:51:29Z"},{"id":"3p-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":1,"reset_time":"2026-10-09T03:51:29Z"}]}]}}}`

func TestGeminiParsesLiveAgyUsageShape(t *testing.T) {
	fakeCLI(t, "agy", "printf '%s' '"+agyLiveUsageOutput+"'\n")
	got := (Gemini{}).Fetch(context.Background())
	if got.State != status.StateOK || len(got.Windows) != 4 {
		t.Fatalf("got %+v", got)
	}
	for i, want := range []struct {
		label   string
		percent float64
		reset   string
	}{
		{"Gemini Models weekly", 32.77, "2026-10-10T18:32:51Z"},
		{"Gemini Models 5h", 0, "2026-10-09T03:51:29Z"},
		{"Claude and GPT models weekly", 0, "2026-10-15T22:51:29Z"},
		{"Claude and GPT models 5h", 0, "2026-10-09T03:51:29Z"},
	} {
		w := got.Windows[i]
		reset, _ := time.Parse(time.RFC3339, want.reset)
		if w.Label != want.label || math.Abs(w.Percent-want.percent) > 0.01 || w.ResetsAt.IsZero() || !w.ResetsAt.Equal(reset) {
			t.Errorf("window %d = %+v, want %+v", i, w, want)
		}
	}
}

func TestGeminiCLIErrorIsGenericAndDoesNotEchoOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the fake agy executable uses a POSIX shell")
	}
	binDir := t.TempDir()
	agyPath := filepath.Join(binDir, "agy")
	if err := os.WriteFile(agyPath, []byte("#!/bin/sh\nprintf '%s' 'secret-cli-output'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got := (&Gemini{}).Fetch(context.Background())
	if got.State != status.StateError || strings.Contains(got.Note, "secret-cli-output") {
		t.Fatalf("CLI error was not safely reported: %+v", got)
	}
}

func TestGeminiWithoutAgyIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	got := (&Gemini{}).Fetch(context.Background())
	if got.State != status.StateNotInstalled {
		t.Fatalf("state = %v, want not installed", got.State)
	}
}

// The CLI omits remaining_fraction for disabled buckets. Missing and null
// values must never become the float64 zero value (100% used).
func TestGeminiUsageBucketsNeverFabricateQuota(t *testing.T) {
	for _, tc := range []struct {
		name, bucket string
		state        status.State
		windows      int
		percent      float64
	}{
		{"missing fraction", `{"id":"gemini-5h","window":"5h"}`, status.StateUnavailable, 0, 0},
		{"null fraction", `{"window":"weekly","remaining_fraction":null}`, status.StateUnavailable, 0, 0},
		{"disabled", `{"window":"weekly","remaining_fraction":0,"disabled":true}`, status.StateUnavailable, 0, 0},
		{"exhausted", `{"window":"5h","remaining_fraction":0}`, status.StateOK, 1, 100},
		{"unused", `{"window":"weekly","remaining_fraction":1}`, status.StateOK, 1, 0},
		{"negative", `{"remaining_fraction":-0.1}`, status.StateError, 0, 0},
		{"over one", `{"remaining_fraction":1.1}`, status.StateError, 0, 0},
		{"wrong type", `{"remaining_fraction":"0.5"}`, status.StateError, 0, 0},
		{"invalid reset", `{"remaining_fraction":0.5,"reset_time":"not-a-date"}`, status.StateError, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := fmt.Sprintf(`{"status":"SUCCESS","command":{"name":"usage","data":{"groups":[{"name":"Gemini Models","buckets":[%s]}]}}}`, tc.bucket)
			fakeCLI(t, "agy", "printf '%s' '"+fixture+"'\n")
			got := (Gemini{}).Fetch(context.Background())
			if got.State != tc.state || len(got.Windows) != tc.windows {
				t.Fatalf("got %+v; want state %v, %d windows", got, tc.state, tc.windows)
			}
			if tc.windows > 0 && got.Windows[0].Percent != tc.percent {
				t.Fatalf("window = %+v", got.Windows[0])
			}
		})
	}
}

func TestGeminiRejectsNonUsageJSON(t *testing.T) {
	for _, fixture := range []string{`{`, `{}`, `{"status":"ERROR","error":"secret"}`, `{"status":"SUCCESS","response":"text","usage":{"total_tokens":100}}`} {
		fakeCLI(t, "agy", "printf '%s' '"+fixture+"'\n")
		got := (Gemini{}).Fetch(context.Background())
		if got.State != status.StateError || len(got.Windows) != 0 || strings.Contains(got.Note, "secret") {
			t.Fatalf("got %+v", got)
		}
	}
}
