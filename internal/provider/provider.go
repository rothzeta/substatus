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

	"github.com/rothzeta/substatus/internal/status"
)

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
// environment that keeps only the named credential variables, and a private
// empty working directory, so no project configuration (such as hooks
// planted in a shared directory) is picked up. Stderr is discarded: it is
// never rendered.
func runCLI(ctx context.Context, name string, keepEnv []string, args ...string) ([]byte, error) {
	ctx, cmd, cleanup, err := newCLICommand(ctx, name, keepEnv, args...)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	stdout := &cappedWriter{limit: maxBody}
	cmd.Stdout = stdout
	err = cmd.Run()
	switch {
	case stdout.exceeded: // checked first: the killed child's exit error hides it
		return nil, errOutputLimit
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case err != nil:
		return nil, err
	}
	return stdout.buf.Bytes(), nil
}

// newCLICommand prepares a provider CLI command with the safety bounds
// runCLI documents. It returns the timeout context the command runs under and
// a cleanup that cancels it and removes the working directory.
func newCLICommand(ctx context.Context, name string, keepEnv []string, args ...string) (context.Context, *exec.Cmd, func(), error) {
	binary, err := exec.LookPath(name)
	if err != nil {
		return ctx, nil, nil, errNotInstalled
	}
	dir, err := os.MkdirTemp("", "substatus-")
	if err != nil {
		return ctx, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = envWithoutSecrets(os.Environ(), keepEnv...)
	cmd.Dir = dir
	cmd.WaitDelay = time.Second
	return ctx, cmd, func() { cancel(); os.RemoveAll(dir) }, nil
}

// fail returns res as a failure of the given state with note and no windows.
func fail(res status.Provider, state status.State, note string) status.Provider {
	res.State, res.Note, res.Windows = state, note, nil
	return res
}

// cliFailure maps a runCLI error to a state and note for the tool's /usage
// command; hint is appended to the generic failure.
func cliFailure(ctx context.Context, res status.Provider, tool, hint string, err error) status.Provider {
	switch {
	case errors.Is(err, errNotInstalled):
		return fail(res, status.StateNotInstalled, "`"+tool+"` CLI not found on PATH")
	case errors.Is(err, errOutputLimit):
		return fail(res, status.StateError, tool+" /usage output exceeded the 1 MiB safety limit")
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		return fail(res, status.StateError, tool+" /usage command timed out or was cancelled")
	}
	return fail(res, status.StateError, tool+" /usage command failed; "+hint)
}

// cappedWriter keeps at most limit bytes. It deliberately does not embed
// bytes.Buffer: a promoted ReadFrom would let io.Copy bypass Write.
type cappedWriter struct {
	buf      bytes.Buffer
	limit    int
	exceeded bool
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		w.exceeded = true
		return 0, errOutputLimit
	}
	return w.buf.Write(p)
}

// envWithoutSecrets drops credential-looking variables (API keys, tokens,
// secrets, passwords, cloud credentials), except those named in keep, so one
// provider's secrets never reach another provider's CLI. It is a best-effort
// denylist, not a guarantee.
func envWithoutSecrets(env []string, keep ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		secret := strings.HasSuffix(upper, "_KEY") || strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") ||
			strings.HasPrefix(upper, "AWS_")
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
