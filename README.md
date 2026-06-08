# Slimference

**Slimference is a local, Codex-first token-savings layer for coding agents.**

It runs on your machine, routes only the Codex sessions you explicitly launch
through it, and reduces wasted input tokens from repeated reads, noisy command
output, logs, search results, cache misses, and duplicated tool context.

The core rule is simple: **near-zero drawdown for savings**. Slimference is
designed to avoid the failure modes that make token savers dangerous:
hallucinations, weaker model reasoning, worse context memory, stale repo state,
tool/workflow regressions, confusing routing, or any user-visible "why did the
agent get worse?" moment. Default product paths are deterministic, guarded,
reversible, and fail open.

Slimference does **not** use external summarization, does **not** ask a smaller
model to rewrite your context, and does **not** replace conversation memory with
lossy summaries. The product philosophy is blunt: if a savings mechanism cannot
be made safe enough to run automatically, it does not belong in the default
product path.

## The Short Version

Slimference is built for one high-value surface first: **Codex text workflows**.

- Codex CLI support is first-class.
- Codex Desktop support is first-class through a scoped app-server launch path.
- Browser ChatGPT, ordinary ChatGPT.app launches, voice, realtime, vision,
  and computer-use surfaces are left alone by default.
- The expensive, repetitive part of coding-agent work is usually text and tool
  context. That is where Slimference spends its engineering budget.

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

## The Secret Sauce

Most token savers chase compression. Slimference chases **safe context
economics**.

The trick is not to summarize everything harder. The trick is to attack waste at
the places where the model never needed the full bytes again:

- repeated file reads after the model already saw the content;
- repeated search/test/git/log output with stable evidence;
- cache-hostile formatting and unstable request shapes;
- oversized tool archives that can be locally recovered;
- provider-cache misses caused by avoidable volatility;
- output/tool-surface overhead where the turn shape proves it is safe.

That gives Slimference an unusually strong tradeoff: high savings on real
coding sessions without depending on a second model, probabilistic memory, or a
"trust me, retrieve it later" workflow.

## Current Product Scope

| Surface | Status | Notes |
|---|---:|---|
| Codex CLI | First-class | Scoped launch through Slimference, normal shell Codex stays direct |
| Codex Desktop | Supported through scoped app-server launch | Normal Finder/Spotlight Codex stays direct |
| Browser ChatGPT | Direct | Not touched by default |
| ChatGPT.app direct launch | Direct | Not touched by default |
| Voice / realtime | Direct | Not optimized by default |
| Vision / computer-use | Direct | Not optimized by default |
| Claude Code | Parked | Code exists for reference, not installed by default |
| Global MITM / hosts / pfctl | Lab only | Explicit advanced path, not product default |

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

Slimference uses layered reducers because each kind of waste needs a different
safety contract.

| Layer | What it does | Why it exists | Safety posture |
|---|---|---|---|
| Layer 0 | Pre-entry / Codex tool-output reducers | Shrinks shell, git, test, log, search, read, and WSS tool output before or as it enters model-visible context | Parser guards, evidence preservation, archive recovery, fail open |
| Layer 1 | Deterministic compression | Removes deterministic waste from safe prefix/tool content | Shorter-than-original guard, schema checks, safety tiers, no semantic paraphrase |
| Layer 2 | Response and provider-cache leverage | Avoids repeat work and accounts provider-cache economics | Canonical keys, stochastic/stateful bypass, dependency invalidation, negative-net visibility |
| Layer 4 | Output and tool-surface reduction | Cuts avoidable completion/tool-definition overhead where the turn shape is proven safe | Exact-answer/repair guards, provider-shape validation, auto-demotion |

Typical wins come from repeated file reads, search outputs, test logs, git
output, JSON/log compaction, archive-backed tool references, and provider cache
alignment. Actual savings depend on workflow shape and should be measured with
the built-in reports.

## Expected Savings

Real savings depend on how you work, but the shape is predictable:

| Workflow | Realistic range | Why |
|---|---:|---|
| Long coding sessions with repeated reads/search/tests | 30-60% input-token reduction | Readcache, tool archive, search/log/test reducers, provider-cache leverage |
| Heavy refactor/debug loops | 40-70% on routed text/tool traffic | Same files and same command surfaces repeat many times |
| Short one-off prompts | 0-20% | Less repeated context means less deterministic waste to remove |
| Output tokens | Usually modest | Slimference avoids weakening the model's answer quality just to force shorter replies |

Those are not billing guarantees. They are the realistic target zone for
Codex-heavy text workflows where the same project context gets touched again
and again.

## How It Compares

Slimference borrows good ideas from the broader token-savings space, but draws
a harder product boundary.

| Tool / approach | Strong at | Slimference position |
|---|---|---|
| RTK | Broad agent wrapping, especially Claude/OpenCode-style surfaces | Great reference for tool breadth and parser ideas; Slimference currently focuses harder on Codex CLI + Codex Desktop and near-zero-drawdown routing |
| Headroom | Aggressive compression research, cache/relevance experiments, local model ideas | Useful inspiration for deterministic signals, parser hardening, cache validation, and observability; Slimference does not default-enable lossy Kompress/local-model/context-retrieval paths |
| Generic proxy/MITM | Capturing many clients through one network layer | Powerful for labs, too broad for default UX; Slimference keeps global routing out of the normal product path |
| Manual prompt discipline | Asking the model to be shorter or remember less | Cheap but unreliable; Slimference prefers deterministic byte/path/tool economics around the model |

The bet is simple: win the Codex coding workflow first, make the safe path feel
native, and only expand to other agents when the same no-drawdown bar can be
met.

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
computer-use flows are not the target path and are not globally intercepted by
default.

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
