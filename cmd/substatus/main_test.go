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

func TestTooShortRefreshRejected(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--refresh", "1s", "--once"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "at least") {
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
	isolateConfig(t)
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
	path, ok := strings.CutPrefix(strings.TrimSpace(out.String()), "saved OpenCode API key to ")
	if !ok {
		t.Fatalf("stdout = %q; want the saved key path", out.String())
	}
	if dir, _ := os.UserConfigDir(); path != filepath.Join(dir, "substatus", "opencode_api_key") {
		t.Fatalf("saved key path = %q; want it under the user config directory %q", path, dir)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "test-key-123\n" {
		t.Fatalf("saved key = %q, %v", data, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v", fi.Mode().Perm())
	}
}

func TestSetOpenCodeKeyRejectsEmptyInput(t *testing.T) {
	isolateConfig(t)
	f, err := os.Create(filepath.Join(t.TempDir(), "empty"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out, errBuf bytes.Buffer
	if code := run([]string{"--set-opencode-key"}, f, &out, &errBuf); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(errBuf.String(), "single non-empty token") {
		t.Fatalf("stderr = %q; want the empty-key validation error", errBuf.String())
	}
}

func TestInteractiveModeNeedsTerminal(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "in"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out, errBuf bytes.Buffer
	if code := run(nil, f, &out, &errBuf); code != 2 || !strings.Contains(errBuf.String(), "--once") {
		t.Fatalf("exit = %d, stderr = %q", code, errBuf.String())
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %q to a non-terminal", out.String())
	}
}

// isolateConfig points the user config directory at a temp dir on every OS
// (os.UserConfigDir uses XDG_CONFIG_HOME on Linux, HOME on macOS).
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
}
