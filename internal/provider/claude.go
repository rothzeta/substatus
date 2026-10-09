package provider

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/local/substatus/internal/status"
)

// Claude runs Claude Code's own /usage command in print mode. Claude Code
// handles /usage locally (no model turn, no quota spent) and authenticates
// with its own credentials; this app never reads them or calls Anthropic.
type Claude struct{}

func (Claude) Name() string { return "Claude" }

func (Claude) Fetch(ctx context.Context) status.Provider {
	res := status.Provider{
		Source:  "claude -p /usage --output-format json",
		Quality: status.QualityCLI,
	}
	// Claude Code's own credential variables stay; other secrets are scrubbed.
	keep := []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}
	out, err := runCLI(ctx, "claude", keep, "-p", "/usage", "--output-format", "json", "--no-session-persistence")
	switch {
	case errors.Is(err, errNotInstalled):
		res.State, res.Note = status.StateNotInstalled, "`claude` CLI not found on PATH"
		return res
	case errors.Is(err, errOutputLimit):
		res.State, res.Note = status.StateError, "claude /usage output exceeded the 1 MiB safety limit"
		return res
	case ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded):
		res.State, res.Note = status.StateError, "claude /usage command timed out or was cancelled"
		return res
	case err != nil:
		res.State, res.Note = status.StateError, "claude /usage command failed; check sign-in with `claude auth`"
		return res
	}

	var result struct {
		Type         string `json:"type"`
		Subtype      string `json:"subtype"`
		IsError      bool   `json:"is_error"`
		NumTurns     int    `json:"num_turns"`
		LocalCommand string `json:"local_command"`
		Result       string `json:"result"`
	}
	if err := json.Unmarshal(out, &result); err != nil || result.Type != "result" {
		res.State, res.Note = status.StateError, "could not parse claude /usage JSON output"
		return res
	}
	// A model turn means this CLI version sent /usage to the model as a prompt.
	if result.LocalCommand != "usage" || result.NumTurns != 0 {
		res.State, res.Note = status.StateUnsupported, "this Claude Code version does not run /usage locally in print mode; update it"
		return res
	}
	if result.IsError || result.Subtype != "success" {
		res.State, res.Note = status.StateError, "claude /usage did not succeed; check sign-in with `claude auth`"
		return res
	}
	windows := parseClaudeUsage(result.Result, time.Now())
	if len(windows) == 0 {
		res.State, res.Note = status.StateUnavailable, "claude /usage reported no subscription quota; API-key billing has none"
		return res
	}
	res.State, res.Windows = status.StateOK, windows
	res.Note = "live usage from Claude Code's /usage command"
	return res
}

// claudeUsageLine matches "Current week (all models): 37% used · resets Oct 14, 10am (UTC)".
var claudeUsageLine = regexp.MustCompile(`^(.+?):\s+(\d+(?:\.\d+)?)%\s+used(?:\s+·\s+resets\s+(.+))?$`)

// parseClaudeUsage extracts quota windows from /usage text. The text is meant
// for people, so a line that does not match is skipped and an unparseable
// reset leaves the reset unknown; neither fabricates a value.
func parseClaudeUsage(text string, now time.Time) []status.Window {
	var out []status.Window
	for _, line := range strings.Split(text, "\n") {
		m := claudeUsageLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		percent, err := strconv.ParseFloat(m[2], 64)
		if err != nil || !validPercent(percent, 100) {
			continue
		}
		label := cleanCLIText(strings.TrimPrefix(m[1], "Current "))
		out = append(out, status.Window{Label: label, Percent: percent, ResetsAt: parseClaudeReset(m[3], now)})
	}
	return out
}

// parseClaudeReset parses "Oct 14, 10am (UTC)" or "1:50pm (Europe/Paris)".
// The year and, for time-only values, the date are the next ones after now.
func parseClaudeReset(s string, now time.Time) time.Time {
	value, zone, ok := strings.Cut(strings.TrimSuffix(strings.TrimSpace(s), ")"), " (")
	if !ok {
		return time.Time{}
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}
	}
	now = now.In(loc)
	for _, layout := range []string{"Jan 2, 2006, 3:04pm", "Jan 2, 2006, 3pm"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t
		}
	}
	for _, layout := range []string{"Jan 2, 3:04pm", "Jan 2, 3pm"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			t = t.AddDate(now.Year()-t.Year(), 0, 0)
			if t.Before(now.Add(-24 * time.Hour)) {
				t = t.AddDate(1, 0, 0)
			}
			return t
		}
	}
	for _, layout := range []string{"3:04pm", "3pm"} {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
			if t.Before(now) {
				t = t.AddDate(0, 0, 1)
			}
			return t
		}
	}
	return time.Time{}
}
