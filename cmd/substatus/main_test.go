package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpExitsZeroAndListsProviders(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--help"}, &out, &errBuf); code != 0 {
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
	if code := run([]string{"--version"}, &out, &errBuf); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "substatus") {
		t.Errorf("version output = %q", out.String())
	}
}

func TestNonPositiveRefreshRejected(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--refresh", "0s", "--once"}, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "positive") {
		t.Errorf("stderr = %q", errBuf.String())
	}
}

func TestUnknownFlagExitsTwo(t *testing.T) {
	var out, errBuf bytes.Buffer
	if code := run([]string{"--nope"}, &out, &errBuf); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// TestOnceOutputInSanitizedEnvironment runs --once with a sanitized HOME (no
// credentials, no CLIs) and verifies all four providers appear, no credential
// material is printed, and the process does not crash.
func TestOnceOutputInSanitizedEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("PATH", "/nonexistent")

	var out, errBuf bytes.Buffer
	if code := run([]string{"--once"}, &out, &errBuf); code != 0 {
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
