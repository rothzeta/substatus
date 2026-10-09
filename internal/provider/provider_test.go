package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

// Output captured from `claude -p /usage --output-format json` (Claude Code
// 2.1.295, 2026-10-09), trimmed. num_turns 0 and local_command confirm /usage
// ran locally without a model turn.
const claudeLiveUsageOutput = `{"type":"result","subtype":"success","is_error":false,"num_turns":0,"total_cost_usd":0,"local_command":"usage","session_id":"private-session","result":"You are currently using your subscription to power your Claude Code usage\n\nCurrent session: 4% used · resets Oct 9, 1:50pm (UTC)\nCurrent week (all models): 37% used · resets Oct 14, 10am (UTC)\nCurrent week (Fable): 0% used · resets Oct 14, 10am (UTC)\n\nWhat's contributing to your limits usage?\nLast 24h · 1928 requests · 10 sessions\n  94% of your usage came from subagent-heavy sessions\n"}`

func claudeCLI(t *testing.T, output string) (argsPath, envPath string) {
	t.Helper()
	argsPath = filepath.Join(t.TempDir(), "args")
	envPath = filepath.Join(t.TempDir(), "env")
	t.Setenv("SUBSTATUS_TEST_ARGS", argsPath)
	t.Setenv("SUBSTATUS_TEST_ENV", envPath)
	t.Setenv("SUBSTATUS_TEST_OUTPUT", output)
	fakeCLI(t, "claude", `printf '%s' "$*" > "$SUBSTATUS_TEST_ARGS"
printf '%s|%s' "${OPENCODE_API_KEY-}" "${CLAUDE_CODE_OAUTH_TOKEN-}" > "$SUBSTATUS_TEST_ENV"
printf '%s\n' "$SUBSTATUS_TEST_OUTPUT"
`)
	return argsPath, envPath
}

func TestClaudeRunsLocalUsageCommand(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "secret-opencode-key")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "claude-own-token")
	argsPath, envPath := claudeCLI(t, claudeLiveUsageOutput)

	got := (Claude{}).Fetch(context.Background())
	if got.State != status.StateOK || got.Quality != status.QualityCLI || len(got.Windows) != 3 {
		t.Fatalf("got %+v", got)
	}
	want := []struct {
		label   string
		percent float64
	}{{"session", 4}, {"week (all models)", 37}, {"week (Fable)", 0}}
	for i, w := range want {
		if got.Windows[i].Label != w.label || got.Windows[i].Percent != w.percent || got.Windows[i].ResetsAt.IsZero() {
			t.Errorf("window %d = %+v, want %+v", i, got.Windows[i], w)
		}
	}
	if args, _ := os.ReadFile(argsPath); string(args) != "-p /usage --output-format json --no-session-persistence" {
		t.Fatalf("claude args = %q", args)
	}
	if env, _ := os.ReadFile(envPath); string(env) != "|claude-own-token" {
		t.Fatalf("claude env (OPENCODE_API_KEY|CLAUDE_CODE_OAUTH_TOKEN) = %q", env)
	}
	if strings.Contains(got.Note, "private-session") {
		t.Fatal("session identity leaked")
	}
}

func TestClaudeRejectsModelTurnsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		output string
		state  status.State
	}{
		{`{"type":"result","subtype":"success","num_turns":1,"result":"Current session: 4% used"}`, status.StateUnsupported},
		{`{"type":"result","subtype":"success","num_turns":0,"result":"Current session: 4% used"}`, status.StateUnsupported},
		{`{"type":"result","subtype":"error","is_error":true,"num_turns":0,"local_command":"usage","result":"secret"}`, status.StateError},
		{`{"type":"result","subtype":"success","num_turns":0,"local_command":"usage","result":"You are using API billing"}`, status.StateUnavailable},
		{`not-json-secret`, status.StateError},
		{`{}`, status.StateError},
	} {
		claudeCLI(t, tc.output)
		got := (Claude{}).Fetch(context.Background())
		if got.State != tc.state || len(got.Windows) != 0 || strings.Contains(got.Note, "secret") {
			t.Errorf("output %s: got %+v, want %v", tc.output, got, tc.state)
		}
	}
}

func TestClaudeWithoutCLIIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := (Claude{}).Fetch(context.Background()); got.State != status.StateNotInstalled {
		t.Fatalf("got %+v", got)
	}
}

