package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHelpExitsZeroAndListsProviders(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--help"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	text := out.String() + errBuf.String()
	for _, want := range []string{"-refresh", "-once", "Codex", "Claude", "Gemini", "OpenCode"} {
		if !strings.Contains(text, want) {
			t.Errorf("help missing %q\n%s", want, text)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--version"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "substatus") {
		t.Errorf("version output = %q", out.String())
	}
}

func TestNonPositiveRefreshRejected(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--refresh", "0s", "--once"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "positive") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

func TestUnknownFlagExitsTwo(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--nope"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// TestOnceOutputInSanitizedEnvironment runs --once with a sanitized HOME (no
// credentials, no CLIs) and verifies all four providers appear, no credential
// material is printed, and the process does not crash.
func TestOnceOutputInSanitizedEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NO_COLOR", "1")
	t.Setenv("PATH", "/nonexistent")

	var out, errBuf bytes.Buffer
	if code := run([]string{"--once"}, nil, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr=%q", code, errBuf.String())
	}
	text := out.String()
	for _, name := range []string{"Codex", "Claude", "Gemini", "OpenCode"} {
		if !strings.Contains(text, name) {
			t.Errorf("output missing provider %q\n%s", name, text)
		}
	}
	// In a sanitized environment every provider must report a non-OK state
	// rather than fabricating a quota.
	if strings.Contains(text, "● ok") {
		t.Errorf("fabricated an OK provider in a credential-free environment:\n%s", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Errorf("NO_COLOR set but output contains ANSI escapes")
	}
	if strings.Contains(text, "Bearer ") || strings.Contains(text, "sk-") {
		t.Errorf("output appears to contain credential material:\n%s", text)
	}
}

func TestSetOpenCodeKeyFromPipeIsSavedPrivately(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	in := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(in, []byte("  test-key-123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(in)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var out, errBuf bytes.Buffer
	if code := run([]string{"--set-opencode-key"}, f, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if strings.Contains(out.String()+errBuf.String(), "test-key-123") {
		t.Fatal("key echoed to output")
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "substatus", "opencode_api_key")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "test-key-123\n" {
		t.Fatalf("saved key = %q, %v", data, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v", fi.Mode().Perm())
	}
}

func TestSetOpenCodeKeyRejectsEmptyInput(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out, errBuf bytes.Buffer
	if code := run([]string{"--set-opencode-key"}, f, &out, &errBuf); code != 1 {
		t.Fatalf("exit = %d", code)
	}
}
