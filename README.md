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
```

Interactive keys: `r` refreshes, `q` or Ctrl-C quits. The UI redraws on resize.

## Provider sources

| Provider | Interface | Setup and limitations |
|---|---|---|
| **Codex** | `codex app-server`, documented JSON-RPC `account/read` and `account/rateLimits/read` | Install Codex and sign in through its official CLI. Authentication stays inside Codex. Uses stdin/stdout and never sends a model prompt. API-key-only accounts do not expose ChatGPT subscription quotas. |
| **Claude** | `claude -p /usage --output-format json --no-session-persistence` | Install and sign in through Claude Code. `/usage` runs locally: the app requires `local_command: "usage"` and `num_turns: 0`, so no model turn runs and no quota is spent. The quota lines are human-readable text; unrecognised lines are skipped and unparseable reset times are left blank rather than guessed. |
| **Gemini (`agy`)** | `agy --print /usage --output-format json` | Install and sign in through Antigravity CLI, version 1.1.11 or later. Parses `command.data.groups[].buckets[]`, `remaining_fraction`, and `reset_time`. These are Antigravity quotas; its Claude/GPT model buckets are not Claude or Codex subscription quotas. |
| **OpenCode Go** | `GET https://opencode.ai/zen/go/v1/usage` | Set `OPENCODE_API_KEY`. Preserves the requested direct API-key integration with no CLI fallback. This first-party, source-derived route has no stable public API contract and reports Go usage windows, not Zen credit balance. |

A missing CLI, missing sign-in, unavailable data, unsupported CLI version, and
failed status request have distinct rows. Missing/null quota fields never become
0% or 100%. Reset times and window durations come from provider output; the app
does not infer quotas from token counts or plan names.

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
  tokens. This boundary remains active: substatus runs the official `claude`
  binary, which authenticates itself. No OAuth credential file, keychain access,
  or private usage endpoint is used by substatus. The child process keeps only
  Claude Code's own `ANTHROPIC_API_KEY`/`CLAUDE_CODE_OAUTH_TOKEN`; other secrets
  are scrubbed from its environment.
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
requested first-party API route. There is no app telemetry, and substatus writes
no files.

## Verification

Tests use synthetic CLI output and fake HTTP servers. They cover protocol ordering,
quota parsing, missing/null values, disabled buckets, invalid percentages, reset
handling and year inference, cancellation, and rejection of Claude model turns.
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
