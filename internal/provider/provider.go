// Package provider implements per-provider usage sources.
//
// Every provider is read-only and returns a status.Provider without its Name,
// which the runner fills in. Codex uses its documented app-server protocol,
// Claude and Gemini their CLIs' /usage commands, and OpenCode its first-party
// API key.
package provider

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Version identifies substatus to provider protocols that ask for a client version.
const Version = "0.1.0"

// maxBody bounds every response body or CLI output we read.
const maxBody = 1 << 20 // 1 MiB

// cliTimeout bounds a single provider CLI invocation.
const cliTimeout = 25 * time.Second

var (
	errNotInstalled = errors.New("CLI not found on PATH")
	errOutputLimit  = errors.New("command output exceeds limit")
)

// validPercent reports whether p is a finite percentage in [0, max].
func validPercent(p, max float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= max
}

// runCLI runs a provider CLI with a bounded deadline and output, a scrubbed
// environment that keeps only the named credential variables, and a neutral
// working directory so no project configuration is picked up. Stderr is
// discarded: it is never rendered.
func runCLI(ctx context.Context, name string, keepEnv []string, args ...string) ([]byte, error) {
	binary, err := exec.LookPath(name)
	if err != nil {
		return nil, errNotInstalled
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = envWithoutSecrets(os.Environ(), keepEnv...)
	cmd.Dir = os.TempDir()
	cmd.WaitDelay = time.Second
	var stdout cappedBuffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

type cappedBuffer struct {
	bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxBody {
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}

// envWithoutSecrets drops credential-looking variables, except those named in
// keep, so one provider's secrets never reach another provider's CLI.
func envWithoutSecrets(env []string, keep ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		secret := strings.HasSuffix(upper, "_API_KEY") || strings.Contains(upper, "OAUTH_TOKEN") ||
			strings.HasSuffix(upper, "_ACCESS_TOKEN") || strings.HasSuffix(upper, "_CLIENT_SECRET") ||
			strings.HasSuffix(upper, "_PASSWORD")
		if secret && !slices.Contains(keep, key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func cleanCLIText(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
}
