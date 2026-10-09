package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"time"
)

// Codex delegates authentication and quota retrieval to the provider's CLI.
// Only documented app-server methods are used; this app never reads auth files,
// exports tokens, sends prompts, or calls Codex backend endpoints.
type Codex struct{}

func (Codex) Name() string { return "Codex" }

func (Codex) Fetch(ctx context.Context) Result {
	res := Result{
		Source:  "codex app-server: account/rateLimits/read",
		Quality: QualityCLI,
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		res.State = ResNotInstalled
		res.Note = "`codex` CLI not found on PATH"
		return res
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server")
	cmd.Env = envWithoutSecrets(os.Environ())
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexFailure(res, ctx, err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexFailure(res, ctx, err)
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return codexFailure(res, ctx, err)
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
	rpc := codexRPC{encoder: json.NewEncoder(stdin), scanner: scanner}
	var initialized struct{}
	err = rpc.call(1, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name": "substatus", "title": "Subscription status", "version": "0.1.0",
		},
	}, &initialized)
	if err != nil {
		return codexFailure(res, ctx, err)
	}
	if err := rpc.encoder.Encode(map[string]any{
		"method": "initialized", "params": map[string]any{},
	}); err != nil {
		return codexFailure(res, ctx, err)
	}
	var account struct {
		Account *struct {
			Type string `json:"type"`
			Plan string `json:"planType"`
		} `json:"account"`
		RequiresOpenaiAuth *bool `json:"requiresOpenaiAuth"`
	}
	if err := rpc.call(2, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return codexFailure(res, ctx, err)
	}
	if account.Account == nil && account.RequiresOpenaiAuth != nil && !*account.RequiresOpenaiAuth {
		res.State = ResUnavailable
		res.Note = "the active Codex model provider does not use OpenAI sign-in; no ChatGPT subscription quota applies"
		return res
	}
	if account.Account == nil {
		res.State = ResAuthMissing
		res.Note = "sign in using the official Codex CLI; this app never starts a login"
		return res
	}
	if account.Account.Type == "apiKey" || account.Account.Type == "amazonBedrock" {
		res.State = ResUnavailable
		res.Note = "Codex subscription quotas require ChatGPT-backed sign-in; API billing is separate"
		return res
	}
	res.Plan = cleanCLIText(account.Account.Plan)
	// The published request has no params; send exactly that shape.
	var limits codexLimits
	if err := rpc.call(3, "account/rateLimits/read", nil, &limits); err != nil {
		return codexFailure(res, ctx, err)
	}
	windows, err := limits.windows()
	if err != nil {
		return codexFailure(res, ctx, err)
	}
	res.Windows = windows
	res.State = ResOK
	res.Note = "current usage from the documented Codex app-server interface"
	if len(windows) == 0 {
		res.State = ResUnavailable
		res.Note = "Codex returned no quota percentages; check /status in the official CLI"
	}
	return res
}

type codexRPC struct {
	encoder *json.Encoder
	scanner *bufio.Scanner
}

type codexRPCError struct {
	Code int `json:"code"`
}

func (e *codexRPCError) Error() string { return "Codex status request failed" }

func (r *codexRPC) call(id int, method string, params any, out any) error {
	if err := r.encoder.Encode(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params}); err != nil {
		return err
	}
	for r.scanner.Scan() {
		var response struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *codexRPCError  `json:"error"`
		}
		if err := json.Unmarshal(r.scanner.Bytes(), &response); err != nil {
			return errors.New("invalid Codex JSON")
		}
		if response.ID == nil || response.Method != "" {
			continue // notifications and server requests never complete a request
		}
		if *response.ID != id {
			return errors.New("unexpected Codex response ID")
		}
		if response.Error != nil {
			return response.Error
		}
		if len(response.Result) == 0 || string(response.Result) == "null" {
			return errors.New("missing Codex result")
		}
		return json.Unmarshal(response.Result, out)
	}
	if err := r.scanner.Err(); err != nil {
		return err
	}
	return errors.New("Codex closed its status stream")
}

func codexFailure(res Result, ctx context.Context, err error) Result {
	res.State, res.Windows = ResError, nil
	res.Note = "Codex status retrieval failed; check the CLI's sign-in, version, and connectivity"
	var rpcErr *codexRPCError
	if ctx.Err() != nil {
		res.Note = "Codex status command timed out or was cancelled"
	} else if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
		res.State = ResUnsupported
		res.Note = "this Codex CLI version lacks the documented status method; update it or use /status manually"
	}
	return res // never render subprocess output, account identity or RPC errors
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

func (l codexLimits) windows() ([]ResultWindow, error) {
	buckets := l.ByID
	if len(buckets) == 0 {
		buckets = map[string]*codexLimit{"codex": l.RateLimits}
	}
	ids := make([]string, 0, len(buckets))
	for id := range buckets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []ResultWindow
	for _, id := range ids {
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
			if math.IsNaN(*w.Used) || math.IsInf(*w.Used, 0) || *w.Used < 0 || *w.Used > 100 {
				return nil, errors.New("invalid Codex percentage")
			}
			window := "primary"
			if i == 1 {
				window = "secondary"
			}
			if w.Minutes != nil {
				n := *w.Minutes
				if n <= 0 {
					return nil, errors.New("invalid Codex duration")
				}
				switch {
				case n%1440 == 0:
					window = fmt.Sprintf("%dd", n/1440)
				case n%60 == 0:
					window = fmt.Sprintf("%dh", n/60)
				default:
					window = fmt.Sprintf("%dm", n)
				}
			}
			item := ResultWindow{Label: label + " " + window, Percent: *w.Used}
			if w.Reset != nil {
				if *w.Reset <= 0 {
					return nil, errors.New("invalid Codex reset")
				}
				item.ResetsAt, item.HasReset = time.Unix(*w.Reset, 0), true
			}
			out = append(out, item)
		}
	}
	return out, nil
}
