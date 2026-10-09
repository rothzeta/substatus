// Package provider implements per-provider usage sources.
//
// Every provider is read-only. Gemini delegates quota retrieval to Antigravity's
// CLI; OpenCode uses its first-party API key. Codex uses its documented
// app-server protocol; Claude consumes documented status-line output.
package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxBody bounds every response body we read.
const maxBody = 1 << 20 // 1 MiB

// defaultTimeout bounds a single HTTP request.
const defaultTimeout = 10 * time.Second

// Provider reports one provider's status.
type Provider interface {
	Name() string
	Fetch(ctx context.Context) Result
}

// Result is the internal provider return before conversion to status.Provider.
// Kept separate so providers never construct UI concerns.
type Result struct {
	State   ResultState
	Plan    string
	Windows []ResultWindow
	Note    string
	Source  string
	Quality Quality
	Err     error
}

// ResultState mirrors status.State without importing the ui layer.
type ResultState int

const (
	ResOK ResultState = iota
	ResAuthMissing
	ResNotInstalled
	ResUnsupported
	ResError
	// ResUnavailable means the supported source has no current quota data.
	ResUnavailable
)

// Quality mirrors status.SourceQuality.
type Quality int

const (
	QualityOfficial Quality = iota
	QualityPrivate
	QualityCLI
	QualityReverse
)

// ResultWindow is a single usage window.
type ResultWindow struct {
	Label    string
	Percent  float64 // used percent in [0,100]; -1 unknown
	ResetsAt time.Time
	HasReset bool
}

// httpClient is shared across providers; each call uses a bounded context.
var httpClient = &http.Client{Timeout: defaultTimeout}

// doJSON issues a bounded GET/POST with the given bearer token and headers and
// returns the response body. Errors are redacted: they never include request
// headers or credential material.
func doJSON(ctx context.Context, method, url, bearer string, headers map[string]string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, redactError(err)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return buf, resp.StatusCode, nil
}

// redactError strips anything that looks like a bearer token or long opaque
// credential from a transport error string.
func redactError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if strings.Contains(msg, "Authorization") {
		msg = "request failed (transport error)"
	}
	return errors.New(sanitize(msg))
}

// sanitize removes obvious credential material from arbitrary strings so error
// text is safe to render. It collapses any "Bearer <token>" and any JWT-looking
// or long opaque base64url token.
func sanitize(s string) string {
	s = replaceBearer(s)
	return s
}

// replaceBearer rewrites "Bearer <token>" sequences.
func replaceBearer(s string) string {
	const marker = "Bearer "
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(marker)
		b.WriteString("<redacted>")
		j := i + len(marker)
		for j < len(s) && s[j] != ' ' && s[j] != '\n' && s[j] != '"' && s[j] != '\'' {
			j++
		}
		s = s[j:]
	}
}

// trimSlash removes trailing slashes from a base URL.
func trimSlash(s string) string {
	return strings.TrimRight(s, "/")
}
