# substatus

A Go TUI for subscription/quota status across **Codex**, **Claude**,
**Gemini (via `agy`)**, and **OpenCode**. It refreshes on a configurable interval
and displays provider-reported used percentages and reset times.

## Build and run

Requires Go 1.24+; no cgo or external Go dependencies.

```sh
go build -o substatus ./cmd/substatus
./substatus --once --no-color
```

```text
substatus [flags]

  -refresh duration   automatic refresh interval (default 1m0s)
  -once               print one snapshot and exit
  -no-color           disable ANSI color (also honors NO_COLOR)
  -version            print version and exit
  -claude-statusline  capture Claude Code status-line JSON from stdin
```

Interactive keys: `r` refreshes, `q` or Ctrl-C quits. The UI redraws on resize.
`--claude-statusline` is a separate mode: it saves only Claude quota fields and
prints a compact status line; it does not query any other provider.

## Provider sources

| Provider | Interface | Setup and limitations |
|---|---|---|
| **Codex** | `codex app-server`, documented JSON-RPC `account/read` and `account/rateLimits/read` | Install Codex and sign in through its official CLI. Authentication stays inside Codex. Uses stdin/stdout and never sends a model prompt. API-key-only accounts do not expose ChatGPT subscription quotas. |
| **Claude** | Documented Claude Code `statusLine` JSON, specifically `rate_limits` | Configure the bridge below. Receives provider output without running Claude or accessing its credentials. Shows the last snapshot and its capture time, with a five-minute freshness limit. |
| **Gemini (`agy`)** | `agy --print /usage --output-format json` | Install and sign in through Antigravity CLI, version 1.1.11 or later. Parses `command.data.groups[].buckets[]`, `remaining_fraction`, and `reset_time`. These are Antigravity quotas; its Claude/GPT model buckets are not Claude or Codex subscription quotas. |
| **OpenCode Go** | `GET https://opencode.ai/zen/go/v1/usage` | Set `OPENCODE_API_KEY`. Preserves the requested direct API-key integration with no CLI fallback. This first-party, source-derived route has no stable public API contract and reports Go usage windows, not Zen credit balance. |

A missing CLI, missing sign-in, unavailable data, unsupported CLI version, and
failed status request have distinct rows. Missing/null quota fields never become
0% or 100%. Reset times and window durations come from provider output; the app
does not infer quotas from token counts or plan names.

## Claude status-line bridge

Claude Code officially supports sending JSON to a local status-line command on
stdin. Its `rate_limits` fields include `five_hour`, `seven_day`, and, for eligible
Claude apps gateways, `spend_limit`. Windows may be independently absent. The
documentation says subscription fields are available for Pro/Max accounts after
the first API response; other plans or older versions may provide no quotas.

Merge this into your Claude Code settings, using the absolute path of the built
binary. The app does not edit those settings itself. If you already have a status
line, integrate the capture command into your existing script instead of replacing
its output.

```json
{
  "statusLine": {
    "type": "command",
    "command": "/absolute/path/to/substatus --claude-statusline"
  }
}
```

Run Claude Code normally, then open substatus with the same user and cache
environment. The bridge stores only quota percentages, reset timestamps, and
capture time in `substatus/claude-usage.json` under the operating system's user
cache directory (`$XDG_CACHE_HOME`, or `~/.cache`, on Linux). It writes atomically
with file mode 0600. Session identity, transcripts, context usage, and unknown
fields are discarded. A payload without quota fields replaces any old data.

**Freshness.** Claude Code runs the status-line command at session start, when an
assistant message arrives, after `/compact`, on permission/vim mode changes, when
a `rate_limits` window reaches `resets_at`, and every `refreshInterval` seconds if
that optional setting is configured (updates are debounced by 300 ms). The
`rate_limits` values themselves come from the session's most recent API response.
The TUI's "written" time is therefore when Claude Code last ran the bridge, not
when Anthropic last measured usage: a `refreshInterval` or mode toggle rewrites the
snapshot with the same values. Usage from other Claude sessions, devices, or
claude.ai appears only after this session receives another API response.

Snapshots written more than five minutes ago are shown as stale/unavailable, so an
idle session without `refreshInterval` drops out after five minutes. Windows whose
reset has passed are omitted until Claude Code supplies new data; the app never
estimates usage after a reset. With several concurrent Claude Code sessions, the
most recent writer wins. Without setup, the Claude row explains how to configure
the bridge instead of claiming that Claude has no supported interface. substatus
never starts a Claude model turn to refresh the data.

## Provider documentation and credential boundaries

Research checked on **2026-10-08**:

