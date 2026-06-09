# Slimference

[![CI](https://github.com/Christopher-Schulze/Slimference/actions/workflows/ci.yml/badge.svg)](https://github.com/Christopher-Schulze/Slimference/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platform](https://img.shields.io/badge/platform-macOS-black?logo=apple&logoColor=white)](#quick-start)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

**Cut repeated Codex context without making the model worse.**

Slimference is a local, macOS-first token-savings layer for Codex CLI and
Codex Desktop. It routes only the sessions you explicitly launch through it,
then removes deterministic waste from repeated file reads, noisy command
output, logs, search results, cache misses, duplicated tool context, and
recoverable tool archives.

The product rule is strict: **savings are not allowed to buy quality loss**.
Slimference keeps model quality, context truth, file reality, tool recovery,
and routing clarity ahead of raw compression. If a reducer could make Codex
hallucinate, miss recency, forget relevant context, or work from stale repo
state, it is not allowed in the default path.

## Why It Exists

Coding agents burn a lot of tokens on the same project facts again and again:
the same files, the same test failures, the same git output, the same search
results, the same logs, and the same tool schemas. Slimference attacks that
waste locally, deterministically, and with recovery paths instead of asking
another model to summarize your context.

What you get today:

| Capability | Status |
|---|---|
| Scoped Codex CLI launch | First-class |
| Scoped Codex Desktop launch | First-class |
| WSS-first Codex routing | Default with safe fallback |
| Deterministic tool-output reduction | Default-on when shape-safe |
| Provider-cache and savings accounting | Built in |
| User-facing chat brevity hints | Conservative and shape-gated |
| Normal Codex / ChatGPT launches | Left direct unless explicitly launched through Slimference |

## Quick Start

Requirements:

- macOS
- Go 1.25+
- Codex CLI or Codex Desktop already installed and logged in

Build, install, and open the TUI:

```bash
./install.sh
slimference
```

Run one scoped Codex CLI prompt:

```bash
slimference codex run --transport=auto -- "check this project"
```

Update an existing source checkout:

```bash
./install.sh
~/.local/bin/slimference status --preflight
```

Normal `codex` in a shell and normal Codex.app launches stay direct unless you
launch them through Slimference or explicitly enable the advanced shared route.

## What Slimference Does

- Launches Codex CLI or Codex Desktop in scoped Slimference mode.
- Leaves normal Codex launches direct unless you start them through Slimference.
- Compacts deterministic tool output before it bloats model-visible context.
- Routes Codex traffic through a local daemon for cache-aware, proof-gated
  savings.
- Supports WSS-first scoped Codex routing with automatic safe fallback.
- Repairs WSS certification drift after Codex updates through daemon/TUI/CLI
  recert paths.
- Tracks savings, cache impact, routed activity, logs, and diagnostics locally.
- Falls back to direct Codex when the daemon or a proof gate is not safe.

Slimference is built for long coding sessions where agents repeatedly inspect
the same files, run the same tests, search the same repo, and carry lots of
tool output across turns.

## Core Approach

Most token savers chase compression. Slimference chases **safe context
economics**.

The goal is not to summarize everything harder. The goal is to attack waste at
the places where the model does not need the full bytes again, while preserving
the information it still needs to do the job:

- repeated file reads after the model already saw the content;
- repeated search/test/git/log output with stable evidence;
- cache-hostile formatting and unstable request shapes;
- oversized tool archives that can be locally recovered;
- provider-cache misses caused by avoidable volatility;
- output/tool-surface overhead where the turn shape proves it is safe.

This keeps the default product path local, deterministic, and recoverable while
still producing meaningful savings on repeated coding-agent work.

## Current Product Scope

| Surface | Status | Notes |
|---|---:|---|
| Codex CLI | First-class | Scoped launch through Slimference, normal shell Codex stays direct |
| Codex Desktop | Supported through scoped app-server launch | Normal Finder/Spotlight Codex stays direct |
| Browser ChatGPT | Direct | Not touched by default |
| ChatGPT.app direct launch | Direct | Not touched by default |
| Voice / realtime | Direct | Not optimized by default |
| Vision / computer-use | Direct | Not optimized by default |

Default install does not change system network settings and does not route
unrelated apps.

## Why Codex Hooks First

Slimference is intentionally focused on Codex's hook and app-server surfaces
before broad multi-agent support.

Codex gives Slimference a narrow, powerful integration point: the launched
process can be scoped, observed, and reverted without hijacking the whole
machine. That is why normal Codex remains direct, while "Slimference mode" is
something you explicitly launch from the TUI or `slimference codex run`.

This is also the key difference from broad proxy tools: Slimference does not
need to globally route your browser, system traffic, or unrelated OpenAI
clients just to optimize one coding session.

Codex Desktop is part of the story. Slimference can launch Codex.app through a
process-local app-server shim, so Desktop conversations can ride the same local
savings route as the CLI without changing normal Finder/Spotlight launches.

## Why It Is Safe

Slimference only turns savings into a normal product path when the reducer is
deterministic and bounded:

- **Quality invariant:** savings cannot buy hallucination risk, stale context,
  weaker reasoning, worse tool recovery, or confusing routing.
- **Fail open:** parser errors, unknown payloads, schema drift, daemon failure,
  or unsafe proof state send the original data or launch direct Codex.
- **No lossy summaries:** the retired semantic summary path is not a default
  savings layer.
- **No external compression model:** no context is sent to another model for
  rewriting.
- **No fake Desktop indicator:** current Codex Desktop builds do not expose a
  stable process-local text chip contract, so Slimference reports route truth in
  the TUI and logs instead of mutating model names or service tiers.
- **Scoped by default:** normal use routes only the Codex process you launch
  through Slimference.

## Savings Layers

Slimference uses layered reducers because each kind of waste needs a different
safety contract.

| Layer | What it does | Why it exists | Safety posture |
|---|---|---|---|
| Layer 0 | Pre-entry / Codex tool-output reducers | Shrinks shell, git, test, log, search, read, and WSS tool output before or as it enters model-visible context | Parser guards, evidence preservation, archive recovery, fail open |
| Layer 1 | Deterministic compression | Removes deterministic waste from safe prefix/tool content | Shorter-than-original guard, schema checks, safety tiers, no semantic paraphrase |
| Layer 2 | Response and provider-cache leverage | Avoids repeat work and accounts provider-cache economics | Canonical keys, stochastic/stateful bypass, dependency invalidation, negative-net visibility |
| Layer 3 | Output and tool-surface reduction | Cuts avoidable completion/tool-definition/chat overhead where the turn shape is proven safe | Exact-answer/repair guards, concise-chat low-ROI guard, provider-shape validation, auto-demotion, no risky model-facing directive unless proof-gated |

Typical wins come from repeated file reads, search outputs, test logs, git
output, JSON/log compaction, archive-backed tool references, and provider cache
alignment. Actual savings depend on workflow shape and should be measured with
the built-in reports.

## Expected Savings

Savings are reported per routed Codex session and split by source: local input
reduction, provider-cache effects, output-wire accounting, and tool-surface
pruning. The realistic but optimistic target zone is:

| Layer | Typical contribution in routed sessions | Strong-case contribution | Notes |
|---|---:|---:|---|
| Layer 0: tool-output reducers | 15-45% | 50%+ bursts | Biggest lever when reads, search, git, tests, logs, or WSS tool output repeat |
| Layer 1: deterministic compression | 3-15% | 20-30% | Helps on structured/repeated context; never semantic paraphrase |
| Layer 2: response/provider-cache leverage | 0-25% | 30-50% | Workload-dependent and reported separately from local input deletion |
| Layer 3: output/chat/tool-surface reduction | 0-8% | 10-20% | Conservative by default; concise chat hints only on safe answer shapes |

Layer contributions overlap and are not additive. The combined session outcome
depends on how much repeated project/tool context exists:

| Routed Codex session shape | Realistic session range | Strong-session upside | Why |
|---|---:|---:|---|
| Normal tool-heavy coding | 25-50% input-token reduction | 50-60% | Repeated reads, search results, git/test output, and cache-stable context |
| Long refactor/debug loop | 35-65% input-token reduction | 65-75% | Same files, failures, commands, and repo slices recur across turns |
| Search/read/log heavy loop | 45-70% input-token reduction | 70%+ bursts | Layer-0 reducers remove the most repeated tool bytes before they enter context |
| Short one-off prompt | 0-15% | 20% | Little repeated context means little deterministic waste |
| Output tokens | Usually modest | High only on exact-answer/chat shapes | Slimference keeps answer quality ahead of aggressive brevity |

Those are not billing guarantees and not a promise for every project. They are
the expected range for Codex-heavy text sessions that repeat project context.
Checked-in v0.6.0 gates currently pass on 55 live-corpus requests and 51 real
Codex CLI/Desktop sessions; the synthetic smoke corpus stays at 57.14% only as
a regression fixture, not as a production average.

## Design Boundaries

Slimference is intentionally conservative about what becomes a default product
feature.

| Boundary | Product rule |
|---|---|
| No semantic context replacement | No external summarizer, local LLM summarizer, OCRL ledger, or lossy memory replacement |
| No global routing by default | Normal install does not change system network settings or unrelated apps |
| No fake Desktop signal | Route truth is shown in Slimference status/TUI/logs, not by mutating Codex model names or service tiers |
| No quality trade for output savings | Output reducers are shape-gated, low-ROI guarded, and fail open |
| No hidden lock-in | `slimference disable` and `slimference uninstall` revert marker-owned state |

## How It Works

1. You start Slimference once as a local daemon.
2. You launch Codex CLI or Codex Desktop from Slimference.
3. Only that launched process gets Slimference routing/env.
4. Codex text/WSS traffic flows through `127.0.0.1:8990`.
5. Slimference applies deterministic reducers, cache accounting, and proof
   gates.
6. If anything looks unsafe, Slimference sends the original bytes or launches
   direct Codex.
7. The TUI shows activity, savings, logs, status, and setup health.

Normal Browser ChatGPT, normal ChatGPT.app, voice/realtime, vision, and
computer-use flows are not the target path and are left direct by default.

## WebSocket And Desktop Routing

Codex traffic is not just plain HTTP. Modern Codex sessions use a Responses/WSS
path for interactive turns. Slimference has a local WSS frontdoor on the daemon
port and a Phase-F reducer path that can inspect and reduce known Codex text
tool-output frames while preserving byte-equal forwarding on unknown or unsafe
frames.

Codex CLI is straightforward: `slimference codex run --transport=auto -- ...`
launches one Codex process with a scoped local provider route.

Codex Desktop is the unusual part. Slimference launches Codex.app with a
process-local `CODEX_CLI_PATH` app-server shim. That hidden shim starts the real
Codex app-server, points only that spawned app process at the local Slimference
route, and keeps ordinary Codex.app launches direct. This is why Slimference can
support both Codex CLI and Codex Desktop without turning on a machine-wide
proxy.

When WSS proof is stale, degraded, or unsafe, Slimference downgrades to bridge,
HTTP, or direct behavior instead of forcing a risky mutation.

## Install From Source

Build and install the TUI/CLI binary:

```bash
git clone https://github.com/Christopher-Schulze/Slimference.git
cd Slimference
./install.sh
```

Update a local source checkout:

```bash
./install.sh
~/.local/bin/slimference status --preflight
```

Open the TUI:

```bash
slimference
```

The installer only installs the local binary and prints PATH guidance. It does
not change system network settings. Use the TUI Setup view or
`slimference install` when you want the scoped Codex service/hooks.

### Install From a Release Archive

Download the macOS archive from
<https://github.com/Christopher-Schulze/Slimference/releases>, then:

```bash
tar -xzf slimference_0.6.0_darwin_arm64.tar.gz
cd slimference_0.6.0_darwin_arm64
./install.sh
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

## What You Can Inspect

Slimference is built to prove what happened instead of guessing.

| View / command | What it tells you |
|---|---|
| TUI Activity | Which Slimference-launched Codex sessions are currently routing traffic |
| TUI Savings | Total saved tokens, provider cache impact, archive savings, per-session accounting |
| TUI Status | Daemon health, install health, scoped route readiness |
| TUI Logs | Bounded diagnostic stream plus export |
| `slimference savings` | Human-readable daily savings summary |
| `slimference gain --proxy today` | Routed proxy/flight savings and cache economics |
| `slimference gain --cache today` | Provider-cache read/create/cached-token accounting |
| `slimference gain --output today` | Output-wire accounting where available |
| `slimference debug bundle` | Content-bounded diagnostics package for later inspection |

Reports separate local input savings, provider-cache read/create effects,
output-token accounting, and negative-net cache impact. That separation matters:
otherwise a cache tweak can look like savings while actually making the billable
shape worse.

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
go run ./scripts/build --install
~/.local/bin/slimference --version
```

Project docs:

- [`docs/install.md`](docs/install.md) is the install/uninstall source of truth.
- [`docs/documentation.md`](docs/documentation.md) is the technical reference.
- [`docs/spec.md`](docs/spec.md) is the implementation-driving specification.

## Reality Check

Slimference is not a general-purpose model optimizer and it does not make a
model smarter. It saves tokens by removing deterministic waste around the model:
redundant tool data, repeated reads, noisy logs, cache-hostile formatting, and
avoidable request bloat.

If a savings idea would make the model lose context, hallucinate, forget
details, work from stale state, or depend on an unproven retrieval/rewrite
scheme, it is not a default Slimference product feature.

## License

Slimference is released under the MIT License. See [`LICENSE`](LICENSE).
