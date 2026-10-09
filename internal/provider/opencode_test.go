package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

func TestOpenCodeFetchesGoUsageFromFirstPartyAPI(t *testing.T) {
	const key = "test-opencode-key"
	t.Setenv("OPENCODE_API_KEY", key)
	reset := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/zen/go/v1/usage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+key {
			t.Errorf("authorization = %q", got)
		}
		fmt.Fprint(w, `{"usage":{"rolling":{"status":"ok","percent":17.5,"resetsAt":"2030-01-01T12:00:00Z"},"weekly":{"status":"ok","percent":40,"resetsAt":"2030-01-07T00:00:00Z"},"monthly":{"status":"rate-limited","percent":100,"resetsAt":"2030-02-01T00:00:00Z"}}}`)
	}))
	defer server.Close()

	got := (OpenCode{baseURL: server.URL}).Fetch(context.Background())
	if got.State != status.StateOK {
		t.Fatalf("state = %v, note = %q", got.State, got.Note)
	}
	if !strings.Contains(got.Source, opencodeUsagePath) || got.Quality != status.QualityPrivate {
		t.Errorf("source/quality = %q/%v", got.Source, got.Quality)
	}
	if len(got.Windows) != 3 {
		t.Fatalf("windows = %d, want 3: %+v", len(got.Windows), got.Windows)
	}
	if got.Windows[0].Label != "rolling" || got.Windows[0].Percent != 17.5 || got.Windows[0].ResetsAt.IsZero() || !got.Windows[0].ResetsAt.Equal(reset) {
		t.Errorf("rolling window = %+v", got.Windows[0])
	}
	if got.Windows[2].Percent != 100 {
		t.Errorf("monthly percent = %v, want 100", got.Windows[2].Percent)
	}
}

func TestOpenCodeWithoutAPIKeyDoesNotMakeRequest(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()

	got := (OpenCode{baseURL: server.URL}).Fetch(context.Background())
	if called {
		t.Fatal("made request without an API key")
	}
	if got.State != status.StateAuthMissing || got.Note == "" {
		t.Errorf("result = %+v, want auth-missing with guidance", got)
	}
}

func TestOpenCodeUnauthorizedDoesNotExposeResponseBody(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "private-key-value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"private-key-value is invalid"}}`)
	}))
	defer server.Close()

	got := (OpenCode{baseURL: server.URL}).Fetch(context.Background())
	if got.State != status.StateAuthMissing {
		t.Fatalf("state = %v, want auth missing", got.State)
	}
	if got.Note == "" {
		t.Fatalf("want a safe note, got %q", got.Note)
	}
	if strings.Contains(got.Note, "private-key-value") || strings.Contains(got.Note, "invalid") {
		t.Errorf("note leaked response body: %q", got.Note)
	}
}

func TestOpenCodeWithoutGoEntitlementIsUnsupported(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"message":"OpenCode Go subscription required"}}`)
	}))
	defer server.Close()

	got := (OpenCode{baseURL: server.URL}).Fetch(context.Background())
	if got.State != status.StateUnsupported {
		t.Fatalf("state = %v, want unsupported", got.State)
	}
	if got.Note == "" {
		t.Fatalf("want a safe note, got %q", got.Note)
	}
}

func TestOpenCodeMalformedUsageIsError(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{`) }))
	defer server.Close()

	got := (OpenCode{baseURL: server.URL}).Fetch(context.Background())
	if got.State != status.StateError || got.Note == "" {
		t.Fatalf("result = %+v, want decode error", got)
	}
}
