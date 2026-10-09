package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/local/substatus/internal/status"
)

func fakeCLI(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI uses a POSIX shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nset -eu\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// Refuse any unapproved method, and require the documented handshake order.
func codexScript(account, limits string) string {
	return `test "$*" = "app-server"
test -z "${OPENCODE_API_KEY-}"
IFS= read -r request
case "$request" in *'"method":"initialize"'*) ;; *) exit 11 ;; esac
printf '%s\n' '{"method":"account/updated","params":{}}' '{"id":1,"result":{}}'
IFS= read -r request
case "$request" in *'"method":"initialized"'*) ;; *) exit 12 ;; esac
IFS= read -r request
case "$request" in *'"method":"account/read"'*'"refreshToken":false'*) ;; *) exit 13 ;; esac
printf '%s\n' '{"id":99,"method":"server/request","params":{}}' '{"id":2,"result":` + account + `}'
IFS= read -r request
case "$request" in *'"params"'*) exit 16 ;; esac
case "$request" in *'"method":"account/rateLimits/read"'*) ;; *) exit 14 ;; esac
printf '%s\n' '` + limits + `'
while IFS= read -r request; do exit 15; done
`
}

func TestCodexReadsDocumentedAppServerStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no credential file access by this app
	t.Setenv("OPENCODE_API_KEY", "secret-opencode-key")
	fakeCLI(t, "codex", codexScript(
		`{"account":{"type":"chatgpt","planType":"pro","email":"private@example.test"}}`,
		`{"id":3,"result":{"rateLimits":{"primary":{"usedPercent":99}},"rateLimitsByLimitId":{"codex":{"planType":"pro","primary":{"usedPercent":25,"windowDurationMins":300,"resetsAt":1893456000},"secondary":{"usedPercent":42,"windowDurationMins":10080}},"other":{"limitName":"Other","primary":{"usedPercent":0,"windowDurationMins":60}}}}}`,
	))
	got := (Codex{}).Fetch(context.Background())
	if got.State != status.StateOK || got.Plan != "pro" || len(got.Windows) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got.Windows[0].Label != "codex 5h" || got.Windows[0].Percent != 25 || got.Windows[0].ResetsAt.IsZero() || got.Windows[0].ResetsAt.Unix() != 1893456000 {
		t.Fatalf("primary = %+v", got.Windows[0])
	}
	if got.Windows[1].Label != "codex 7d" || got.Windows[1].Percent != 42 || !got.Windows[1].ResetsAt.IsZero() {
		t.Fatalf("secondary = %+v", got.Windows[1])
	}
	if strings.Contains(got.Note, "private@example.test") {
		t.Fatal("account identity leaked")
	}
}

// The response example published with the app-server account/rateLimits/read docs.
func TestCodexParsesDocumentedRateLimitsExample(t *testing.T) {
	fakeCLI(t, "codex", codexScript(
		`{"account":{"type":"chatgpt","email":"user@example.com","planType":"pro"},"requiresOpenaiAuth":true}`,
		`{"id":3,"result":{"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":25,"windowDurationMins":15,"resetsAt":1730947200},"secondary":null,"rateLimitReachedType":null},"rateLimitsByLimitId":{"codex":{"limitId":"codex","limitName":null,"primary":{"usedPercent":25,"windowDurationMins":15,"resetsAt":1730947200},"secondary":null,"rateLimitReachedType":null},"codex_other":{"limitId":"codex_other","limitName":"codex_other","primary":{"usedPercent":42,"windowDurationMins":60,"resetsAt":1730950800},"secondary":null,"rateLimitReachedType":null}},"rateLimitResetCredits":{"availableCount":2,"credits":null}}}`,
	))
	got := (Codex{}).Fetch(context.Background())
	if got.State != status.StateOK || got.Plan != "pro" || len(got.Windows) != 2 {
		t.Fatalf("got %+v", got)
	}
	if w := got.Windows[0]; w.Label != "codex 15m" || w.Percent != 25 || w.ResetsAt.Unix() != 1730947200 {
		t.Fatalf("codex = %+v", w)
	}
	if w := got.Windows[1]; w.Label != "codex_other 1h" || w.Percent != 42 || w.ResetsAt.Unix() != 1730950800 {
		t.Fatalf("codex_other = %+v", w)
	}
}

func TestCodexFallbackAndMissingPercent(t *testing.T) {
	for _, tc := range []struct {
		limits  string
		windows int
		state   status.State
	}{
		{`{"primary":{"usedPercent":0,"windowDurationMins":15}}`, 1, status.StateOK},
		{`{"primary":{"windowDurationMins":300}}`, 0, status.StateUnavailable},
		{`{"primary":{"usedPercent":null}}`, 0, status.StateUnavailable},
		{`{"primary":{"usedPercent":101}}`, 0, status.StateError},
		{`{"primary":{"usedPercent":-1}}`, 0, status.StateError},
		{`{"primary":{"usedPercent":40,"windowDurationMins":0}}`, 0, status.StateError},
		{`{"primary":{"usedPercent":40,"resetsAt":0}}`, 0, status.StateError},
		{`{}`, 0, status.StateUnavailable},
	} {
		fakeCLI(t, "codex", codexScript(
			`{"account":{"type":"chatgpt"}}`,
			`{"id":3,"result":{"rateLimits":`+tc.limits+`}}`,
		))
		got := (Codex{}).Fetch(context.Background())
		if got.State != tc.state || len(got.Windows) != tc.windows {
			t.Fatalf("limits %s: %+v", tc.limits, got)
		}
	}
}

func TestCodexAuthAndProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		account, limits string
		state           status.State
	}{
		{`{"account":null,"requiresOpenaiAuth":true}`, `{}`, status.StateAuthMissing},
		{`{"account":null}`, `{}`, status.StateAuthMissing},
		{`{"account":null,"requiresOpenaiAuth":false}`, `{}`, status.StateUnavailable},
		{`{"account":{"type":"amazonBedrock","credentialSource":"awsManaged"},"requiresOpenaiAuth":false}`, `{}`, status.StateUnavailable},
		{`{"account":{"type":"apiKey"}}`, `{}`, status.StateUnavailable},
		{`{"account":{"type":"chatgpt"}}`, `{"id":3,"error":{"code":-32601,"message":"private-token"}}`, status.StateUnsupported},
		{`{"account":{"type":"chatgpt"}}`, `{"id":3,"error":{"code":-32000,"message":"private-token"}}`, status.StateError},
		{`{"account":{"type":"chatgpt"}}`, `not-json-private-token`, status.StateError},
		{`{"account":{"type":"chatgpt"}}`, `{"id":99,"result":{}}`, status.StateError},
	} {
		fakeCLI(t, "codex", codexScript(tc.account, tc.limits))
		got := (Codex{}).Fetch(context.Background())
		if got.State != tc.state || len(got.Windows) != 0 || strings.Contains(got.Note, "private-token") {
			t.Fatalf("got %+v, want %v", got, tc.state)
		}
	}
}

func TestCodexMissingAndCancellation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got := (Codex{}).Fetch(context.Background()); got.State != status.StateNotInstalled {
		t.Fatalf("got %+v", got)
	}
	fakeCLI(t, "codex", "while IFS= read -r line; do :; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := (Codex{}).Fetch(ctx); got.State != status.StateError || len(got.Windows) != 0 {
		t.Fatalf("got %+v", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("cancellation did not terminate command")
	}
}