- [OpenAI's Codex app-server documentation](https://developers.openai.com/codex/app-server)
  explicitly describes embedding the protocol in a product, the initialization
  handshake, managed ChatGPT authentication, and `account/rateLimits/read` with
  `usedPercent`, `windowDurationMins`, and Unix-second `resetsAt`. This is the
  documented mechanism used here, rather than private HTTP endpoints or a scrape
  of `/status`. `account/read` uses `refreshToken: false`; the app never starts
  a login, exports a token, creates a thread, or consumes/resets credits. The
  quota request excludes the separate reset-credit detail lookup.
- [OpenAI Terms of Use](https://openai.com/policies/terms-of-use/) prohibit sharing
  credentials, bypassing restrictions, and automated extraction of service data.
  This implementation relies on the provider-published client protocol and its
  explicitly documented status method, not dashboard extraction or private RPCs.
- [Claude Code's legal and compliance documentation](https://code.claude.com/docs/en/legal-and-compliance)
  restricts third-party use and collection of Claude.ai OAuth credentials/session
  tokens. This boundary remains active. No `claude` invocation, OAuth credential
  file, keychain access, or private usage endpoint is used by substatus.
- [Claude Code's status-line documentation](https://code.claude.com/docs/en/statusline)
  explicitly supports local commands receiving JSON and displaying rate limits.
  The bridge consumes that provider-delivered output. It does not authenticate
  with Anthropic or route inference requests.
- [Anthropic Consumer Terms](https://www.anthropic.com/legal/consumer-terms) and
  [Commercial Terms](https://www.anthropic.com/legal/commercial-terms) remain
  applicable to the user's Claude Code use. This app does not alter them or
  independently grant access to Claude.
- [Antigravity `/usage`](https://antigravity.google/docs/cli/commands/usage) is the
  official quota command. The [CLI changelog, v1.1.11](https://antigravity.google/docs/changelog)
  confirms structured print-mode output without an agent turn or quota spend.
- [OpenCode's route source](https://github.com/anomalyco/opencode/blob/dev/packages/console/app/src/routes/zen/go/v1/usage.ts)
  establishes the retained Go usage endpoint and its response fields.

The earlier prohibition on reusing Claude/Codex credentials is preserved. The
supported output/protocol integrations above replace the hard-coded unsupported
rows. No fallback reads private provider credentials or calls an undocumented
quota RPC. If a supported interface does not return quota data, the app reports
that limitation and points to the official CLI/account interface.

## Privacy and bounds

Provider commands have finite deadlines and output limits. CLI stderr, raw RPC
errors, account identity, and provider credentials are not rendered. OpenCode's
API key is excluded from child-process environments and is sent only to its
requested first-party API route. There is no app telemetry. Ordinary TUI mode
reads the Claude snapshot; only `--claude-statusline` writes it.

## Verification

Tests use synthetic CLI output and fake HTTP servers. They cover protocol ordering,
quota parsing, missing/null values, disabled buckets, invalid percentages, reset
handling, cancellation, stale Claude snapshots, and quota-only cache persistence.
They do not prove live account access or a particular installed CLI's JSON shape.

```sh
gofmt -w cmd internal
go test ./internal/provider ./internal/runner ./internal/ui ./cmd/substatus
go vet ./...
go test ./...
go test -race ./...
go build -o substatus ./cmd/substatus
./substatus --version
```

For a containerized check:

```sh
docker run --rm -v "$PWD:/src" -w /src -u "$(id -u):$(id -g)" \
  -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
  golang:1.24-bookworm sh -c 'test -z "$(gofmt -l cmd internal)" && go vet ./... && go test ./... && go build -o substatus ./cmd/substatus'
```

Live Gemini validation requires running `agy --print /usage --output-format json`
in a normal terminal and comparing its reported fractions/resets to the TUI.
Do not read its token file or replace the command with a private endpoint.

Verification record (2026-10-08):

- After the most recent source edits, a direct check passed a `gofmt`
  cleanliness check, `go vet ./...`, `go test ./... -count=1`,
  `go test -race ./... -count=1`, and `go build -o substatus ./cmd/substatus`.
  The rebuilt `./substatus --version` printed `substatus 0.1.0`.
- A live `agy --print /usage --output-format json` request returned status
  `SUCCESS` with 2 groups and 4 active buckets.
- The rebuilt `./substatus --once --no-color` parsed live Codex and Gemini status
  successfully. Quota values and account details are intentionally not recorded
  here.
- In that run, Claude correctly reported setup required because no status-line
  snapshot was configured, so the bridge was not exercised live. OpenCode
  correctly reported `OPENCODE_API_KEY` absent, so live OpenCode API-key status
  was not checked.
- The installed Codex CLI (0.161.0) generated its official app-server JSON
  schemas, and the request methods, parameters, quota fields, and reset/duration
  types were checked against them. That confirms the local protocol contract,
  not a live account response.
- The Claude status-line fields and update triggers were re-checked against
  the status-line documentation. Re-checking the OpenAI and Antigravity pages was
  blocked by sandbox network permissions; their citations above come from the
  earlier review.
- Still unverified live: the Claude bridge (not configured) and OpenCode API-key
  status (no key present).
