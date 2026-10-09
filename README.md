# substatus

A Go TUI for subscription/quota status across **Codex**, **Claude**,
**Gemini (via `agy`)**, and **OpenCode**. It refreshes on a configurable interval
and displays provider-reported used percentages and reset times.

## Install and update

```sh
curl -fsSL https://raw.githubusercontent.com/rothzeta/substatus/main/install.sh | sh
```

The script downloads the latest [release](https://github.com/rothzeta/substatus/releases)
binary for Linux or macOS (amd64/arm64), verifies its SHA-256 checksum, and
installs it to `$BIN_DIR` (default `~/.local/bin`). **To update, run the same
command again**; it does nothing when you already have the latest version.
`SUBSTATUS_VERSION=v0.1.0` pins a release. On first install it offers to save an
OpenCode API key (`SUBSTATUS_NO_PROMPT=1` skips that) and prints a note if
`$BIN_DIR` is not on your `PATH`. If a different `substatus` earlier on `PATH`
shadows the installed one, the script warns.

Linux and macOS are supported. Windows is not.

To install from source instead (Go 1.24+, no cgo or external dependencies):

```sh
go install github.com/rothzeta/substatus/cmd/substatus@latest
```

`substatus --version` then reports the module version from the build info. A
plain `go build -o substatus ./cmd/substatus` reports `dev` unless built with
`-ldflags "-X main.version=..."`.

Releases are built by GitHub Actions when a `v*` tag is pushed, after the full
CI checks pass; the tag becomes the version that `substatus --version` prints.

### Uninstall

Delete the binary (`~/.local/bin/substatus`, or `$BIN_DIR/substatus`) and the
config directory (`~/.config/substatus/` on Linux,
`~/Library/Application Support/substatus/` on macOS). The config directory
holds only the saved OpenCode key.

## Usage

```text
substatus [flags]

  --refresh duration   automatic refresh interval, minimum 15s (default 5m0s)
  --once               print one snapshot and exit
  --no-color           disable ANSI color (also honors NO_COLOR)
  --version            print version and exit
  --set-opencode-key   read an OpenCode API key from stdin and save it
```

Interactive keys: `r` refreshes, `q` or Ctrl-C quits. The UI redraws on resize.
The interactive view needs a terminal; use `--once` in scripts. One refresh
costs several CPU-seconds (mostly `claude -p`), hence the 5 minute default.
Every provider row appears immediately as loading and fills in as soon as that
provider answers, so one slow CLI never holds up the others.

## Provider sources

| Provider | Interface | Setup and limitations |
|---|---|---|
| **Codex** | `codex app-server`, documented JSON-RPC `account/read` and `account/rateLimits/read` | Install Codex and sign in through its official CLI. Authentication stays inside Codex. Uses stdin/stdout and never sends a model prompt. API-key-only accounts do not expose ChatGPT subscription quotas. |
| **Claude** | `claude -p /usage --output-format json --no-session-persistence` | Install and sign in through Claude Code. `/usage` runs locally: the app requires `local_command: "usage"` and `num_turns: 0`, so no model turn runs and no quota is spent. The quota lines are human-readable text; unrecognised lines are skipped and unparseable reset times are left blank rather than guessed. |
| **Gemini (`agy`)** | `agy --print /usage --output-format json` | Install and sign in through Antigravity CLI, version 1.1.11 or later. Parses `command.data.groups[].buckets[]`, `remaining_fraction`, and `reset_time`. These are Antigravity quotas; its Claude/GPT model buckets are not Claude or Codex subscription quotas. |
| **OpenCode Go** | `GET https://opencode.ai/zen/go/v1/usage` | Run `substatus --set-opencode-key` or set `OPENCODE_API_KEY` (see below). It calls the API directly, with no CLI fallback. This first-party, source-derived route has no stable public API contract and reports Go usage windows, not Zen credit balance. |

A missing CLI, missing sign-in, unavailable data, unsupported CLI version, and
failed status request have distinct rows. Missing/null quota fields never become
0% or 100%. Reset times and window durations come from provider output; the app
does not infer quotas from token counts or plan names.

## OpenCode API key

```sh
substatus --set-opencode-key                    # hidden prompt
substatus --set-opencode-key < ~/opencode.key   # or piped, first line
```

The key is saved to `substatus/opencode_api_key` under the user config
directory (`$XDG_CONFIG_HOME` or `~/.config` on Linux, `~/Library/Application
Support` on macOS), written atomically with mode 0600. `OPENCODE_API_KEY`, when
set, takes precedence. Delete the file to forget the key.

## Provider documentation and credential boundaries

Checked on **2026-10-08**. No integration reads private provider credentials or
calls an undocumented quota RPC. If a supported interface returns no quota data,
the app says so and points to the official CLI or account.

- [Codex app-server documentation](https://developers.openai.com/codex/app-server)
  describes embedding the protocol in a product, the initialization handshake,
  managed ChatGPT authentication, and `account/rateLimits/read` with
  `usedPercent`, `windowDurationMins`, and Unix-second `resetsAt`. substatus
  uses this protocol, not private HTTP endpoints or a `/status` scrape.
  `account/read` uses `refreshToken: false`; the app never starts a login,
  exports a token, creates a thread, or consumes or resets credits.
- [OpenAI Terms of Use](https://openai.com/policies/terms-of-use/) prohibit
  sharing credentials, bypassing restrictions, and automated extraction of
  service data. substatus relies only on the provider-published client protocol
  and its documented status method.
- [Claude Code's legal and compliance documentation](https://code.claude.com/docs/en/legal-and-compliance)
  restricts third-party use and collection of Claude.ai OAuth credentials and
  session tokens. substatus runs the official `claude` binary, which
  authenticates itself, and uses no OAuth credential file, keychain access, or
  private usage endpoint. The child process keeps only `ANTHROPIC_API_KEY`,
  `ANTHROPIC_AUTH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN`; other common credential
  variables are scrubbed (a best-effort denylist).
- [Anthropic Consumer Terms](https://www.anthropic.com/legal/consumer-terms) and
  [Commercial Terms](https://www.anthropic.com/legal/commercial-terms) still
  apply to your Claude Code use. substatus does not alter them or grant access
  to Claude.
- [Antigravity `/usage`](https://antigravity.google/docs/cli/commands/usage) is the
  official quota command. The [CLI changelog, v1.1.11](https://antigravity.google/docs/changelog)
  confirms structured print-mode output without an agent turn or quota spend.
- [OpenCode's route source](https://github.com/anomalyco/opencode/blob/dev/packages/console/app/src/routes/zen/go/v1/usage.ts)
  defines the Go usage endpoint and its response fields.

## Privacy and bounds

Provider commands run in a private, empty temporary directory with finite
deadlines and output limits, and are killed when substatus exits. CLI stderr, raw RPC
errors, account identity, and provider credentials are not rendered. OpenCode's
API key is excluded from child-process environments and is sent only to its
requested first-party API route. There is no app telemetry. The only file substatus
writes is the OpenCode key, and only on `--set-opencode-key`.

## Verification

Tests use synthetic CLI output and fake HTTP servers. They cover protocol
ordering, quota parsing, missing/null values, disabled buckets, invalid
percentages, reset handling and year inference, cancellation, rejection of
Claude model turns, progressive snapshot delivery and subprocess reaping, the
CLI output cap and private working directory, environment scrubbing, and
redirect refusal. They do not prove live account access or a particular
installed CLI's output. CI runs the checks below on every push, and the release
workflow runs them before publishing.

```sh
test -z "$(gofmt -l .)"
go vet ./...
GOOS=darwin go vet ./...
go test -race ./...
shellcheck install.sh
```

Live Gemini validation requires running `agy --print /usage --output-format json`
in a normal terminal and comparing its reported fractions/resets to the TUI.
Do not read its token file or replace the command with a private endpoint.

## License

MIT; see [LICENSE](LICENSE).