func TestParseClaudeUsageNeverFabricates(t *testing.T) {
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	got := parseClaudeUsage(strings.Join([]string{
		"Current session: 12.5% used · resets 1:50pm (UTC)",
		"Current week (all models): 101% used · resets Jan 3, 10am (UTC)", // invalid percent
		"Current week (Fable): 7% used · resets someday",                  // unparseable reset
		"Current week (Opus): 9% used",
		"  94% of your usage came from subagent-heavy sessions",
	}, "\n"), now)
	if len(got) != 3 {
		t.Fatalf("windows = %+v", got)
	}
	if !got[0].ResetsAt.Equal(time.Date(2026, 12, 30, 13, 50, 0, 0, time.UTC)) || got[0].Percent != 12.5 {
		t.Errorf("session = %+v", got[0])
	}
	if got[1].Label != "week (Fable)" || !got[1].ResetsAt.IsZero() {
		t.Errorf("fable = %+v", got[1])
	}
	if got[2].Label != "week (Opus)" || !got[2].ResetsAt.IsZero() {
		t.Errorf("opus = %+v", got[2])
	}
}

func TestParseClaudeResetInfersYearAndDay(t *testing.T) {
	now := time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"Jan 3, 10am (UTC)":        time.Date(2027, 1, 3, 10, 0, 0, 0, time.UTC),
		"Dec 30, 1:50pm (UTC)":     time.Date(2026, 12, 30, 13, 50, 0, 0, time.UTC),
		"10am (UTC)":               time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC),
		"Feb 1, 2027, 9am (UTC)":   time.Date(2027, 2, 1, 9, 0, 0, 0, time.UTC),
		"Dec 31, 9am (Asia/Tokyo)": time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		"Dec 31, 9am (Not/AZone)":  {},
		"Dec 31, 9am":              {},
		"tomorrow (UTC)":           {},
	} {
		if got := parseClaudeReset(in, now); !got.Equal(want) {
			t.Errorf("parseClaudeReset(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseClaudeResetHandlesLeapDay(t *testing.T) {
	now := time.Date(2027, 12, 1, 0, 0, 0, 0, time.UTC)
	want := time.Date(2028, 2, 29, 10, 0, 0, 0, time.UTC)
	if got := parseClaudeReset("Feb 29, 10am (UTC)", now); !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestClaudeUnrecognisedFormatIsAnError(t *testing.T) {
	claudeCLI(t, `{"type":"result","subtype":"success","num_turns":0,"local_command":"usage","result":"Session — 4% used, resetting soon"}`)
	if got := (Claude{}).Fetch(context.Background()); got.State != status.StateError {
		t.Fatalf("got %+v", got)
	}
}

func TestRunCLIUsesPrivateDirAndCapsOutput(t *testing.T) {
	pwdFile := filepath.Join(t.TempDir(), "pwd")
	t.Setenv("SUBSTATUS_TEST_PWD", pwdFile)
	fakeCLI(t, "tool", `printf '%s' "$PWD" > "$SUBSTATUS_TEST_PWD"
printf ok
`)
	out, err := runCLI(context.Background(), "tool", nil)
	if err != nil || string(out) != "ok" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	dir, _ := os.ReadFile(pwdFile)
	if string(dir) == "" || string(dir) == os.TempDir() || !strings.Contains(filepath.Base(string(dir)), "substatus-") {
		t.Fatalf("CLI ran in %q, want a private substatus- directory", dir)
	}
	if _, err := os.Stat(string(dir)); !os.IsNotExist(err) {
		t.Fatalf("private directory %q not removed: %v", dir, err)
	}

	// 2 MiB of output from a shell loop (no external commands on PATH).
	fakeCLI(t, "tool", `chunk=xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
i=0
while [ $i -lt 32768 ]; do printf '%s\n' "$chunk"; i=$((i+1)); done
`)
	if out, err := runCLI(context.Background(), "tool", nil); !errors.Is(err, errOutputLimit) || out != nil {
		t.Fatalf("len(out) = %d, err = %v; want errOutputLimit", len(out), err)
	}
}

func TestEnvWithoutSecretsKeepsOnlyNamedCredentials(t *testing.T) {
	got := envWithoutSecrets([]string{
		"PATH=/bin", "HOME=/h", "OPENCODE_API_KEY=a", "GITHUB_TOKEN=b", "AWS_SECRET_ACCESS_KEY=c",
		"MY_SECRET=d", "DB_PASSWORD=e", "CLAUDE_CODE_OAUTH_TOKEN=f",
	}, "CLAUDE_CODE_OAUTH_TOKEN")
	if strings.Join(got, ",") != "PATH=/bin,HOME=/h,CLAUDE_CODE_OAUTH_TOKEN=f" {
		t.Fatalf("env = %v", got)
	}
}
