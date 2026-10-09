package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/local/substatus/internal/status"
)

// Codex delegates authentication and quota retrieval to the provider's CLI.
// Only documented app-server methods are used; this app never reads auth files,
// exports tokens, sends prompts, or calls Codex backend endpoints.
type Codex struct{}

func (Codex) Name() string { return "Codex" }

func (Codex) Fetch(ctx context.Context) status.Provider {
	res := status.Provider{
		Source:  "codex app-server: account/rateLimits/read",
		Quality: status.QualityCLI,
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		res.State, res.Note = status.StateNotInstalled, "`codex` CLI not found on PATH"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, cliTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	cmd.Env = envWithoutSecrets(os.Environ())
	cmd.Dir = os.TempDir()
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexFailure(ctx, res, err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexFailure(ctx, res, err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return codexFailure(ctx, res, err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	// Closing pipes unblocks readers if a descendant inherited stdout.
	stopClosing := context.AfterFunc(ctx, func() {
		_ = stdout.Close()
		_ = stdin.Close()
	})
	defer stopClosing()
	scanner := bufio.NewScanner(io.LimitReader(stdout, maxBody+1))
	scanner.Buffer(make([]byte, 4096), maxBody)
	rpc := &codexRPC{encoder: json.NewEncoder(stdin), scanner: scanner}

	var initialized struct{}
	err = rpc.call("initialize", map[string]any{
		"clientInfo": map[string]string{
			"name": "substatus", "title": "Subscription status", "version": Version,
		},
	}, &initialized)
	if err != nil {
		return codexFailure(ctx, res, err)
	}
	if err := rpc.encoder.Encode(map[string]any{
		"method": "initialized", "params": map[string]any{},
	}); err != nil {
		return codexFailure(ctx, res, err)
	}
	var account struct {
		Account *struct {
			Type string `json:"type"`
			Plan string `json:"planType"`
		} `json:"account"`
		RequiresOpenaiAuth *bool `json:"requiresOpenaiAuth"`
	}
	if err := rpc.call("account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return codexFailure(ctx, res, err)
	}
	switch {
	case account.Account == nil && account.RequiresOpenaiAuth != nil && !*account.RequiresOpenaiAuth:
		res.State = status.StateUnavailable
		res.Note = "the active Codex model provider does not use OpenAI sign-in; no ChatGPT subscription quota applies"
		return res
	case account.Account == nil:
		res.State = status.StateAuthMissing
		res.Note = "sign in using the official Codex CLI; this app never starts a login"
		return res
	case account.Account.Type == "apiKey" || account.Account.Type == "amazonBedrock":
		res.State = status.StateUnavailable
		res.Note = "Codex subscription quotas require ChatGPT-backed sign-in; API billing is separate"
		return res
	}
	res.Plan = cleanCLIText(account.Account.Plan)
	// The published request has no params; send exactly that shape.
	var limits codexLimits
	if err := rpc.call("account/rateLimits/read", nil, &limits); err != nil {
		return codexFailure(ctx, res, err)
	}
	windows, err := limits.windows()
	if err != nil {
		return codexFailure(ctx, res, err)
	}
	if len(windows) == 0 {
		res.State = status.StateUnavailable
		res.Note = "Codex returned no quota percentages; check /status in the official CLI"
		return res
	}
	res.State, res.Windows = status.StateOK, windows
	res.Note = "current usage from the documented Codex app-server interface"
	return res
}

// codexRPC is a minimal JSON-RPC client over the app-server's stdio.
type codexRPC struct {
	encoder *json.Encoder
	scanner *bufio.Scanner
	lastID  int
}

type codexRPCError struct {
	Code int `json:"code"`
}

func (e *codexRPCError) Error() string { return fmt.Sprintf("Codex RPC error %d", e.Code) }

func (r *codexRPC) call(method string, params any, out any) error {
	r.lastID++
	id := r.lastID
	if err := r.encoder.Encode(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params}); err != nil {
		return fmt.Errorf("send %s: %w", method, err)
	}
	for r.scanner.Scan() {
		var response struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *codexRPCError  `json:"error"`
		}
		if err := json.Unmarshal(r.scanner.Bytes(), &response); err != nil {
			return fmt.Errorf("%s: invalid JSON: %w", method, err)
		}
		if response.ID == nil || response.Method != "" {
			continue // notifications and server requests never complete a request
		}
		if *response.ID != id {
			return fmt.Errorf("%s: unexpected response ID %d", method, *response.ID)
		}
		if response.Error != nil {
			return fmt.Errorf("%s: %w", method, response.Error)
		}
		if len(response.Result) == 0 || string(response.Result) == "null" {
			return fmt.Errorf("%s: missing result", method)
		}
		return json.Unmarshal(response.Result, out)
	}
	if err := r.scanner.Err(); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return fmt.Errorf("%s: Codex closed its status stream", method)
}

// codexFailure maps an internal error to a safe note: subprocess output,
// account identity and RPC messages are never rendered.
func codexFailure(ctx context.Context, res status.Provider, err error) status.Provider {
	res.State, res.Windows = status.StateError, nil
	res.Note = "Codex status retrieval failed; check the CLI's sign-in, version, and connectivity"
	var rpcErr *codexRPCError
	switch {
	case ctx.Err() != nil:
		res.Note = "Codex status command timed out or was cancelled"
	case errors.As(err, &rpcErr) && rpcErr.Code == -32601:
		res.State = status.StateUnsupported
		res.Note = "this Codex CLI version lacks the documented status method; update it or use /status manually"
	}
	return res
}

type codexLimits struct {
	RateLimits *codexLimit            `json:"rateLimits"`
	ByID       map[string]*codexLimit `json:"rateLimitsByLimitId"`
}

type codexLimit struct {
	Name      string       `json:"limitName"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
}

type codexWindow struct {
	Used    *float64 `json:"usedPercent"`
	Minutes *int64   `json:"windowDurationMins"`
	Reset   *int64   `json:"resetsAt"`
}

func (l codexLimits) windows() ([]status.Window, error) {
	buckets := l.ByID
	if len(buckets) == 0 {
		buckets = map[string]*codexLimit{"codex": l.RateLimits}
	}
	var out []status.Window
	for _, id := range slices.Sorted(maps.Keys(buckets)) {
		bucket := buckets[id]
		if bucket == nil {
			continue
		}
		label := cleanCLIText(bucket.Name)
		if label == "" {
			label = cleanCLIText(id)
		}
		for i, w := range []*codexWindow{bucket.Primary, bucket.Secondary} {
			if w == nil || w.Used == nil {
				continue
			}
			if !validPercent(*w.Used, 100) {
				return nil, errors.New("invalid Codex percentage")
			}
			window := [...]string{"primary", "secondary"}[i]
			if w.Minutes != nil {
				n := *w.Minutes
				switch {
				case n <= 0:
					return nil, errors.New("invalid Codex duration")
				case n%1440 == 0:
					window = fmt.Sprintf("%dd", n/1440)
				case n%60 == 0:
					window = fmt.Sprintf("%dh", n/60)
				default:
					window = fmt.Sprintf("%dm", n)
				}
			}
			item := status.Window{Label: label + " " + window, Percent: *w.Used}
			if w.Reset != nil {
				if *w.Reset <= 0 {
					return nil, errors.New("invalid Codex reset")
				}
				item.ResetsAt = time.Unix(*w.Reset, 0)
			}
			out = append(out, item)
		}
	}
	return out, nil
}
