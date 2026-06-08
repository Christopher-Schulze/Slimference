# Slimference

**Slimference is a local, Codex-first token-savings layer for coding agents.**

It runs on your machine, routes only the Codex sessions you explicitly launch
through it, and reduces wasted input tokens from repeated reads, noisy command
output, logs, search results, cache misses, and duplicated tool context.

The core rule is simple: **no model-quality drawdown for savings**. Slimference
does not use external summarization, does not ask a smaller model to rewrite
your context, and does not replace conversation memory with lossy summaries.
Default product paths are deterministic, guarded, reversible, and fail open.

## What Slimference Does

- Launches Codex CLI or Codex Desktop in a scoped Slimference mode.
- Leaves normal Codex launches direct unless you start them through Slimference.
- Compacts deterministic tool output before it bloats the model context.
- Routes Codex traffic through a local daemon for cache-aware, proof-gated
  savings.
- Tracks savings, cache impact, routed activity, logs, and diagnostics locally.
- Falls back to direct Codex when the daemon or a proof gate is not safe.

Slimference is built for long coding sessions where agents repeatedly inspect
the same files, run the same tests, search the same repo, and carry lots of
tool output across turns.

## Current Product Scope

| Surface | Status | Notes |
|---|---:|---|
| Codex CLI | First-class | Scoped launch through Slimference, normal shell Codex stays direct |
| Codex Desktop | Supported through scoped app-server launch | Normal Finder/Spotlight Codex stays direct |
| Browser ChatGPT | Direct | Not touched by default |
| ChatGPT.app direct launch | Direct | Not touched by default |
| Claude Code | Parked | Code exists for reference, not installed by default |
| Global MITM / hosts / pfctl | Lab only | Explicit advanced path, not product default |

## Why It Is Safe

Slimference only turns savings into a normal product path when the reducer is
deterministic and bounded:

- **Fail open:** parser errors, unknown payloads, schema drift, daemon failure,
  or unsafe proof state send the original data or launch direct Codex.
- **No lossy summaries:** the retired semantic summary path is not a default
  savings layer.
- **No external compression model:** no context is sent to another model for
  rewriting.
- **No fake Desktop indicator:** current Codex Desktop builds do not expose a
  stable process-local text chip contract, so Slimference reports route truth in
  the TUI and logs instead of mutating model names or service tiers.
- **Scoped by default:** no system proxy, no persistent OpenAI base URL, no
  machine-wide `chatgpt.com` route.

## Savings Layers

| Layer | What it saves | Drawdown posture |
|---|---|---|
| Layer 0 | Shell/tool output before it enters context | Deterministic parser reducers, fail open |
| Layer 1 | Repeated/noisy request content | Deterministic transforms only |
| Layer 2 | Response/cache accounting path | No model-facing lossy summary replacement |
| Layer 3 | Response and prompt-cache leverage | Cache-aware, measured, negative-net guarded |
| Layer 4 | Output/tool-surface waste where proven safe | Conservative and proof-gated |

Typical wins come from repeated file reads, search outputs, test logs, git
output, JSON/log compaction, archive-backed tool references, and provider cache
alignment. Actual savings depend on workflow shape and should be measured with
the built-in reports.

## Install From Source

Requirements:

- macOS
- Go 1.25+
- Codex CLI / Codex Desktop already installed and logged in

Build and install:

```bash
go run ./scripts/build --install
~/.local/bin/slimference install
~/.local/bin/slimference status --preflight
```

Update a local source checkout:

```bash
go run ./scripts/build --restart
~/.local/bin/slimference status --preflight
```

Open the TUI:

```bash
slimference
```

## Daily Use

From the TUI:

- **Launch Codex CLI** starts a new scoped Codex CLI session through
  Slimference in the current project directory.
- **Launch Codex App** starts Codex Desktop with the scoped Slimference
  app-server route when the proof gate is green.
- **Activity** shows currently routed Slimference traffic.
- **Savings** shows token accounting, cache impact, archive savings, and
  provider-level breakdowns.
- **Status** shows daemon/install/runtime health.
- **Logs** shows bounded diagnostic logs and export support.
- **Setup** repairs install/autostart/scoped route prerequisites.

Normal Codex CLI or Codex Desktop launches outside the TUI remain direct unless
you explicitly enable an advanced shared route.

## Useful Commands

```bash
# Launch one scoped Codex CLI prompt through Slimference
slimference codex run -- "check this project"

# Launch Codex CLI with the safest available transport
slimference codex run --transport=auto -- "run a quick status"

# Inspect scoped route / daemon state
slimference status --preflight
slimference codex status

# Savings reports
slimference savings
slimference gain today
slimference gain --proxy today
slimference gain --cache today
slimference gain --output today

# Diagnostics
slimference debug bundle
slimference debug paths
slimference debug flight last

# Service lifecycle
slimference start
slimference stop
slimference restart
slimference service status
```

## Local Data

Slimference stores runtime data under `~/.slimference/`.

Important paths:

- `~/.slimference/logs/`
- `~/.slimference/analytics/`
- `~/.slimference/filter.db`
- `~/.slimference/debug/`
- `~/.slimference/exports/`
- `~/.slimference/run/daemon.pid`

Diagnostics are designed to be useful without dumping private prompt content by
default. Review exported bundles before sharing them.

## Development

Run the full project gate:

```bash
go run ./scripts/ci
```

Fast focused checks:

```bash
go test ./cmd/slimference ./internal/tui ./internal/debug ./internal/filter ./internal/evidence
git diff --check
```

Build the installed binary:

```bash
go build -o ~/.local/bin/slimference ./cmd/slimference
slimference --version
```

Project docs:

- [`docs/install.md`](docs/install.md) is the install/uninstall source of truth.
- [`docs/documentation.md`](docs/documentation.md) is the technical reference.
- [`spec+.md`](spec+.md) is the implementation-driving specification.
- [`docs/todo.md`](docs/todo.md) tracks active engineering work.

## Reality Check

Slimference is not a magic output-token reducer and it does not make a model
smarter. It saves tokens by removing deterministic waste around the model:
redundant tool data, repeated reads, noisy logs, cache-hostile formatting, and
avoidable request bloat.

If a savings idea would make the model lose context, hallucinate, forget
details, work from stale state, or depend on an unproven retrieval/rewrite
scheme, it is not a default Slimference product feature.

## License

License file is not present in this checkout yet. Add one before publishing a
public release.
