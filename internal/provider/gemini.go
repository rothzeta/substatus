package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/local/substatus/internal/status"
)

// Gemini obtains quota through Antigravity CLI's documented /usage command.
// It does not read the CLI's credential files or call Google endpoints itself.
type Gemini struct{}

func (Gemini) Name() string { return "Gemini (agy)" }

func (Gemini) Fetch(ctx context.Context) status.Provider {
	res := status.Provider{
		Source:  "agy --print /usage --output-format json",
		Quality: status.QualityCLI,
	}
	out, err := runCLI(ctx, "agy", nil, "--print", "/usage", "--output-format", "json")
	switch {
	case errors.Is(err, errNotInstalled):
		res.State, res.Note = status.StateNotInstalled, "`agy` CLI not found on PATH"
		return res
	case errors.Is(err, errOutputLimit):
		res.State, res.Note = status.StateError, "agy /usage output exceeded the 1 MiB safety limit"
		return res
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		res.State, res.Note = status.StateError, "agy /usage command timed out or was cancelled"
		return res
	case err != nil:
		res.State, res.Note = status.StateError, "agy /usage command failed; check the CLI's sign-in and version"
		return res
	}

	var response agyUsageResponse
	if err := json.Unmarshal(out, &response); err != nil {
		res.State, res.Note = status.StateError, "could not parse agy /usage JSON output"
		return res
	}
	if !strings.EqualFold(response.Status, "SUCCESS") || !strings.EqualFold(response.Command.Name, "usage") {
		res.State, res.Note = status.StateError, "agy did not return a successful /usage result"
		return res
	}
	windows, err := response.windows()
	if err != nil {
		res.State, res.Note = status.StateError, err.Error()
		return res
	}
	if len(windows) == 0 {
		res.State = status.StateUnavailable
		res.Note = "agy /usage returned no active quota buckets; missing or disabled buckets have unknown usage"
		return res
	}
	res.State, res.Windows = status.StateOK, windows
	res.Note = "Antigravity quotas from /usage; Claude/GPT buckets are not Claude or Codex subscription quotas"
	return res
}

type agyUsageResponse struct {
	Status  string `json:"status"`
	Command struct {
		Name string `json:"name"`
		Data struct {
			Groups []struct {
				Name    string `json:"name"`
				Buckets []struct {
					Name              string   `json:"name"`
					Window            string   `json:"window"`
					RemainingFraction *float64 `json:"remaining_fraction"`
					ResetTime         string   `json:"reset_time"`
					Disabled          bool     `json:"disabled"`
				} `json:"buckets"`
			} `json:"groups"`
		} `json:"data"`
	} `json:"command"`
}

func (r agyUsageResponse) windows() ([]status.Window, error) {
	var out []status.Window
	for _, group := range r.Command.Data.Groups {
		groupName := cleanCLIText(group.Name)
		for _, bucket := range group.Buckets {
			if bucket.Disabled || bucket.RemainingFraction == nil {
				continue
			}
			remaining := *bucket.RemainingFraction
			if !validPercent(remaining, 1) {
				return nil, errors.New("agy /usage returned a remaining fraction outside 0..1")
			}
			label := groupName
			if label == "" {
				label = cleanCLIText(bucket.Name)
			}
			if window := cleanCLIText(bucket.Window); window != "" {
				label = strings.TrimSpace(label + " " + window)
			}
			if label == "" {
				label = "quota"
			}
			w := status.Window{Label: label, Percent: (1 - remaining) * 100}
			if bucket.ResetTime != "" {
				t, err := time.Parse(time.RFC3339Nano, bucket.ResetTime)
				if err != nil {
					return nil, errors.New("agy /usage returned an invalid quota reset time")
				}
				w.ResetsAt = t
			}
			out = append(out, w)
		}
	}
	return out, nil
}
