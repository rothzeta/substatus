package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

const (
	agyUsageTimeout = 25 * time.Second
	agyOutputLimit  = 1 << 20
)

// Gemini obtains quota through Antigravity CLI's documented /usage command.
// It does not read the CLI's credential files or call Google endpoints itself.
type Gemini struct{}

func (Gemini) Name() string { return "Gemini (agy)" }

func (Gemini) Fetch(ctx context.Context) Result {
	res := Result{
		Source:  "agy --print /usage --output-format json",
		Quality: QualityCLI,
		Note:    "Antigravity quotas from /usage; Claude/GPT buckets are not Claude or Codex subscription quotas",
	}
	binary, err := exec.LookPath("agy")
	if err != nil {
		res.State = ResNotInstalled
		res.Note = "`agy` CLI not found on PATH"
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, agyUsageTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--print", "/usage", "--output-format", "json")
	cmd.Env = envWithoutSecrets(os.Environ())
	cmd.WaitDelay = time.Second
	var stdout cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		res.State = ResError
		if ctx.Err() != nil {
			res.Note = "agy /usage command timed out or was cancelled"
		} else if errors.Is(err, errOutputLimit) {
			res.Note = "agy /usage output exceeded the 1 MiB safety limit"
		} else {
			res.Note = "agy /usage command failed; check the CLI's sign-in and version"
		}
		return res
	}

	var response agyUsageResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		res.State = ResError
		res.Note = "could not parse agy /usage JSON output"
		return res
	}
	if !strings.EqualFold(response.Status, "SUCCESS") || !strings.EqualFold(response.Command.Name, "usage") {
		res.State = ResError
		res.Note = "agy did not return a successful /usage result"
		return res
	}

	res.State = ResOK
	for _, group := range response.Command.Data.Groups {
		groupName := cleanCLIText(group.Name)
		for _, bucket := range group.Buckets {
			if bucket.Disabled || bucket.RemainingFraction == nil {
				continue
			}
			remaining := *bucket.RemainingFraction
			if math.IsNaN(remaining) || math.IsInf(remaining, 0) || remaining < 0 || remaining > 1 {
				res.State, res.Windows = ResError, nil
				res.Note = "agy /usage returned a remaining fraction outside 0..1"
				return res
			}
			label := groupName
			if label == "" {
				label = cleanCLIText(bucket.Name)
			}
			window := cleanCLIText(bucket.Window)
			if window != "" {
				if label != "" {
					label += " "
				}
				label += window
			}
			if label == "" {
				label = "quota"
			}
			w := ResultWindow{Label: label, Percent: (1 - remaining) * 100}
			if bucket.ResetTime != "" {
				t, err := time.Parse(time.RFC3339Nano, bucket.ResetTime)
				if err != nil {
					res.State, res.Windows = ResError, nil
					res.Note = "agy /usage returned an invalid quota reset time"
					return res
				}
				w.ResetsAt, w.HasReset = t, true
			}
			res.Windows = append(res.Windows, w)
		}
	}
	if len(res.Windows) == 0 {
		res.State = ResUnavailable
		res.Note = "agy /usage returned no active quota buckets; missing or disabled buckets have unknown usage"
	}
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

var errOutputLimit = errors.New("command output exceeds limit")

type cappedBuffer struct {
	bytes.Buffer
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > agyOutputLimit {
		return 0, errOutputLimit
	}
	return b.Buffer.Write(p)
}

func envWithoutSecrets(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(key)
		if key == "OPENCODE_API_KEY" || strings.HasSuffix(key, "_API_KEY") ||
			strings.Contains(key, "OAUTH_TOKEN") || strings.HasSuffix(key, "_ACCESS_TOKEN") ||
			strings.HasSuffix(key, "_CLIENT_SECRET") || strings.HasSuffix(key, "_PASSWORD") {
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
