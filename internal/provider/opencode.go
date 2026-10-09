package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rothzeta/substatus/internal/status"
)

const (
	opencodeOrigin    = "https://opencode.ai"
	opencodeUsagePath = "/zen/go/v1/usage"
	httpTimeout       = 10 * time.Second
)

// OpenCode reads Go subscription usage directly from OpenCode's first-party
// Zen API with OPENCODE_API_KEY, or else the key saved by SaveOpenCodeKey.
// The endpoint is present in OpenCode's server source, but is not covered by
// a stable public API contract.
type OpenCode struct {
	baseURL string // tests only; empty means opencodeOrigin
}

func (OpenCode) Name() string { return "OpenCode" }

type opencodeUsageWindow struct {
	Status   string     `json:"status"`
	Percent  *float64   `json:"percent"`
	ResetsAt *time.Time `json:"resetsAt"`
}

func (o OpenCode) Fetch(ctx context.Context) status.Provider {
	res := status.Provider{
		Source:  "OpenCode Go usage API (" + opencodeUsagePath + ")",
		Quality: status.QualityPrivate,
	}
	key := opencodeKey()
	if key == "" {
		return fail(res, status.StateAuthMissing,
			"run `substatus --set-opencode-key` or set OPENCODE_API_KEY to query OpenCode Go usage")
	}
	body, code, err := o.get(ctx, key)
	if err != nil {
		return fail(res, status.StateError, "OpenCode usage request failed; check connectivity")
	}
	switch code {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return fail(res, status.StateAuthMissing, "OpenCode rejected the API key (HTTP 401)")
	case http.StatusForbidden:
		return fail(res, status.StateUnsupported,
			"OpenCode Go usage is unavailable for this API key (HTTP 403); a Go subscription may be required")
	default:
		return fail(res, status.StateError, fmt.Sprintf("OpenCode usage endpoint returned HTTP %d", code))
	}

	var payload struct {
		Usage struct {
			Rolling opencodeUsageWindow `json:"rolling"`
			Weekly  opencodeUsageWindow `json:"weekly"`
			Monthly opencodeUsageWindow `json:"monthly"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fail(res, status.StateError, "could not parse OpenCode usage response")
	}
	var limited []string
	for _, item := range []struct {
		label  string
		window opencodeUsageWindow
	}{
		{"rolling", payload.Usage.Rolling},
		{"weekly", payload.Usage.Weekly},
		{"monthly", payload.Usage.Monthly},
	} {
		if item.window.Percent == nil {
			continue
		}
		if !validPercent(*item.window.Percent, 100) {
			return fail(res, status.StateError, "OpenCode "+item.label+" usage percent is outside 0..100")
		}
		w := status.Window{Label: item.label, Percent: *item.window.Percent}
		if item.window.ResetsAt != nil {
			w.ResetsAt = *item.window.ResetsAt
		}
		res.Windows = append(res.Windows, w)
		if item.window.Status == "rate-limited" {
			limited = append(limited, item.label)
		}
	}
	if len(res.Windows) == 0 {
		return fail(res, status.StateError, "OpenCode usage response contained no quota windows")
	}
	res.State, res.Plan = status.StateOK, "Go"
	res.Note = "first-party endpoint; Go subscription usage only (Zen credit balance is not exposed)"
	if len(limited) > 0 {
		res.Note += "; rate-limited: " + strings.Join(limited, ", ")
	}
	return res
}

// get fetches the usage document. Transport errors are not rendered, so the
// key in the request header can never leak through them.
func (o OpenCode) get(ctx context.Context, key string) ([]byte, int, error) {
	base := o.baseURL
	if base == "" {
		base = opencodeOrigin
	}
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+opencodeUsagePath, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	// Never follow redirects: they could carry the bearer key elsewhere.
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return body, resp.StatusCode, err
}

// opencodeKeyPath is where SaveOpenCodeKey stores the key: substatus/opencode_api_key
// under the user config directory ($XDG_CONFIG_HOME or ~/.config on Linux).
func opencodeKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(dir, "substatus", "opencode_api_key"), nil
}

// SaveOpenCodeKey atomically writes key with mode 0600 and returns the file's
// path.
func SaveOpenCodeKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.ContainsAny(key, " \t\r\n") {
		return "", errors.New("API key must be a single non-empty token")
	}
	path, err := opencodeKeyPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".opencode_api_key-*") // mode 0600
	if err != nil {
		return "", fmt.Errorf("create key file: %w", err)
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(key + "\n")
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("write key file: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return "", fmt.Errorf("replace key file: %w", err)
	}
	return path, nil
}

// opencodeKey returns OPENCODE_API_KEY, or else the saved key, or "".
func opencodeKey() string {
	if key := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY")); key != "" {
		return key
	}
	path, err := opencodeKeyPath()
	if err != nil {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	line, _, _ := bufio.NewReaderSize(io.LimitReader(f, 4096), 4096).ReadLine()
	return strings.TrimSpace(string(line))
}
