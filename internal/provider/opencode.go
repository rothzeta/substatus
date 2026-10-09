package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const opencodeUsagePath = "/zen/go/v1/usage"

// OpenCode reads Go subscription usage directly from OpenCode's first-party
// Zen API. The endpoint is present in OpenCode's server source, but is not
// covered by a stable public API contract.
type OpenCode struct {
	// APIKey overrides OPENCODE_API_KEY (tests and explicit embedding only).
	APIKey string
	// BaseURL overrides the first-party origin (tests only).
	BaseURL string
}

func (o OpenCode) Name() string { return "OpenCode" }

func (o OpenCode) apiKey() string {
	if o.APIKey != "" {
		return strings.TrimSpace(o.APIKey)
	}
	return strings.TrimSpace(os.Getenv("OPENCODE_API_KEY"))
}

func (o OpenCode) baseURL() string {
	if o.BaseURL != "" {
		return trimSlash(o.BaseURL)
	}
	return "https://opencode.ai"
}

type opencodeUsageResponse struct {
	Usage struct {
		Rolling opencodeUsageWindow `json:"rolling"`
		Weekly  opencodeUsageWindow `json:"weekly"`
		Monthly opencodeUsageWindow `json:"monthly"`
	} `json:"usage"`
}

type opencodeUsageWindow struct {
	Status   string     `json:"status"`
	Percent  *float64   `json:"percent"`
	ResetsAt *time.Time `json:"resetsAt"`
}

func (o OpenCode) Fetch(ctx context.Context) Result {
	res := Result{
		Source:  "OpenCode Go usage API (" + opencodeUsagePath + ")",
		Quality: QualityPrivate,
		Note:    "first-party endpoint; Go subscription usage only (Zen credit balance is not exposed)",
	}
	key := o.apiKey()
	if key == "" {
		res.State = ResAuthMissing
		res.Note = "set OPENCODE_API_KEY to query OpenCode Go subscription usage"
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	body, status, err := doJSON(ctx, http.MethodGet, o.baseURL()+opencodeUsagePath, key, map[string]string{
		"Accept": "application/json",
	}, nil)
	if err != nil {
		res.State = ResError
		res.Err = err
		return res
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		res.State = ResAuthMissing
		res.Note = "OpenCode rejected OPENCODE_API_KEY (HTTP 401)"
		return res
	case http.StatusForbidden:
		res.State = ResUnsupported
		res.Note = "OpenCode Go usage is unavailable for this API key (HTTP 403); a Go subscription may be required"
		return res
	default:
		res.State = ResError
		res.Err = fmt.Errorf("OpenCode usage endpoint returned HTTP %d", status)
		return res
	}

	var payload opencodeUsageResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		res.State = ResError
		res.Err = fmt.Errorf("decode OpenCode usage: %w", err)
		return res
	}
	res.State = ResOK
	res.Plan = "Go"
	for _, item := range []struct {
		label  string
		window opencodeUsageWindow
	}{
		{label: "rolling", window: payload.Usage.Rolling},
		{label: "weekly", window: payload.Usage.Weekly},
		{label: "monthly", window: payload.Usage.Monthly},
	} {
		if item.window.Percent == nil {
			continue
		}
		percent := *item.window.Percent
		if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
			res.State = ResError
			res.Err = fmt.Errorf("OpenCode %s usage percent is outside 0..100", item.label)
			res.Windows = nil
			return res
		}
		w := ResultWindow{Label: item.label, Percent: percent}
		if item.window.ResetsAt != nil {
			w.ResetsAt = *item.window.ResetsAt
			w.HasReset = true
		}
		res.Windows = append(res.Windows, w)
		if item.window.Status == "rate-limited" {
			res.Note = strings.TrimSuffix(res.Note, " (Zen credit balance is not exposed)")
			res.Note += "; " + item.label + " usage is rate-limited"
		}
	}
	if len(res.Windows) == 0 {
		res.State = ResError
		res.Err = fmt.Errorf("OpenCode usage response contained no quota windows")
	}
	return res
}
