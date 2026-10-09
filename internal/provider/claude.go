package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"
)

const claudeSnapshotMaxAge = 5 * time.Minute

// Claude reads only substatus's own quota snapshot. Claude Code delivers this
// data through its documented statusLine command interface; no Claude process,
// credential file, transcript, token, or private endpoint is accessed here.
type Claude struct{}

func (Claude) Name() string { return "Claude" }

func ClaudeUsagePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("could not locate substatus cache directory")
	}
	return filepath.Join(dir, "substatus", "claude-usage.json"), nil
}

func (Claude) Fetch(ctx context.Context) Result {
	res := Result{
		Source:  "Claude Code status-line JSON (local snapshot)",
		Quality: QualityCLI,
	}
	if ctx.Err() != nil {
		res.State = ResError
		res.Note = "Claude status check cancelled"
		return res
	}
	path, err := ClaudeUsagePath()
	if err != nil {
		res.State = ResError
		res.Note = err.Error()
		return res
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		res.State = ResUnavailable
		res.Note = "setup required: configure Claude Code's status-line command as substatus --claude-statusline (see README); no credentials are read"
		return res
	}
	if err != nil {
		res.State = ResError
		res.Note = "could not read substatus's Claude quota snapshot"
		return res
	}
	defer file.Close()
	data, err := readBounded(file)
	var snapshot claudeSnapshot
	if err != nil || json.Unmarshal(data, &snapshot) != nil {
		res.State = ResError
		res.Note = "could not parse substatus's Claude quota snapshot"
		return res
	}
	now := time.Now()
	if snapshot.CapturedAt.IsZero() || now.Sub(snapshot.CapturedAt) > claudeSnapshotMaxAge || snapshot.CapturedAt.Sub(now) > time.Minute {
		res.State = ResUnavailable
		res.Note = "Claude status-line snapshot is stale; refresh it from an active Claude Code session"
		return res
	}
	windows, err := snapshot.RateLimits.windows(now)
	if err != nil {
		res.State = ResError
		res.Note = "Claude status-line snapshot contains invalid quota data"
		return res
	}
	res.State, res.Windows = ResOK, windows
	res.Note = "Claude Code status-line snapshot written " + snapshot.CapturedAt.UTC().Format(time.RFC3339) + "; values are from that session's latest API response, not polled by this app"
	if len(windows) == 0 {
		res.State = ResUnavailable
		res.Note = "Claude Code supplied no current rate_limits; fields depend on plan/version and appear after a session response"
	}
	return res
}

type claudeWindow struct {
	Used  *float64 `json:"used_percentage,omitempty"`
	Reset *int64   `json:"resets_at,omitempty"`
}

type claudeRateLimits struct {
	FiveHour *claudeWindow `json:"five_hour,omitempty"`
	SevenDay *claudeWindow `json:"seven_day,omitempty"`
	Spend    *claudeWindow `json:"spend_limit,omitempty"`
}

type claudeSnapshot struct {
	CapturedAt time.Time        `json:"captured_at"`
	RateLimits claudeRateLimits `json:"rate_limits"`
}

func (r claudeRateLimits) windows(now time.Time) ([]ResultWindow, error) {
	var out []ResultWindow
	for _, item := range []struct {
		label  string
		window *claudeWindow
		limit  float64
	}{
		{"5h", r.FiveHour, 100},
		{"7d", r.SevenDay, 100},
		// Documented to exceed 100 once a gateway spend limit is passed.
		{"spend limit", r.Spend, math.Inf(1)},
	} {
		w := item.window
		if w == nil || w.Used == nil {
			continue
		}
		if math.IsNaN(*w.Used) || math.IsInf(*w.Used, 0) || *w.Used < 0 || *w.Used > item.limit {
			return nil, errors.New("invalid Claude percentage")
		}
		window := ResultWindow{Label: item.label, Percent: *w.Used}
		if w.Reset != nil {
			if *w.Reset <= 0 {
				return nil, errors.New("invalid Claude reset")
			}
			window.ResetsAt = time.Unix(*w.Reset, 0)
			if !window.ResetsAt.After(now) {
				continue // never invent a reset or zero usage
			}
			window.HasReset = true
		}
		out = append(out, window)
	}
	return out, nil
}

// SaveClaudeStatusLine accepts provider-owned stdin, retaining only documented
// quota fields and capture time. It never saves session identity or credentials.
// A snapshot with no quotas replaces the old one so vanished fields cannot linger.
func SaveClaudeStatusLine(input io.Reader, path string, now time.Time) error {
	data, err := readBounded(input)
	if err != nil {
		return err
	}
	var payload *struct {
		RateLimits claudeRateLimits `json:"rate_limits"`
	}
	if json.Unmarshal(data, &payload) != nil || payload == nil {
		return errors.New("invalid Claude status-line JSON")
	}
	if _, err := payload.RateLimits.windows(now); err != nil {
		return err
	}
	snapshot := claudeSnapshot{CapturedAt: now.UTC(), RateLimits: payload.RateLimits}
	data, err = json.Marshal(snapshot)
	if err != nil {
		return errors.New("could not encode Claude quota snapshot")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New("could not create substatus cache directory")
	}
	file, err := os.CreateTemp(dir, ".claude-usage-*.json")
	if err != nil {
		return errors.New("could not create Claude quota snapshot")
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return errors.New("could not write Claude quota snapshot")
	}
	if err := file.Close(); err != nil {
		return errors.New("could not close Claude quota snapshot")
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("could not replace Claude quota snapshot")
	}
	return nil
}

// ClaudeStatusLine stores the provider payload and prints a compact local line.
// It is separate from Fetch: invoking this mode never queries other providers.
func ClaudeStatusLine(input io.Reader, output io.Writer) error {
	path, err := ClaudeUsagePath()
	if err != nil {
		return err
	}
	if err := SaveClaudeStatusLine(input, path, time.Now()); err != nil {
		return err
	}
	res := (Claude{}).Fetch(context.Background())
	if res.State == ResError {
		return errors.New("could not read Claude quota snapshot")
	}
	if len(res.Windows) == 0 {
		_, err = fmt.Fprintln(output, "Claude quota unavailable")
		return err
	}
	for i, w := range res.Windows {
		if i > 0 {
			if _, err := fmt.Fprint(output, " | "); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(output, "%s: %g%% used", w.Label, w.Percent); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(output)
	return err
}

func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, errors.New("quota input exceeds limit or cannot be read")
	}
	return data, nil
}
