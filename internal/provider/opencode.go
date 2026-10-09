package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
// Zen API with OPENCODE_API_KEY. The endpoint is present in OpenCode's server
// source, but is not covered by a stable public API contract.
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
	key := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY"))
	if key == "" {
		res.State, res.Note = status.StateAuthMissing, "set OPENCODE_API_KEY to query OpenCode Go subscription usage"
		return res
	}
	body, code, err := o.get(ctx, key)
	if err != nil {
		res.State, res.Note = status.StateError, "OpenCode usage request failed; check connectivity"
		return res
	}
	switch code {
	case http.StatusOK:
	case http.StatusUnauthorized:
		res.State, res.Note = status.StateAuthMissing, "OpenCode rejected OPENCODE_API_KEY (HTTP 401)"
		return res
	case http.StatusForbidden:
		res.State = status.StateUnsupported
		res.Note = "OpenCode Go usage is unavailable for this API key (HTTP 403); a Go subscription may be required"
		return res
	default:
		res.State, res.Note = status.StateError, fmt.Sprintf("OpenCode usage endpoint returned HTTP %d", code)
		return res
	}

	var payload struct {
		Usage struct {
			Rolling opencodeUsageWindow `json:"rolling"`
			Weekly  opencodeUsageWindow `json:"weekly"`
			Monthly opencodeUsageWindow `json:"monthly"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		res.State, res.Note = status.StateError, "could not parse OpenCode usage response"
		return res
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
			res.State, res.Windows = status.StateError, nil
			res.Note = "OpenCode " + item.label + " usage percent is outside 0..100"
			return res
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
		res.State, res.Note = status.StateError, "OpenCode usage response contained no quota windows"
		return res
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
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	return body, resp.StatusCode, err
}
