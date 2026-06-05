# Slimference - Technical Documentation

Version: 2.3.0
Last updated: 2026-06-05

Comprehensive reference for the Slimference token-optimising proxy. This
document is re-written for the 2.3 line; sections follow current code
layout, each with file:line pointers so readers can jump from prose to
source in one hop.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Request Lifecycle](#3-request-lifecycle)
4. [Layer 0 - Pre-Entry Filter](#4-layer-0-pre-entry-filter)
5. [Layer 1 - Deterministic Compression](#5-layer-1-deterministic-compression)
6. [Layer 2 - OCRL Context Ledger and Background Summarisation](#6-layer-2-ocrl-context-ledger-and-background-summarisation)
7. [Layer 3 - Response Cache](#7-layer-3-response-cache)
8. [Provider Support](#8-provider-support)
9. [Install and integration](#9-install-and-integration)
10. [Bypass and Fallback](#10-bypass-and-fallback)
11. [Security](#11-security)
12. [Observability](#12-observability)
13. [TUI](#13-tui)
14. [Configuration Reference](#14-configuration-reference)
15. [CLI Reference](#15-cli-reference)
16. [Installation](#16-installation)
17. [Build and Release](#17-build-and-release)
18. [Testing Strategy](#18-testing-strategy)
19. [Package Map](#19-package-map)

---

## 1. Overview

Slimference is a Go reverse proxy plus local install tooling that reduces
token usage for Codex-first workflows. The current production target is Codex
CLI first and Codex Desktop next. Claude Code support remains in the tree for
reference, but the shipped product path parks it completely: no install, no
hooks, no `~/.claude` writes, and no Anthropic routing. The admin/control plane
listens on `127.0.0.1:8990`; the transparent
SNI listener defaults to `127.0.0.1:8443` when armed. Request bodies can run
through deterministic compression, background summarisation, output reduction,
and cache layers before the daemon re-dials the real upstream.

The proxy is **fail-open transparent**: request shape, headers, streaming
semantics, and unknown payloads are preserved. Known conversation bodies and
known output frames may shrink when a deterministic reducer proves the mutated
payload is shorter and schema-safe.

### Why it works

| Problem                                          | Slimference answer                          |
|--------------------------------------------------|---------------------------------------------|
| Large tool outputs repeated across turns         | Exact dedup plus archive-backed near-dedup  |
| Long sessions exceed the context window          | Context ledger shadowing; summary replacement is legacy opt-in |
| Identical requests re-cost tokens                | Response cache + prompt-cache breakpoints   |
| Verbose shell / git / test output                | 24 built-in filters + TOML DSL (Layer 0)    |
| Compression costs latency on small requests      | Thresholds + latency-budget guard (T54)     |

### Client support

- **Codex CLI**: default scoped path is `slimference install`, `status
  --preflight`, then `slimference codex run -- <prompt>`.
  This affects only that Codex CLI process and leaves Browser ChatGPT and
  ChatGPT.app direct.
- **Codex Desktop**: the app remains direct when launched normally from
  Finder/Spotlight. The Slimference path is
  `slimference codex launch-desktop --transport=app-server --replace-existing`,
  which sets only `CODEX_CLI_PATH` plus Slimference shim metadata on the spawned
  app process. Codex.app starts Slimference as its app-server; the hidden shim is
  a thin stdin JSON-RPC mediator that execs the real Codex app-server (provider
  block pointing at `http://127.0.0.1:8990/backend-api/codex`) and rewrites the
  one field that blocked routing: Codex Desktop opens conversations with
  `thread/start` carrying `modelProvider: null`, which resolves to the account
  default (chatgpt.com direct); the shim rewrites a default (null/absent)
  `modelProvider` to `slimference-codex`, byte-identical for everything else.
  Realtime/voice threads and explicit provider choices are passed through; any
  parse ambiguity fails open. stdout/stderr pass through untouched. This avoids
  the old proxy/CA/TLS root-store barrier entirely. Proof and TUI launches pass
  `--replace-existing` so an already running Codex.app is quit and verified gone
  before the scoped Slimference instance starts; raw CLI launch keeps a
  conservative refusal unless the same flag is explicit. Verified (2026-05-22):
  the spawned Desktop app-server holds loopback sockets to `:8990` with zero
  direct `chatgpt.com` sockets, and the daemon decisions log records the Desktop
  conversation as `route_mode=websocket_phasef` for `/backend-api/codex/responses`
  - the same Phase-F savings route the certified CLI uses, with byte-identical
  `permessage-deflate` frames. Desktop conversations therefore save like the CLI
  on real (compressible) turns. Voice (`thread/realtime/*`), Browser ChatGPT,
  ChatGPT.app, computer-use, and Claude Code are untouched. Note: the sampled
  `desktop status` WSS counters lag and must not be used to claim or deny
  savings; the decisions-log `route_mode` is the reliable signal.
- **Global transparent lab**: `cert-trust`, `root-arm
  --global-chatgpt-hosts`, `enable`, `disable`, and `root-disarm` still
  exist for explicit lab certification. They route `chatgpt.com` and
  `api.openai.com` machine-wide and therefore include Browser ChatGPT and
  ChatGPT.app in the bridge.
- **Claude Code**: code remains available in the repository, but the product
  binary parks it. `slimference install`, `hook`, `integrate`, TUI apps, and
  `/admin/apps` do not enable or modify Claude Code.
- **Generic OpenAI API**: `api.openai.com/v1/chat/completions` routed by
  path detection when no Codex UA is present.

### Design invariants

- **Scoped before global**: the default Codex CLI path is per-process and
  does not use `/etc/hosts`, pfctl, System Proxy settings, or persistent
  env vars. Transparent MITM is explicit global lab mode only.
- **Passthrough on failure**: if any layer errors, the original body is
  forwarded. See section 10.
- **Bypass switch**: a single atomic flag collapses every provider + layer
  toggle to off, making the proxy a pure relay.
- **`encoding/json` only**: no third-party JSON library.
- **Hot path budget ≤ 5 ms**: all Layer 1 sub-layers benchmarked. Layer 2 runs
  as a bounded background optimizer and is not a request-path prerequisite.

---

## 2. Architecture

```
┌─────────────┐       hooks        ┌─────────────────────────────────────┐
│ Codex CLI   │───────────────────▶│ slimference admin/control :8990     │
│ Codex App   │                    │                                     │
└─────────────┘       TLS/SNI      │ transparent SNI listener :8443      │
      │        chatgpt.com:443     │  ┌──── request/WSS pipeline ────┐   │
      └───────────────────────────▶│  │ detect → L1/F → L2 → L3      │   │──HTTPS──▶ chatgpt.com
                                   │  └──────────────────────────────┘   │──HTTPS──▶ api.openai.com
                                   │  ┌──── response pipeline ───────┐   │
                                   │  │ streamcut/repdet + cache     │   │
                                   │  └──────────────────────────────┘   │
                                   │                                     │
                                   │  TUI, admin state, analytics, debug │
                                   └─────────────────────────────────────┘

Claude Code support stays in the codebase as parked legacy/reference plumbing.
It is not installed, toggled on, or routed by the Phase H path.
```

The proxy process also owns:

- launchd service (macOS) with `KeepAlive{Crashed=true,SuccessfulExit=false}`
  that restarts the binary in ≤ 2 s when it dies (T68).
- On-disk state: `~/.slimference/` (analytics, read-cache, tool-archive,
  checkpoints, session-logs, filter.db).
- Config: `$XDG_CONFIG_HOME/slimference/config.toml` by default, with
  legacy `~/.slimference/config.toml` as fallback (T46).

### Main goroutines

- `http.Server.Serve`: accepts connections.
- Compression worker pool (default 4): drains `compressQueue`.
- Analytics worker (1): drains `analyticsQueue`, persists JSONL.
- FileWatcher: invalidates response cache on file modifications.
- HealthMonitor: 20-slot ring of per-provider request outcomes.
- Optional TUI goroutines (BubbleTea).

---

## 3. Request Lifecycle

Entry: `internal/proxy/proxy.go::ServeHTTP` (line 347).

1. **Provider detect** (`detectProviderWithUA`): path prefix and
   `User-Agent` decide Anthropic, OpenAI, or CodexChatGPT.
2. **Passthrough fast path** for non-compressible URLs (health, admin,
   streaming endpoints we should not touch).
3. **Body read**, bounded by `maxRequestBodySize = 32 MiB`. Oversize → 413.
4. **Re-detect** with body available (lets the body-probe branch run).
5. **Provider toggle** — if disabled via admin or bypass, straight
   passthrough.
6. **Version negotiation** (T62): unknown `anthropic-version` downgrades
   to `PipelineConservative` or `PipelinePassthrough`.
7. **Layer 0 hooks** — handled *out of process* by Claude Code / Codex
   before the HTTP request is ever sent, but the results appear as
   compressed tool outputs in the body we now receive.
8. **Layer 1 compression** — deterministic, 15 sub-layers plus preview
   passes.
9. **Prompt-cache breakpoints** (T45) — up to 4 `ephemeral` markers
   spread evenly across the stable prefix.
10. **OpenAI prompt-cache hints** (T136) — optional hashed
    `prompt_cache_key` and model-gated `prompt_cache_retention` injection
    for generic OpenAI API requests only; CodexChatGPT backend routes stay
    untouched until live proof.
11. **Layer 2** — background semantic summarisation when
    `len(tokens) >= min_tokens_for_layer2`, provider prerequisites pass, and
    latency policy permits (T54/T152).
12. **Upstream call** via the per-provider HTTP client. Streaming is
    preserved.
13. **Overflow recovery** (spec+.md §17.4): on HTTP 400 with context-
    too-large signal, retry with aggressive re-compression, then raw.
14. **Layer 3 response cache** — stores by request hash; `FileWatcher`
    invalidates on change.
15. **Analytics events** via non-blocking queue. Drops are counted +
    warn-logged (T42).

### Phase histograms (T58)

Every phase records into `internal/analytics/phase_hist.go` for live
p50/p95 via `/admin/status.pipeline`. Instrumentation overhead: ~15 ns
per phase.

### Panic recovery

`recoverMiddleware` catches any panic inside the handler: best-effort
passthrough using the original body stashed in the request context; if
the body was not yet stashed, returns 502.

---

## 4. Layer 0 - Pre-Entry Filter

Layer 0 runs **outside** the HTTP proxy. In product mode it is invoked by
Codex hooks only: `~/.codex/hooks.json` points at
`~/.slimference/hooks/*.sh`, which invoke `slimference filter`,
`slimference rewrite`, `slimference posttool`, `slimference codexhook`,
and `slimference readhook codex`. Claude hook code remains in-tree but no
public install path writes `~/.claude`.

### Pipeline

`internal/filter/pipeline.go::RunPipeline` runs:

1. Exec the tool command; capture stdout + stderr + exit code.
2. ANSI strip on stdout.
3. Built-in reducers tried in priority order from
   `filter.Layer0ReducerRegistry()`. The registry records each reducer's
   mechanism id, command family, safety class, default eligibility, and
   preserved-evidence contract before the reducer can participate in product
   dispatch. The default order covers git-status, git-diff, git-log, git-show,
   build-output, test-output, dotnet, ruby, search, ls, tree, lint, log,
   format, psql, package-manager, container, gh list, glab list, AWS JSON,
   python traceback, Terraform plan/init/validate/show, structured JSON, and
   JSON minify. Long `terraform state list` and plain human-readable
   `terraform output` full-pass in the default package because resource
   addresses, output names, and output values are requested facts unless a
   future route-specific reducer owns exact archive recovery.
   `build-output` includes the shared diagnostic parser for Go, Cargo,
   GCC/Clang, TypeScript, Svelte, frontend tools (Next/Vite/Vitest/Jest/
   Playwright/ESLint/Biome/Oxlint/Turbo/Nx/Lerna/Bun), Python diagnostics
   (ruff/pylint/flake8/mypy/pyright/pytest/unittest matching), Zig, SQL/DB
   client diagnostics (psql/sqlite/mysql/mariadb/Prisma/Drizzle/SQLFluff/
   Sqruff), Markdown, and practical ecosystem compilers (Java/Kotlin/Swift/
   Dart/Flutter/PHP, Docker/Kubernetes/Helm, and adjacent wrappers).
   `lint-output` also calls the shared parser after exact success compactors
   so non-empty Python lint/type-check output is reduced without losing the
   older ok-paths.
   `package-manager` compacts install/update success summaries and
   npm/pnpm/yarn/bun/pip/uv resolver-error noise to actionable lines.
   `psql` covers SQL-shell table-border compaction for psql, MySQL/MariaDB,
   SQLite, and SQLite3 outputs.
4. Fallback: `FirstMatchingTOMLRule` applies user-defined 8-stage
   rules from `~/.slimference/filters.toml`.
   The embedded default TOML catalog uses a product-safe application path:
   line caps preserve late error/fatal/warning/diagnostic evidence and emit an
   omitted-line marker. User and project TOML rules keep their literal DSL
   semantics, because they are operator-owned configuration.
5. Truncate with a short `[truncated …]` hint to
   `passthrough_max_chars` (default 4000; spec+.md §4.6).
6. Emit to stdout + write the raw bytes to the tee dir for recovery.
7. Record the run in `filter.db` (SQLite) with local-tokenizer counts for
   input/output tokens, falling back to byte/4 only if the tokenizer is
   unavailable.

### Exit-code contract

See `docs/layer0-exit-codes.md` for the complete matrix. The invariant:
Slimference **never** swallows a non-zero child exit. Filter failures
are degradation signals, not status translations.

### Token-saving reporting

`slimference gain [today|week|month|all]` aggregates rows from
`filter.db` into a summary with savings percentages. Layer 0 now stores
local-tokenizer counts rather than plain byte/4 estimates, so parser-family
comparisons are closer to what the provider will bill. `--by-command` breaks it
down per argv[0]; `--by-parser` groups persisted Layer-0 savings by parser/tool
family; `--cache` reports persisted provider prompt-cache read/create counters;
`--output` reports persisted T130 output-reduce telemetry without inventing a
savings baseline, including T141 profile tier and task-shape buckets plus a
provider/model/profile/task-shape profile row report for manual evolution;
`--proxy`
reads flight-recorder decision logs and reports provider-only proxied LLM
requests with input/cache/output accounting. `--csv` / `--json` for machine
consumption.

### Output-reduce quality governor

Output reduction is a runtime-governed output-wire layer, not a billable-input
savings claim. HTTP/SSE streamcut and stop-sequence injection remain separate
from Codex WSS Phase-F input reducers; WSS streamcut is deliberately disabled
until a terminal-safe WSS sequence is live-proven. Terminal WSS response payloads
stay byte-equal so code, patch, JSON, and final-answer text cannot be silently
rewritten after the model emits it.

The output directive injector is task-shape aware. Exact replies and repair
follow-ups skip injection; exact replies include `reply only`/`respond only`/
`return only`/`json only` style prompts and the German `gib/antworte/sage nur`
variants, not only `reply exactly`. Detail-sensitive task shapes full-pass
output-reduce injection by default until each shape has positive paired A/B
evidence: code edits, new-file generation, debugging, reviews, tool-result
reasoning, final summaries, read-only analysis, deep explanations, and
planning. This is stricter than the earlier standard-only cap and prevents
unproven directive text from changing model behavior on tasks where missing
detail is a product drawdown. Explicit command-output relay turns such as "show
the output", "full terminal output", or
German `gib die komplette Terminal-Ausgabe` skip output-reduce injection entirely
with `command_output_relay_exact_output`, preserving requested output, paths,
errors, exit codes, and line order. The proof gate runs before runtime cooldown
selection in the proxy and again inside the injector as defense in depth. The
central planner mirrors the same contract for product/audit output: exact
reply, command-output relay, repair follow-up, unproven detail shapes, and
low-ROI direct-answer turns are reported as bypassed. Repair signals such as
"you skipped", "too short",
missing detail, malformed patches, failed apply-patch
feedback, and German `fehlt`/`nochmal ausführlicher` style re-asks are stored by
session and immediately downgrade the affected
provider/model/profile/task-shape bucket without waiting for the normal sample
window.
Task-shape detection reads only model instructions: user, system, developer,
top-level `instructions`, top-level `system`, and top-level prompt/input text.
It deliberately ignores prior Codex `function_call`, `function_call_output`,
tool stdout/stderr, and tool arguments, so old terminal output cannot make the
current user turn look like a patch, repair, or command-output relay request.
This keeps aggressive-profile caps focused on the actual current instruction
instead of silently sacrificing output savings because of historical tool text.
For Codex Responses bodies, output-reduce directives are written only to the
top-level `instructions` string. The injector does not rewrite `input` and never
creates `input` items with `role=system`, because Codex rejects those and because
output-reduce must not alter the model's task/tool context while trying to save
output tokens. On the WSS Phase-F path, output-reduce is considered only for
prompt/user-turn request bodies. Requests carrying `function_call_output` items
are not output-reduce candidates, so read/search/git/test/tool-output reducers
remain the only mechanisms that can alter tool-output deltas.
Streaming provider usage is accounted by field semantics, not by blind addition:
if an OpenAI/Codex or Anthropic stream reports final `output_tokens`, that total
replaces earlier text estimates for the request; OpenAI/Codex `cached_tokens`
usage is likewise treated as a per-request total. Anthropic cache read/create
fields remain separate provider counters. This prevents output-wire or
OpenAI/Codex provider-cache claims from being inflated by counting an
intermediate usage event plus the final usage event.

### Codex read-compression (mechanisms and safety model)

Goal: maximum Codex token savings with no model-quality drawdown. A drawdown is
the model getting dumber, losing context, drifting from the real files, breaking
workflow, or hallucinating because Slimference removed information too early.
Mechanisms split into two product classes.

Default-auto tier (the proven product bulk): read-delta (repeat reads collapse to
references), exact repeated-output, content-defined chunk dedup, the Codex
exec-envelope stripper, and low-risk search-output grouping. The exact reducers
elide only what the model already holds or can reconstruct exactly; search grouping
keeps representative match context plus the recovery path of re-running the search.
On a real codebase-exploration session the default-auto reducers saved 36533
billable tokens with zero parse/compression/degraded errors.

Retired path: first-read AST/structure scan-mode elision is not part of the Codex
product mode. It saved tokens in narrow probes, but it gave the model less file
information on first sight and relied on the model noticing the missing detail and
re-reading. Slimference's default instead keeps first file reads unelided and
spends engineering effort on better cache hits, repeat-output reuse, ranged-read
reuse, and search/build/test compaction.

Safety model: (1) first file reads full-pass; (2) repeat and ranged reads use exact
cache/delta decisions; (3) edit-target guards - recently-edited or edit/debug reads
full-pass; (4) archive-backed references are emitted only by mechanisms that keep a
deterministic recovery handle and pass the positive-token guard.

Current product status:

| Mechanism | Default status | Proof state | Drawdown position |
|---|---|---|---|
| WSS Phase-F routing for Codex CLI/Desktop | On when route proof is fresh; bridge/fallback on drift | CLI and Desktop route plus mutation proofs recorded; auto-recert guards version drift | Fail-open; route-ready is still distinct from savings-proven |
| Read-delta for repeated full-file reads | On | Proven in real CLI/Desktop repeat-read captures and A/B replay with `lost=0`; 2026-06-02 strict release matrix covered CLI + Desktop repeat reads and the mixed Desktop workday | Low risk: first read was already sent in full |
| Ranged read-delta for `head` / `tail` / `sed -n` | On | Covered by T250/T257 capture matrix; 2026-06-02 strict release matrix covered CLI + Desktop ranged `sed -n` repeat reads | Low risk: first range full-passes, later same range collapses only after exact observation |
| Exact repeated non-file output dedup | On | Implemented through the shared Codex Layer-0 reducer; 2026-06-02 automatic CLI replay covered Codex exec-envelope repeated-output recovery with `lost=0`; 2026-06-02 Desktop search-delta proof recorded a live repeated-output hit with 14,973 billable input tokens saved | Low risk: exact same command/output only, archive-backed, fail-open on changes; search uses a stricter match-set delta when visible evidence changes |
| Search-output grouping and repeated search delta | On | Real `rg` capture compacted about 40 KB to about 9 KB; T257 covers search workloads; 2026-06-02 strict release matrix covered CLI + Desktop search loops plus a mixed Desktop workday; 2026-06-02 Desktop search-delta proof passed live counters and replay `lost=0` | Low to medium: grouped first search keeps representative matches, changed repeated searches emit added/removed match evidence plus archive recovery, and ambiguous cwd full-passes for reusable keys |
| Build/test/git/lint/parser compactors | On where parser recognizes the command/output | Unit/integration covered; T252/T260 hardened caps and error priority | Low to medium: deterministic parser summaries only, positive-token guard |
| Content-defined chunk dedup | Auto-eligible on recoverable WSS tool-output workloads; HTTP blocked from archive refs | T255/T266 live CLI+Desktop proof, T256 policy wiring, T258 route/risk/proof gate | Medium but guarded: archive recovery and `live` proof required, recent/re-read risk loosens |
| Archive recovery note | Default-off | Mechanism and replay support exist | Kept off by default until route/workload proof needs it |
| First-read AST/signature scan-mode | Removed | Removed by T253; tests enforce first file reads full-pass even in `max` | High drawdown, not product-safe |
| Predictive post-edit file state | Closed | T253 closed | Rejected for default-auto: first post-edit read full-passes to preserve recency/context; later repeats dedup normally |
| apply_patch context dedup | Closed | T253 closed | Rejected as standalone work: patch context is model working memory; exact repeated outputs remain covered |
| Reasoning-trace compaction | Closed | T253 closed | Rejected for default-auto: do not mutate reasoning/cognition surface for savings |
| Server-state mirror | Shadow/policy infra only | T254 closed as shadow | Tracks exact forwarded-state opportunities; no generalized model-facing mutation or reference language |
| Policy engine v2 | Foundation active | T258 in progress | Central route/workload/risk/recovery/proof decisions; unsafe candidates blocked or telemetry-only |
| HTTP archive recovery/promotion | Conservative lock active | T259 closed | HTTP fallback keeps safe Layer-0 reducers but cannot emit archive refs even in `max` |

Real-workload truth that shaped this: Codex reads files via `sed -n '1,Np'` partial
reads and searches via `rg`, never full `cat`, and truncates every exec output to a
token budget. So the original `cat`-only scan could not fire (extended to `sed`), and
search grouping was defeated by the truncation tail (made robust). The recurring
upstream `400 invalid_request` is an oversized-request rejection, which Slimference's
compaction makes less likely by shrinking requests, not a Slimference fault.

Codex WSS and HTTP proxy-Layer-0 savings now share one explicit reducer entry
point and a central policy engine with route labels (`http`, `wss_phasef`),
workload classes, mechanism risk, recovery level, and proof level. Current policy
actions are `allow`, `shadow`, `full_pass`, and `block`: proven lossless reducers
are allowed, recoverable WSS chunk dedup is allowed only with archive recovery and
`codex_chunk_dedup_proof_level="live"` evidence, and recent/edit and
post-collapse re-read signals full-pass. First-read elision,
predictive post-edit synthesis, apply_patch context dedup, reasoning compaction,
and generalized server-state-mirror mutation are closed as non-product-default
surfaces. The server-state mirror remains telemetry/policy infrastructure only.
HTTP is explicitly blocked from archive-backed chunk references; WSS is the
product route for recoverable archive/chunk mechanisms.

Layer 2 is being redirected away from "summary as truth" toward deterministic
context ledgers. The pure `internal/contextledger` package builds archive-backed
capsules for command, file, search, failure, decision, and recovery observations:
compact facts plus provenance, stable hashes where raw bytes exist, and archive
ids, without storing raw omitted content inside the capsule. This is the safe
replacement foundation for old-context compression. A deterministic selector now
fails closed before any future model-facing use: active turns, recent turns,
missing policy session scope, missing provenance, missing archive ids, file or
search capsules without an explicit execution scope, incomplete decision/recovery
facts, and high-risk failure content stay verbatim; only old inactive archive-backed
command/file/search/decision/recovery capsules can be selected. Archive expansion
is loader-based and must restore exact bytes or fail. It is not yet a default
hot-path replacement mechanism; readcache provenance, replay, and live corpus
proof remain the promotion gates.

Classical Layer 2 summary replacement is double-gated. First, Layer 2 itself
must be enabled. Second, cached extractive or provider summaries remain
shadow/background artifacts unless
`[compression.summary].allow_model_facing_replacement = true` (or
`SLIMFERENCE_L2_ALLOW_MODEL_FACING_REPLACEMENT=1`) is explicitly set. This keeps
summary-as-truth out of the product path while the context-ledger replacement is
being proven. The same double gate covers normal request compression,
mid-exchange summaries, and overflow recovery's read-only cached summary pass.

The Codex Layer-0 reducer now feeds the tool-output ledger builders in the hot
path as telemetry only. It builds command/file/search/failure capsule
observations from tool-output metadata and exposes only content-free capsule
counts in `/admin/state.savings`, globally and per `http` / `wss_phasef` route.
Search ledger telemetry requires explicit execution scope, either from tool
`workdir`/`cwd` metadata or from a repo-scoped command such as
`cd /repo && rg ...`, an absolute search path, or `git -C /repo grep ...`;
implicit-cwd search output remains telemetry-full-pass for ledger search
capsules.
Decision and recovery capsules are pure fail-closed primitives for the future
ledger insertion path; they are not counted from the reducer until a real
product source supplies explicit decision/recovery provenance. WSS decision
summaries also carry a `context_ledger` shadow block with capsule counts and the
re-read canary count, making ledger coverage and context-pressure visible in live
proof logs without logging payloads. No ledger capsule is inserted into
model-facing context until archive provenance, expansion replay, and live proof
are complete.

Readcache decisions expose structured `ArchiveURI` and `FullPassTurnID`
provenance to the reducer. File ledger observations are counted only when an
archive-backed source and explicit tool-call workdir exist; the provenance comes
from the readcache decision object, not by parsing model-facing marker text. This
keeps the ledger path content-free and fail-closed. Future capsule selection also
refuses command, file, search, and failure capsules whose required deterministic
facts are missing, and archived file capsules require repo/workdir scope plus the
prior full-pass turn before they can be counted or promoted.

The offline A/B harness can replay archive-backed references with a caller
provided archive resolver. A `local-archive://` marker is considered safe only
when the resolver expands it to the exact elided bytes or when the same bytes
were already sent verbatim earlier in the session; missing or mismatched archive
expansion is counted as a lost comprehension issue. Codex exec envelopes are
normalized for this prior-full check: the harness tracks the stable payload after
`Output:\n` in addition to the full block, so volatile `Chunk ID` / `Wall time`
headers do not turn a safe repeat-read collapse into a false loss. Archive-backed
Codex exec compaction also stores the stable `Output:` payload, not volatile
wrapper bytes. OCRL archive lists are replay-aware too: `archives=[...]` inside
an `[ocrl:v1 ...]` block is treated as a recoverable reference only when the
resolver expands the listed archive id to the exact direct block. If OCRL deletes
a following old block because the first OCRL block covers it, the harness checks
the whole compressed turn's OCRL archive set before calling the deletion lost.
envelope metadata, and the harness follows bounded nested archive references so
`captured_output` followed by `repeated_output` can be proven recoverable without
pretending the model needs the changing `Chunk ID` line.

The reducer telemetry includes mechanism attribution:
tool-result blocks seen, unresolved tool-use references, command-resolved
blocks, command-unresolved blocks, read-delta attempts, read-delta misses,
modified blocks, read-delta blocks, captured-output filter blocks, and Codex
exec-envelope blocks, exact repeated-output blocks, chunk-dedup blocks, and
content-free policy counters under `proxy_layer0_policy` keyed by route,
mechanism, action, reason, and block reason. Opportunity and miss fields make
hit-rate visible without claiming savings. The modified-block and mechanism-hit
fields are success counters and are only recorded with a positive token saving.
Cache-decision counters under `proxy_layer0_cache` separately record route,
mechanism, `hit`/`miss`, reason, and count for read-delta and exact
repeated-output. Those reasons make cold starts, first-seed full passes,
recent-edit bypasses, missing session/key state, non-shorter deltas, unavailable
archives, and unchanged hits visible without raw payload capture.
For product UI and low-noise status surfaces, `/admin/state` also exposes
`savings.product`: a content-free rollup with `status` (`idle`,
`active_no_savings`, `saving`, or `attention`), billable input tokens saved,
output-wire bytes saved, request-side bytes reduced, cost estimate, cache hit/miss
counts, read/repeated/chunk hits, tool-resolution misses, analytics drop
counters, and aggregate safety issues. Analytics drops are split into total,
proof-critical, and low-priority counters; any proof-critical analytics loss
sets the product status to `attention` because the release proof window is no
longer evidence-complete. The raw route, policy, parser, and cache counters
remain available under the existing debug fields, but product surfaces should
prefer this rollup instead of inventing their own mixed headline.
`/admin/state.host_budget` is the product resource guard. It reports `ok`,
`unknown`, or `attention`, daemon RSS, uptime, process CPU time, lifetime and
windowed CPU percentage, OS-reported lifetime and windowed disk read/write
operation counters, bounded state-directory size, the 200 MiB RSS budget, the
512 MiB state budget, WSS compression/parse/degrade health, mutation activity,
and content-free reason codes. The daemon/admin path uses an in-process
resource probe for PID, uptime, real RSS, process CPU time, disk I/O counters,
and state size where the platform can provide it, avoiding a loopback
self-health guess for the product budget. State-directory scans are entry-bound:
if a state tree exceeds the bounded scan limit, the host budget treats that as
resource pressure instead of reporting a partial undercount as healthy.
If the RSS resource probe is missing while WSS is otherwise active, the status
remains `unknown` instead of becoming `ok`; proof gates must not treat an
unmeasured host as a green resource budget. The TUI product panel renders that
as `host budget unknown` instead of `safety ok`.
CPU-window demotion ignores sub-second windows: tiny startup/admin-poll samples
remain visible in telemetry but cannot force managed reducers into full-pass.
Only a stable window of at least one second can trigger
`cpu_window_budget_exceeded`, which prevents false startup demotion while still
protecting the product path from sustained local CPU pressure.
Policy demotion uses the same concept through budget inputs. Host-budget
attention demotes recoverable or heavier mechanisms such as chunk references
while keeping cheap lossless/exact cache-hit reducers (`read_delta`,
`repeated_output`) available, so a transient local resource spike does not erase
the safest savings. Repeated Layer-0 latency budget breaches set a separate
`latency_budget_full_context` gate after three slow frames and recover after
cheap frames, so one spike does not disable savings but repeated local overhead
cannot degrade Codex UX. The latency demotion bucket is persisted under
`.slimference/runtime-budget` with a 30 minute TTL and capped strike debt, so a
daemon restart does not immediately forget recent local-overhead pressure, while
cheap frames still auto-recover without operator action. Readcache and WSS
tool-use/collapsed-key state use same-process memory plus short write-behind
flushes, so reconnect hydration is immediate while per-frame sync writes stay
out of the hot path. Readcache
write-behind flushes are version-guarded: a delayed disk flush can only mark the
same state revision clean, and it cannot overwrite a newer in-memory save made
while the flush was in flight. Windowed CPU and disk-write spikes also trip
host-budget attention, which demotes heavier managed Codex reducers until the
next healthy snapshot without turning off lossless exact repeat-read savings.
Exact token-count guards use the provider tokenizer, but large repeated texts
are cached by encoding, byte length, and SHA-256 content hash. This keeps
o200k/cl100k accounting exact while preventing repeated BPE regex passes from
becoming the local bottleneck on repeated Codex reads/searches.
These counters are emitted globally and under `proxy_layer0_routes.http` /
`proxy_layer0_routes.wss_phasef` through `/admin/state` and `aggregate-savings`,
so future cache or reducer work can measure which route and mechanism actually
saved tokens before broadening mutation surfaces. `workday-savings` carries the
same counters through start/finish deltas, and `wss-audit --admin-state-file`
can join a matching admin snapshot to show policy and cache decisions next to
the decisions-log route/session audit.
Codex tool metadata preserves `workdir` / `cwd` / `working_directory` /
`directory` when present. Relative single-file read commands are resolved
against that absolute workdir before readcache evaluation, which improves
repeat-read hit rate and prevents same-relative-path cache collisions across
repositories. Plain `git ...` commands with absolute workdir metadata become
`git -C <abs> ...`, so repeated git output keys stay repo-scoped. Safe shell
wrappers of the form `cd <abs> && <read>` are normalized to absolute `cat` /
`head` / `tail` / `sed -n` commands, and `cd <abs> && git ...` is normalized to
`git -C <abs> ...`. Search wrappers are not stripped until the path/key can be
made repo-safe. Codex tool outputs that arrive
as a single text part inside an `output` / `content` style array, or inside a
nested MCP-style object such as `result.content[0].text`, are extracted and
rewritten in place, preserving sibling non-text parts, metadata, and the
original object/array shape. Multi-text or otherwise ambiguous arrays fail open
instead of being stringified.

The readcache frontier is archive-backed for large observed tool reads. Full-file
and recognized ranged outputs (`cat`, `head`, `tail`, `sed -n`, and strict
single-file `awk` line ranges) are hashed and stored in the local content archive
while the session JSON keeps only the
hash/archive URI, avoiding unbounded session-state bloat. Ranged reads are keyed
by `path+offset+limit`, so `head -n 80 x` and `sed -n 120,180p x` never collide
with each other or with the full file. On a later same-session read, Slimference
expands the archive only when needed to build an exact delta; unchanged reads
use the stored content hash and archive URI without inflating the gzip payload.
If the archive is missing or the delta is not shorter, the original content is
sent unchanged. This keeps the savings path reconstructable and fail-open while
improving repeat-read hit-rate for both WSS and HTTP Codex traffic.

The same session state also tracks exact repeated non-file tool outputs. For
non-read commands, the reducer first applies deterministic captured-output
filters; if the candidate text is what will actually be sent upstream, its hash
is recorded under the resolved command key. A later identical command/output
pair in the same session can be replaced by a neutral archive-backed unchanged
output note. The mechanism is exact-only: changed output, short output,
unresolved commands, archive failures, and full-file reads fail open. Full-file
`cat` reads stay on the full-file read-delta path. Partial reads use their own
ranged read-delta key when the range is recognized; other deterministic commands
can still save through the exact command/output path when the same output repeats.
Repeated search outputs (`rg` / `grep` / `git grep`) also get position-aware
delta treatment, not just exact unchanged references, but only when the key is
repo-scoped through an absolute workdir, `cd <abs> && ...`, absolute search path,
or `git -C <abs> ...`. An implicit-cwd search can still use first-pass grouping,
but it does not seed cross-turn search collapse or search delta state. This keeps
repeat savings for commands such as repeated repo-scoped `rg`, `git status`,
build/test reports, partial file ranges, or custom deterministic tools without
introducing semantic summaries or cross-repo false hits.

Layer-0 reducer metadata is part of the safety contract. Every default reducer
declares its mechanism id, command family, safety class, required retained
fields, preserved evidence, and fail-open recovery path. The registry is used
for audits/control surfaces only; it does not add model-facing text.

Layer-0 cap handling is evidence-first, not first-N. Known default reducers that
truncate large outputs keep actionable rows before noise and sample the tail
inside important groups: test JSON keeps late failures, SARIF/ESLint JSON keep
late same-priority errors, kubectl JSON full-passes healthy lists and only
compacts unhealthy attention items, cargo metadata keeps late workspace members,
Terraform JSON keeps late destructive or state resource evidence, search grouping
keeps head/tail matches per file and file set, and the embedded default TOML
catalog keeps late diagnostic evidence when `max_lines`, `head_lines`, or
`tail_lines` would otherwise cut it away.
Omitted-count markers remain explicit, and malformed or non-shorter outputs
full-pass where the reducer owns that gate; operator-authored TOML rules remain
literal user configuration.

Git diff/show compaction is evidence-preserving: it keeps file paths, hunk
headers, added/removed lines, and structural diff metadata such as mode changes,
new/deleted files, renames/copies, similarity markers, and binary-file markers.
Context lines are stripped only after that metadata is retained. This avoids the
old silent loss where a rename-only or mode-only diff could collapse to a
`+0/-0` file entry without the reason for the change.

First-pass search outputs are grouped by `TryCompactSearchOutput` /
`groupSearchResults` (file -> match list with a `[tool] N match(es) in M file(s)`
header, capped at 30 files with `[+N more files]`). This grouping used to abandon
the whole output on the FIRST colon-less line, which a real-workload capture showed
defeats it on every Codex search: Codex truncates exec output to a token budget, so
the captured `rg` payload always ends in a cut-off line and carries a leading
`Total output lines: N` header - both colon-less. The grouper now SKIPS colon-less
noise lines (header, context separators, truncated tail) and only abandons grouping
when nothing parses or noise dominates (`skipped*2 > nonEmpty`). On the real captured
`rg` (402 matches, 79 files) this compacts 40 KB to ~9 KB (78%). The compaction is
default-auto in the filter pipeline, low-risk (the model keeps the match count,
representative file/match context, and can re-run the search to recover dropped matches),
and is a search-output reducer, so it has none of the first-read-seeding conflict that
made first-read scan-mode unsuitable for the product default.

Codex content-defined chunk dedup is available as a policy-gated extension of the
same Layer-0 reducer. A multi-plan chunker splits large tool outputs/file reads
into content-addressed regions: FastCDC handles general binary/text overlap, and
a line-boundary plan handles long logs and command outputs where line stability is
stronger than byte-window stability. A bounded in-memory session store tracks only
chunk identities, not raw payloads. When a later output shares chunks with content
already sent to the model in the same session, the reducer chooses the best locally
verified plan and emits the novel bytes plus neutral
`[context-chunk status=unchanged uri=local-archive://...]` references to archived
chunks. The product default is
`codex_savings_policy_mode="auto"`: safe lossless reducers stay on, and chunk
dedup is limited to recoverable WSS paths with archive support, cross-send
seeding, density budgets, local decode verification, and patch/diff/edit-output
guards. Same-batch edit uncertainty also demotes only chunk dedup: if the current
Layer-0 batch carries an edit/apply-patch/write signal, fresh command/search/log
outputs stay full context while lossless read-delta and exact repeated-output
reducers may still run. Cross-send seeding means repeated chunks first
encountered inside the same model-facing output stay verbatim and only seed
future overlap; references are emitted only for chunks that were known before the
current output started.
Commands such as
`apply_patch`, `patch`, `diff`, `colordiff`, patch/diff file reads,
`git diff`, `git show`, `git log -p`, `git apply`, `git am`,
`git format-patch`, `gh pr diff`, `gh pr view --patch`, `jj diff`,
`jj show`, `hg diff`, and `svn diff` do not receive chunk references, because
those outputs are fresh reasoning material for edits and reviews. Normal
search/status commands, including searches whose pattern is the word `diff`,
remain eligible. Patch/diff outputs can still use safer deterministic filters
or exact repeated-output collapse when eligible. Chunk dedup becomes eligible
only when the output is large enough and no recency/context-risk signal asks for
full text. The legacy
`codex_chunk_dedup_enabled=true` toggle remains as an explicit override for
conservative policy, not as the normal product path.
`codex_chunk_dedup_proof_level` records the content-free proof level that policy
may trust for automatic promotion (`none`, `unit`, `replay`, or `live`). The
current default is `live` because the release proof corpus contains positive CLI
and Desktop chunk-dedup evidence with zero safety issues. If an operator lowers
that value, `auto` shadows chunk dedup instead of emitting archive-backed
references; `max` still requires at least replay proof unless the explicit
operator override is set.
Runtime demotion inputs also cover quality spikes, archive recovery loops,
missing-tool retries, degraded routes, host-budget pressure, chunk
session-integrity budget pressure, and negative-savings history. Any supplied
demotion signal full-passes the affected managed Codex tool-output reducer and
records the exact content-free reason in mechanism telemetry.
Per-output and cumulative session reference-density caps are enforced as byte
budgets during encoding, not as a crude all-or-nothing rejection. A candidate can
replace repeated chunks only until the remaining budget is exhausted; repeated
chunks past that point stay verbatim in the model-facing output, preserving fresh
recency while still capturing the safe part of the overlap. The cumulative session
budget's denominator counts every observed output sent through the chunk store,
including first-send seed outputs and candidates that produced no accepted
references. Those bytes were visible to the model and therefore increase the safe
budget for later references instead of blocking the first useful overlap hit.
Layer-0 also asks the chunk store for a content-free "budget available after this
candidate" signal before policy evaluation. If the session budget cannot support
another useful chunk reference, the policy full-passes only chunk dedup with
reason `session_integrity_budget`; lossless read-delta and exact repeated-output
reducers remain eligible. This avoids spending hot-path CPU on a recoverable
reference mechanism that the integrity budget would reject while preserving the
safe cache hits.
The store is bounded by `codex_chunk_dedup_max_sessions`,
`codex_chunk_dedup_max_chunks_per_session`, and
`codex_chunk_dedup_ttl_seconds`; the default min block size is 4096 bytes so
auto mode catches Codex's observed ~8 KiB truncated exec-output envelope. It
fails open if archive recovery is unavailable or the token guard is not positive.
WSS Phase-F and HTTP share the same reducer primitives, but only WSS can
currently emit chunk/archive references because it can inject the recovery note
automatically when a reference is actually emitted. For Codex Responses bodies the
note is written to the top-level `instructions` string, never as a `system` item
inside `input`, because Codex's backend rejects `input` system messages. HTTP stays
conservative and does not emit chunk/archive references until that route has its
own proven recovery-note wiring. `/admin/state` reports chunk-dedup hits globally plus
under `proxy_layer0_routes.wss_phasef` / `.http`.
The 2026-05-30 T255 live proof used scoped Codex WSS frames with
`--codex-chunk-dedup --chunk-dedup-min-bytes=0 --fail-on-lost`: replay saved
7757 model-facing bytes with `gate_passed=true`, and live counters reported
1707 billable input tokens saved plus one global and WSS-route chunk-dedup hit
with zero parse, degraded-session, or compression errors. The follow-up T256
policy engine makes that proof usable by default through auto mode instead of
requiring operators to decide when a raw feature flag is safe.

Real log-heavy Codex.app workloads can be handled before chunk dedup by safer
deterministic filters. A 2026-06-02 Desktop proof with two ~40 KB similar log
outputs saved 16,192 billable input tokens through captured-output compaction,
`lost=0`, and zero safety counters; chunk dedup correctly recorded no live block
because the earlier reducer produced a stronger, lower-risk compaction. Replaying
the pre-filter log capture through the chunk store after the line-boundary plan
saved 7155 reducer tokens with 16 archive-backed references and exact
reconstruction. Product interpretation: chunk dedup is a fallback/overlap lever
for large similar outputs that survive stricter deterministic reducers, not a
reason to bypass safer log/search/test parsers.

First-read scan-mode (T253) is retired from Codex runtime. Earlier prototypes replaced
large first file reads with code signatures plus recovery notes. That mechanism is no
longer policy-reachable, has no apply environment flag, and has no live counters in
`/admin/state`. The invariant is enforced by tests: Codex first-read file outputs
full-pass in `auto`, `conservative`, and `max`. If a future design wants similar
savings, it must be rebuilt as a default-safe cache-hit mechanism that does not weaken
first-read information or cannibalize the lossless read-delta/chunk seed.

Model-facing readcache replacements use neutral `[context-* ...]` markers and
preserve the `local-archive://<id>` pattern without naming Slimference inside
tool output. This keeps the mechanical recovery handle while reducing prompt
contamination from product-specific marker text. Archive expansion remains
opportunistic: the proxy can expand a later incoming request that quotes a
stored URI. A neutral once-per-session WSS archive-recovery note exists behind
`archive_recovery_note_enabled`; it is default-off and injects no product name.
When enabled, it tells the model that `local-archive://<id>` may be requested if
full elided content is needed. `read_delta_recent_full_pass_turns` is also
default-off (`0`): operators can raise it after A/B proof to keep immediate
cross-turn re-reads full when recency matters more than dedup savings.
The auto policy adds a stronger runtime guard: after a collapsed key is
deliberately re-read, that key full-passes for the rest of the socket/session so
recency beats further savings. Recently edited reads also full-pass. These are
not manual toggles; they are the central policy's response to model-attention
signals.

`go run ./scripts/utils workday-savings start|finish` is the real-workday
measurement ceremony. `start` captures a baseline from `/admin/state` (and
optionally the filter DB); `finish` captures the current state and prints the
counter delta. Operators must close Codex CLI/Desktop sessions before `finish`
so WSS counters flush. The report keeps route-ready separate from
savings-proven: positive mutation/token counters are the proof, not the fact
that a WSS route was bridged. The same report also keeps the current Codex
route / auto-recert snapshot: auto mode, selected transport, WSS certification,
bridge availability, `needs_recert`, fallback reason, recert status, attempt id,
repair timestamps, last error, and bounded recert log path. Workday windows can
therefore explain whether savings were active, bridged, repaired, or in
fallback instead of only reporting token deltas. The finish report also carries
the host-resource budget snapshot: RSS, CPU-window percentage, disk-write delta,
state bytes, compression/degradation health, status, and reasons. Release proof
uses that snapshot as a promotion gate, because positive savings are not enough
if local resource cost would hurt normal Codex operation.

The 2026-06-02 strict T257 release gate completed with 14 local scoped WSS
captures. The capture matrix covered 9 CLI captures, 5 Desktop captures, all 10
required workload classes, 11 positive live-token-savings captures, 3
expected-zero controls, 43,113 live billable/input tokens saved, `lost=0` in
A/B replay, `captures_with_issues=0`, and safety counters `parse_failures=0`,
`degraded_sessions=0`, `compression_errors=0`. The clean CLI workday window
saved 372 billable
WSS-input tokens on an archive-backed `git status --short` workload with
`phasef_bridged=1`, `compressed_messages_mutated=1`, `frames_reencoded=1`, and
zero parse, degraded-session, or compression errors. The clean Desktop workday
window saved 382 billable WSS-input tokens on an archive-backed `rg -n TODO`
workload with `phasef_bridged=2`, `compressed_messages_mutated=1`,
`frames_reencoded=1`, and zero parse, degraded-session, or compression errors.
This proves representative CLI/Desktop WSS savings breadth for deterministic
reducers. The 2026-06-02 strict matrix additionally covered repeat reads,
ranged reads, search loops, git-status compaction, apply-patch/read safety,
changed-file safety, similar-file safe-zero behavior, test-failure safe-zero
behavior, no-savings controls, and a mixed Desktop workday through the same
product path. The mixed Desktop row alone saved 8,394 live billable/input
tokens with `read_delta=1`, `captured_output=1`, `codex_exec_envelope=1`,
`lost=0`, and zero safety errors. In this strict release proof,
`repeated_output` and `chunk_dedup` did not record live block hits and are not
part of the 43,113-token claim. The similar-files capture stayed expected-zero /
net-negative for default-auto.
After the session-budget denominator fix, the real CLI chunk probe capture
`chunk-live-cli-similar-output-20260602T150301.jsonl` replays through default
auto with `reducer_tokens_saved=6636`, `reducer_chunk_dedup_blocks=1`,
`reducer_chunk_dedup_references=4`, `bytes_saved=32195`, and `gate_passed=true`.
That is a reducer/replay proof on real WSS frames, not yet a fresh live-token
matrix claim.

`go run ./scripts/utils wss-audit` also reports a content-free re-read canary:
the number of WSS request summaries that repeated a resolved read/tool key and
the total repeated count. A non-zero canary is not automatically bad, because
repeat reads are also the highest-value savings workload. It is the live signal
to compare with positive savings, recent-edit guards, and future comprehension
A/B results when deciding whether a session needs looser compression. On the live
WSS reducer, a post-collapse deliberate re-read of the same read key suppresses
further collapse for that key for the rest of the session, restoring full recency
instead of fighting the model's attention signal.

Savings-proven and comprehension-preserved are intentionally separate claims.
Positive mutation and input-token counters prove Codex WSS savings for a
workload. They do not, by themselves, prove that every future response preserves
model comprehension. Deterministic reducers stay guarded by reconstruction,
token-decrease checks, recent-edit observations, content-free canaries, and
byte-equal fail-open; broader semantic compression or archive-instruction
recovery requires separate fixture and live proof before default-on promotion.
`internal/proxy.RunWSSPhaseFABReplay` is the offline bridge for that proof: it
replays decompressed Codex WSS frames through the real Phase-F reducer, extracts
the direct and compressed model-facing request context, and feeds both into
`internal/abharness.Compare`. The CI-covered fixture proves repeat-read
read-delta is recoverable because the first full read was already sent, and that
the recovery note is visibly audited as an expected extra model-facing context
change when a recoverable-reference mechanism needs it.
The replay extractor includes Codex Responses top-level `instructions` as
model-facing system context, so recovery-note or output-reduce hint injection
cannot bypass the no-drawdown comparison just because it does not live in
`input`.
Known output-reduce directive suffixes are audited separately as expected
instruction extras: the direct instructions must remain a prefix, the suffix
must contain the output-reduce marker, and unknown instruction rewrites still
count as lost context under `--fail-on-lost`.
The harness aligns inserted recovery-note/system blocks before comparing changed
tool-output blocks, so note insertion cannot create false content-loss findings.
For chunk dedup, the harness expands every `[context-chunk ... local-archive://...]`
reference and compares the reconstructed block to the exact direct model-facing
source; a URI by itself is not enough to pass the no-loss gate.
`go run ./scripts/utils wss-ab-replay <frames.jsonl> [--json|--fail-on-lost|--archive-recovery-note|--codex-chunk-dedup]`
is the operator-facing report wrapper. With default config it runs the same
`auto` policy as the product path, including T255 when the capture presents a
safe recoverable candidate. `--codex-chunk-dedup` remains a force flag for
threshold experiments; it implies the recovery note and separates the expected
once-per-session recovery-note extra block from true loss-gate failures. The
report separates two concepts: `bytes_saved` is the comprehension A/B byte delta
after archive expansion and note alignment, while `reducer_tokens_saved` and the
`reducer_*` mechanism counters report actual model-facing compressed request
savings from the Phase-F reducer. Its JSONL input is content-bearing by
definition, so it belongs in local/private
captures only; it does not read auth headers or WebSocket upgrade metadata. Each
replay uses an isolated temporary home directory so disk-backed
readcache/tooluse/archive state from prior live sessions cannot skew the A/B
result.
Set `SLIMFERENCE_WSS_AB_CAPTURE=/private/path/frames.jsonl` on the Slimference
daemon process to append those replay frames during a scoped Codex WSS session.
The capture hook records only decompressed JSON frame payloads and direction,
before any Phase-F mutation; it is disabled unless the env var is set.
`slimference start` preserves the caller environment when spawning the detached
daemon, so this capture env var works through the normal lifecycle command.

---

## 5. Layer 1 - Deterministic Compression

`internal/compression/layer1.go::DeterministicCompressor.Compress`
orchestrates 15 sub-layers. Execution order per spec+.md §5 plus the T143
semantic frontier:

| # | Sub-layer                          | File                           |
|---|-------------------------------------|--------------------------------|
| 1 | ANSI / control-char strip          | `ansi_strip.go`                |
| 2 | JSON minify                        | `json_minify.go`               |
| 3 | Comment strip (38 path languages)  | `comment_strip.go`             |
| 4 | Exact dedup + archive-backed near dedup | `dedup.go` + `dedup_minhash.go`|
| 5 | Structure extraction               | `structure.go`                 |
| 6 | Delta encoding (LCS unified diff)  | `delta.go`                     |
| 7 | Tool classifier                    | `tool_classifier.go`           |
| 8 | Tool-type-aware compression        | `tool_compressor.go`           |
| 9 | Success-shortcircuit               | `success_shortcircuit.go`      |
|10 | Image-block replace                | `image_replace.go`             |
|11 | Reversible path dictionary         | `semantic_dictionary.go`       |
|12 | Repeated-line collapse             | `repeated_collapse.go`         |
|13 | File-op graph pruning              | `graph_pruning.go`             |
|14 | Prefilter tag                      | `prefilter_tag.go`             |
|15 | Loop nudge (T37)                   | `loop_detect.go`               |

Plus:
- Structure-aware preview for oversized tool results (T38 / T55).
- Prompt-cache breakpoints (T23, T45).

### Layer 1 safety registry

`compression.Layer1SubLayerRegistry()` is the control-plane contract for Layer 1.
It classifies each sub-layer as exact, reversible, recoverable-with-archive,
task-preserving summary, or non-default. The registry also records default
eligibility, whether an archive is required, the model-risk being controlled,
and the recovery path. The executor enforces that contract for archive-required
mutations: if the original block cannot be archived and stamped with a valid
archive id, the block full-passes and its per-block savings counters are reset.
Exact and reversible transforms can stay automatic; context-dropping summaries
must stay archive-backed or be bypassed. Exact dedup remains reversible without
an archive because the referenced block was already model-facing in the same
context. MinHash/near-dedup is not exact and is therefore classified separately
as `dedup_near`: it full-passes unless the current omitted block is archived and
stamped, and its decision telemetry is reported separately from exact `dedup`.
The same archive-required rule applies to side paths such as
`structure_in_window`: even when an in-window tool-result block is eligible for
structural extraction, the original must archive successfully before the summary
can replace model-facing text.
Success short-circuit summaries follow the same rule: a verbose success-only
tool output can become an `[ok]` marker only when the original output has been
archived, so the marker never becomes an unrecoverable source of truth.

Every Layer 1 compression call also emits content-free `layer1_decisions`
telemetry in the proxy decisions log. Each record names the sub-layer, safety
tier, attempted flag, applied flag, reason, saved-token count, archive
requirement, recovery path, archive-write count, and default eligibility.
`attempted=false` means the workload never reached that sub-layer's reducer,
not merely that it saved zero tokens. Archive-write count is incremented only
after the recorder returns a non-empty archive id, so an archive-required
positive decision can prove that recovery material was actually written. This is
an audit/control surface only: it does not add model-facing text and does not
change compression output. It lets proofs separate "not attempted",
"attempted but not applicable", "full-passed because archive recovery was
unavailable", and "applied with positive savings and a concrete recovery
record" per sub-layer.
The near-dedup regression fixture uses `DiskRecorder` plus
`contentarchive.Get` to prove that a similar-but-changed omitted block expands
back to exact original bytes. A broader Layer-1 corpus guard exercises multiple
historical messages and archive-backed mutations, then expands every emitted
archive id and verifies exact original block bytes plus session, message, block,
and sub-layer metadata. This is the reconstruction boundary for default Layer 1:
future archive-required sub-layers must pass the same proof shape before they
can run default-auto.

Layer 1 also respects provider-cache boundaries. Any content block that already
carries `cache_control` is skipped by the Layer-1 block mutators, even if a
local sub-layer could save a few tokens. This prevents local compression from
rotating or weakening a provider-cached stable prefix and keeps prompt-cache
economics ahead of small local wins.

### Reversible path dictionary (T143a)

`semantic_dictionary.go` aliases repeated absolute local paths inside one
tool-result block only when the embedded legend plus aliases are strictly
shorter. It preserves reversibility by prepending a small dictionary such as
`[P1]=/Users/.../file.go`, then replacing repeated body occurrences with
`[P1]`. The marker is neutral (`[path dictionary]`) and product-name-free so the
model reads it as tool-output notation rather than a third-party speaker. It is
deliberately narrow: known local filesystem roots only,
minimum path length and occurrence gates, URL-style paths ignored, and no
application when the legend would create a negative saving.

### Structure extraction frontier (T143b)

Structure extraction now covers the main code stacks plus high-volume text and
config formats. `structure_more.go` adds Markdown, SQL, GraphQL, HCL,
Dockerfile, and Makefile summaries on top of Go, TypeScript/JavaScript,
Rust, Python, C/C++, Java, Ruby, shell, Zig, Swift, Kotlin, PHP, Dart,
Scala, Elixir, Solidity, and Svelte.

The new text/config summaries are deliberately lossy but recoverable through
the existing content archive. They keep only structural markers: Markdown
headings/lists/tables/fences, SQL DDL/DML/constraint clauses, GraphQL/HCL
top-level blocks, Dockerfile image/control instructions with `RUN` chains
collapsed to a command count, and Makefile includes/variables/targets.
The `structure_min_tokens` gate is evaluated with the local tokenizer, falling
back to byte/4 only if tokenizer initialization fails. Negative-saving bypass
still applies before any compacted block is used.

### Semantic test-failure compaction (T143d)

`stacktrace_compact.go` runs inside the existing L1 tool-output compressor for
large `ToolTypeTestOutput` blocks that actually look like stack traces. It keeps
failing test anchors, assertion/diff lines, top application source frames, and
package/status summaries, then collapses framework/vendor frames and excessive
diff/context lines with explicit omitted-count markers. The existing
shorter-than-original guard still decides whether the compacted block is used.

### Layer 2 task-shaped contracts (T144a)

Before any configured Layer 2 provider sees a summarization request,
`prompt_contract.go` classifies the transcript as coding, debugging, review,
planning, documentation, live E2E, or generic. The selected contract is appended
to the model-agnostic system prompt and preserves exact paths, commands,
failures, decisions, uncertainty markers, and no-prose bullet format according
to task shape. `validator.go` also rejects summary file paths that are absent
from the summarized source slice, with normalization for relative/absolute path
variants.

### Adaptive dedup staircase (T53)

Jaccard threshold lowers as the conversation grows
(`[compression.tuning.dedup_staircase]`):

| Messages | Threshold | Rationale                                 |
|----------|-----------|-------------------------------------------|
| 0-10     | 0.88      | Short session; tighter to avoid collapse. |
| 11-20    | 0.85      | Pre-T53 default.                          |
| 21-40    | 0.82      | Near-duplicates accumulate.               |
| 41+      | 0.78      | Long session; aggressive dedup pays off.  |

Empty staircase or invalid step falls back to
`Compression.DedupSimilarityThreshold` scalar.

### Tool-compressor tuning (T61)

RTK-inspired heuristics now live in
`[compression.tuning.tool_compressor]`:

| Field                          | Default |
|--------------------------------|---------|
| `aggressive_after_multiplier`  | 2       |
| `git_moderate_diff_limit`      | 60      |
| `test_max_failure_lines`       | 40      |

`SetToolCompressorTuning` installs these at proxy boot; zero/negative
fields fall back to the compile-time defaults.

### Structure preview (T38 / T74 / T76)

`[compression.tuning] structure_preview = true` is the default after T76's
content-archive foundation. Oversized tool_result blocks with JSON /
path-list / ASCII-table shape can be replaced with a compact, shape-aware
preview when strictly shorter, while archive-backed recovery keeps the
original body locally retrievable.

---

## 6. Layer 2 - OCRL Context Ledger and Background Summarisation

Layer 2 is no longer treated as "summary as truth" for product defaults. The
product direction is OCRL, the Old Context Replacement Layer documented in
`docs/ocrl.md`. OCRL is deterministic old-context replacement: it can render
archive-backed capsules only when current-session provenance, inactive-context
selection, archive recoverability, route eligibility, and positive token
accounting are all proven. Any missing gate full-passes the original context.

`internal/contextledger` builds deterministic command, file, search, failure,
decision, and recovery capsules as compact provenance facts with hashes and
archive ids. `internal/contextledger/ocrl.go` adds the route-gated OCRL engine:
`off`, `shadow`, `auto`, and `max` modes; Codex WSS stays shadow-only; only
full-history HTTP-style routes are model-facing eligible; archive availability
is verified before rendering; and net savings must remain positive after
capsule and recovery overhead. When an exact old-context slice is not available
yet, OCRL shadow telemetry can count original tokens from archive payloads and
report those numbers as would-save proof only. The Codex Layer-0 reducer now
retains the actual capsule objects in its internal stats path, not only
counters, so future OCRL promotion can use real provenance objects instead of
re-parsing telemetry. Codex WSS request summaries now include content-free OCRL
shadow fields (`ocrl_mode`, `ocrl_route`, `ocrl_reason`, candidate counts,
archive expansion count, original archive tokens, replacement tokens, and
would-save tokens). Those values never change model-facing context and never add
to product `net_tokens` while the route remains shadow-only.

`internal/contextledger/message_apply.go` is the exact full-history message
apply primitive. It accepts only explicit message/block targets paired with
capsules, runs the normal session, active-path, recent-turn, quality-pressure,
route, archive, and token gates before mutating anything, and verifies only the
targets actually selected for replacement. Each selected target capsule must
have exactly one archive id and the archived payload must be byte-equal to the
current target block text. Final savings accounting counts only selected
targets, not verbatim or rejected candidates, and includes covered-marker
overhead. Invalid selected targets, duplicate selected targets, archive
mismatch, shadow mode, Codex WSS, and non-positive selected-target savings all
full-pass the original messages. Verbatim/rejected targets remain original and
cannot block other independently safe old-context replacements.

OCRL is operator-visible and configured under `[compression.ocrl]`. Fresh
configs default to `mode = "shadow"`, `max_capsules = 512`,
`min_net_saved_tokens = 1`, and `max_replacement_tokens = 0`. Env overrides are
`SLIMFERENCE_OCRL_MODE`, `SLIMFERENCE_OCRL_MAX_CAPSULES`,
`SLIMFERENCE_OCRL_MIN_NET_SAVED_TOKENS`, and
`SLIMFERENCE_OCRL_MAX_REPLACEMENT_TOKENS`. `slimference layer2 status` prints
the effective OCRL policy and the Codex WSS shadow-only route guard, so the
deterministic replacement layer is visible without enabling legacy summaries.

`internal/summarization/layer2.go` still exists as a background optimizer. Its
local fallback is deterministic extractive summarisation, and an
OpenAI-compatible `/v1/chat/completions` endpoint can still be configured for
higher semantic compression. `[compression.minimax]` is now a historical
section name: `base_url`, `model`, and `api_key_env` can point at another
compatible provider without code changes.

Ledger capsule selection is fail-closed. A model-facing OCRL path may select
only archive-backed old context from the current session. Active turns, recent
turns, high-risk failures, missing provenance, missing facts, wrong-session
capsules, missing archives, and budget-overflow candidates stay verbatim or
rejected. `SelectionPolicy.ActivePaths` also keeps any file, search, or
decision capsule touching an actively worked file verbatim, including
repo-relative search hits resolved against the capsule `repo_root`.
`SelectionPolicy.QualityPressure` full-passes every capsule when re-read,
recovery, or comprehension canaries report pressure.
Full-history message application can derive explicit OCRL targets only by exact
archive-to-message matching: one capsule archive payload must equal exactly one
current message block. Ambiguous, duplicate, missing, errored, or unmatched
candidates are omitted and reported instead of guessed. Target bookkeeping uses
compact numeric keys instead of formatted strings, and explicit archive payload
checks compare bytes to current message text without allocating converted
payload strings.

Focused verification on 2026-06-05:

- `go test ./internal/contextledger -count=1`
- `go test ./internal/abharness ./internal/contextledger -count=1`
- `go test ./internal/proxy -run 'TestApplyProxyLayer0Ledger|TestProxyLayer0Ledger|TestApplyProxyLayer0Branches' -count=1`
- `go test ./internal/contextledger -bench='Benchmark(BuildOCRLReplacement|DeriveOCRLMessageTargets|ApplyOCRLToMessagesByArchiveMatch)' -benchmem -run '^$'`

The latest OCRL benchmark on Apple M1 processed 512 file capsules in about
1.986 ms with 238096 B/op and 11 allocs/op after archive verification and
renderer scratch-buffer reuse. Exact archive-to-message target derivation for
512 capsules measured about 0.936 ms with 186211 B/op and 22 allocs/op; full
archive-match OCRL apply measured about 4.273 ms with 1114840 B/op and 1086
allocs/op.

The live-corpus gate now has an OCRL-aware validator. `ocrl_full_history`
requires applied full-history OCRL evidence, selected capsules, archive
expansions, positive OCRL saved tokens, and no shadow-only rows. The committed
`synthetic_ocrl_full_history` fixture proves the gate wiring only; it is not a
real Codex CLI/Desktop promotion claim. The stricter `benchmark-corpus
--maxx-check` also requires a real, non-synthetic `ocrl_full_history` workload
and repeats the applied/full-history/archive/savings/no-shadow checks before
the whole max-out program can pass.

The content archive exposes `Peek` for shadow/proof paths. Unlike `Get`, it
loads the exact archived payload without incrementing real expansion counters,
so OCRL proof telemetry cannot pollute recovery metrics.

### Decision rule (T54)

```
if tokens < min_tokens_for_layer2:       skip "below_threshold"
if budget_ms > 0 and projected > budget: skip "latency_budget"
else:                                    run
```

Where projected latency = EMA(observed Layer 2 latencies) ×
`layer2_latency_projection_multiplier`. The EMA seeds with 400 ms so
the guard is conservative on a cold start.

Default `min_tokens_for_layer2 = 15000` (was 30 k pre-T54). The
latency-budget guard is opt-in; `layer2_latency_budget_ms = 0`
disables it.

Current default state: fresh configs keep Layer 2 disabled. Operators opt in
with `slimference layer2 enable --acknowledge-data-policy` or an explicit
`layer2_enabled = true` config. Even when Layer 2 is enabled, model-facing
classical summary replacement stays blocked unless the explicit legacy override
`[compression.summary].allow_model_facing_replacement = true` is configured.
The inverse is also true: the legacy override alone is insufficient while
`layer2_enabled` is false, including overflow recovery's cached-summary retry.
The first interactive startup with Layer 2 enabled records an explicit acknowledgement under
`~/.slimference/policy/layer2-default-on-ack.json`; non-interactive startup
warns without blocking. `slimference layer2 acknowledge` records the marker
manually, and `slimference layer2 status` prints the ack state. The legacy
override is not the product direction; default-safe Layer 2 requires
proof-gated ledger insertion with archive expansion, current-session scope, and
no unresolved archive ids. Even under the explicit legacy replacement override,
cached summaries are applied only when the caller has a non-empty session id,
the summary text is non-empty, the cached prefix hash still matches the live
conversation prefix, and the cached token accounting is a positive saving. The
old sessionless `ApplyToMessages` compatibility wrapper therefore full-passes;
model-facing replacement requires `ApplyToMessagesSession`.

T152 hardens Layer 2 as a background-only optimizer. After the active
request completes, `ScoreBackgroundCandidateSession` checks provider
availability, compressible-prefix size, recent edit/error anchors,
projected savings, and existing summary coverage. Eligible jobs enter
the bounded `compressQueue` with a session candidate hash. The worker
drops stale hashes before running the summariser. Product callers invoke cached
replacement only when Layer 2 is enabled, the legacy model-facing replacement
gate is enabled, and the cached summary hash still matches the live covered
prefix. Gate disabled, hash mismatch, stale worker job, provider failure,
timeout, validation failure, and anchor-loss validation all fail open to the
original context. Telemetry is exposed at `/admin/status.layer2.cache_stats`.

Layer 2 caps oversized message text before outbound redaction/rendering, then
caps the formatted summariser input again before preprocessing or density
scoring. Both caps keep the newest text inside the 120k-token quality window,
respect the same CJK-heavy token heuristic used by `estimateTokens`, and trim
only on UTF-8 rune boundaries. Adaptive target tokens are computed from the
actually submitted text, not the pre-cap message slice. This prevents huge
historical reads from burning CPU or timing out before the provider call while
leaving the original message slice intact for hashing, anchors, and covered
range validation.
When `outbound_redaction=off`, the redactor returns the original message slice
unchanged instead of deep-copying it; Layer 2 callers treat that slice as
read-only. Default and strict redaction modes still copy before mutation.

Provider/runtime knobs:

- `SLIMFERENCE_MINIMAX_BASE_URL`, `SLIMFERENCE_MINIMAX_MODEL`, and
  `SLIMFERENCE_MINIMAX_API_KEY_ENV` override the summariser endpoint,
  model, and secret env var for fast provider swaps. Names are legacy
  compatibility, not a default product claim.
- `SLIMFERENCE_MINIMAX_API_KEY` is a direct key override and switches
  `api_key_env` to itself instead of being silently ignored.
- `temperature` defaults to `0` and `top_p` to `1` for deterministic
  compression. Both are now honoured in the outbound request.
- `enable_reasoning_split = true` is default for MiniMax M2.x so
  thinking content is returned outside `message.content`; set
  `SLIMFERENCE_MINIMAX_ENABLE_REASONING_SPLIT=false` for non-MiniMax
  compatible endpoints that reject this extension.

### Operating modes (T36)

`[compression.summary] mode = strict | balanced | fast`:

- `strict`: lowest compression ratio, highest fidelity.
- `balanced`: middle ground (default).
- `fast`: aggressive, lowest latency.

Explicit numeric overrides in the same block take precedence; the mode
fills unset fields from a coherent bundle.

### Hierarchical context capsules (T153)

`internal/summarization/capsules.go` provides deterministic, reversible
context capsules for future T149 selection. `ContextCapsule` records the
tier (`micro`, `phase`, `session`), source range, token accounting,
anchor indices, validation state, summary text, and content-archive URIs.
Micro capsules cover large non-anchor tool results. Phase capsules cover
inactive task slices split on user task/next/fix/plan boundaries. Session
capsules compose old validated phases. Any range containing edit/error/
decision/config anchors is skipped so critical material remains verbatim.
Every capsule is archive-backed and can be expanded with the existing
`slimference expand <local-archive-id>` path.

### Proxy-visible read/file deltas (T154)

T154 moves the read-cache/delta win from hook-only file reads into proxied
request history. When a tool result contains a concrete file-read command and
the request carries a stable session id, `readcache.EvaluateObserved` hashes
the observed content, archives the full text through `internal/contentarchive`,
and updates the session-scoped read entry before Layer 0 proxy compaction.

The first full read is allowed through, with any normal deterministic Layer 0
compaction still available. An unchanged reread becomes a stable reference to
the archived full content. A changed reread becomes a textual delta only when
the delta plus archive reference is shorter than the current full content.
Unknown sessions, missing archives, non-shorter deltas, unknown paths, and
recent same-session edits fail open to the original content. Recent-edit
detection uses the file-backed hook turn state so an edit/apply-patch event
keeps the next read verbatim instead of hiding fresh code behind a delta.

### Incremental staircase (T27)

For iterative summaries (same session, new tail), a staircase governs
the range-overlap threshold. Identical structure to T53's dedup
staircase.

### Tool-priority staircase (T26)

Some tool-result types are more valuable than others
(`error` > `decision` > `edit` > `config` > `generic`). The summariser
biases towards preserving high-priority content.

---

## 7. Layer 3 - Response Cache

`internal/caching/response_cache.go` is an LRU keyed by the SHA-256 of
the provider, HTTP method plus route path/query, request-affecting Slimference policy
partition, canonical request body, and pertinent headers. Hits skip Layer 1 and
Layer 2 entirely and serve the cached upstream response. Method and route keying
are deliberate: the same body on two methods or provider endpoints cannot alias. Policy keying
is equally deliberate: local response-cache hits do not cross request policies
such as stop-sequence injection or be-terse treatment cohorts, because those can
change the upstream model response even when the user body is identical.

The local response cache only serves explicitly deterministic, non-tool request
shapes. Missing sampling fields are treated as provider defaults, not as proof
that replay is safe. Streaming, non-zero temperature, top-p sampling, multiple
completions, tool definitions, explicit tool choices, function-call fields,
tool/function roles, and Responses function-call outputs all full-pass upstream.
Routes that can create or continue upstream server state are also fail-closed:
Responses requests must explicitly set `store:false`, and any
`previous_response_id`, conversation, thread, assistant, nested metadata
session/conversation/thread/assistant marker, or Codex turn-metadata marker
full-passes upstream. Local replay is allowed only for stateless deterministic
requests, because skipping upstream response-id creation or conversation-state
updates would change workflow state even if the visible text matched.
This keeps Layer 3 from replaying a cached tool workflow or a fresh model sample
where timing, tool state, or stochasticity matters.

Provider-cache accounting is intentionally split by provider shape. Anthropic
`cache_read_input_tokens` and `cache_creation_input_tokens` are recorded as
provider-cache read/create tokens. OpenAI and Codex `cached_tokens` are treated
as provider-cache read tokens for product/admin savings, while debug flight
records preserve the provider-specific cached-input shape so reports can avoid
double counting. HTTP and WSS `response.completed` usage both feed the same
admin-state savings rollup. Provider-cache numbers remain separate from local
response-cache hits and output-wire savings.

Output-reduce proof runs can override the runtime profile without editing the
config file via `SLIMFERENCE_OUTPUT_REDUCE_PROFILE` and
`SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS`. These env overrides are scoped to
the daemon process that receives them; they do not change product defaults.

### Invalidation

`internal/caching/file_watcher.go` watches every file referenced by
recently-cached tool calls (via fsnotify). A write invalidates every
cache entry whose key was computed from a body mentioning that path.

### Prompt-cache breakpoints (T45)

`internal/compression/prompt_cache.go::OptimizeCacheBreakpoints` places
up to 4 `cache_control: {type: "ephemeral"}` markers on the messages of
the stable prefix when the prefix is at least 1024 estimated tokens.
Breakpoints are selected by expected cache value: large stable
`tool_result` blocks first, then late stable user/assistant/tool turns,
with deterministic tie-breaking. The caller-owned message slice is not
mutated. This keeps Anthropic cache hints on stable content while avoiding
cache-control overhead on tiny one-shot requests.

Cumulative injection count:
`/admin/status.prompt_cache.breakpoints_injected_total`.

### Double-keyed pre-compress lookup (T20)

Cache is consulted *before* running Layer 1 (key = SHA256 of original
body) and *after* Layer 1 (key = SHA256 of compressed body). A pre-L1
hit avoids the pipeline entirely; a post-L1 hit short-circuits the
upstream call.

### Response-cache TTL

Default 5 min; configurable via `[cache] response_cache_ttl_seconds`.

---

## 8. Provider Support

`internal/types/types.go::Provider` has three values:

```go
Anthropic       // /v1/messages
OpenAI          // /v1/chat/completions (plain API)
CodexChatGPT    // /backend-api/codex/* OR any /v1/* with "codex" in UA
```

### Detection (T66)

`internal/proxy/provider.go::detectProviderWithUA`:

1. Path contains `/backend-api/codex/` → CodexChatGPT.
2. Path contains `/messages` → Anthropic.
3. User-Agent contains "codex" → CodexChatGPT (catches Codex's
   `/v1/responses` endpoint via `openai_base_url`).
4. Path contains `/chat/completions` → OpenAI.
5. Body probe: `max_tokens` + not `frequency_penalty` → Anthropic.
6. Fallback: OpenAI.

### Upstream routing

Config: `[upstream.*] base_url`:

| Provider      | Default                    | ENV override                                       |
|---------------|----------------------------|----------------------------------------------------|
| Anthropic     | `https://api.anthropic.com`| `SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL`          |
| OpenAI        | `https://api.openai.com`   | `SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL`             |
| CodexChatGPT  | `https://chatgpt.com`      | `SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL`      |

### Header forwarding

All headers flow through verbatim except `Host` (rewritten to upstream
authority). `Authorization`, `User-Agent`, `OpenAI-Beta`, and `Cookie`
are preserved so Cloudflare + upstream see the same identity they
always see from this user. The proxy does not add an upstream-identifying
header.

### Codex request-body compression (T73)

Codex support is not only routing. Known Codex request shapes now enter
the same Layer 1-3 compression path:

- OpenAI-style `messages` bodies are parsed through the existing OpenAI
  normalizer.
- Responses-style `input` arrays map `message`, `function_call`,
  `function_call_output`, local-shell call/output variants, direct
  `command`/`args` arrays, `cmdline`/`shell_command` aliases, read path
  aliases, `aggregated_output`, `stdout`, and `stderr` into `types.Message`.
- Codex CLI exec envelopes (`Chunk ID`, exit code metadata, `Output:`)
  are treated as transport metadata: Layer 0 compacts the payload after
  `Output:` and preserves the header.
- `/v1/responses` is considered compressible only after User-Agent/body
  detection classifies it as `CodexChatGPT`; generic OpenAI Responses
  traffic remains passthrough.
- `/backend-api/codex/*` is a potential Codex compression path, but unknown
  body shapes return to byte-equal passthrough.
- Rebuild preserves body-level fields such as `conversation_id`, `metadata`,
  `stream`, and `store`, plus auth/session headers in the forwarded request.

### Server-side state lever (T78)

`[proxy] server_state_enabled = true` activates the T78 lever for
providers whose capability map sets `SupportsResponseID = true`
(OpenAI, CodexChatGPT). On follow-up turns the proxy:

1. Pulls a session key from the body (OpenAI:
   `metadata.session_id` → `metadata.conversation_id` →
   `previous_response_id`; CodexChatGPT: top-level `conversation_id`).
2. Looks up the last upstream response id stored for that session.
3. Rewrites the request: history collapsed to the last user turn,
   `previous_response_id` injected.
4. Captures the upstream response id from the non-streaming response
   body and stores it for the next turn.
5. On a 4xx whose error mentions `previous_response_id`,
   `response not found`, or `conversation not found`, forgets the
   anchor, retries the original full body once, and continues.

Anthropic stays untouched (capability says no). Default off so traffic
shape stays identical until you opt in. Counters at
`/admin/status.server_state.{sessions,skip_total,recover_total}`.
Streaming response-id capture is deferred — SSE replies do not yet
seed the next-turn anchor.

### OpenAI prompt-cache hints (T136)

`[proxy.openai_prompt_cache]` is an opt-in request-hint layer for generic
OpenAI API traffic. When enabled, Slimference first builds a stable-prefix
plan from `messages` / Responses `input` arrays plus stable top-level fields
(`instructions`, `system`, `developer`, `tools`). Only content before the
final user turn is eligible; the latest user turn is excluded so normal prompt
edits do not rotate the cache key. The hint gate uses stable-prefix tokens,
not whole-request tokens, so one-turn requests do not pay cache-hint overhead
just because the latest prompt is large.

Generated `prompt_cache_key` values are privacy-safe hashes over session/model
strategy plus the stable-prefix hash. They rotate when the stable prefix or
tool schema changes, but they never contain raw prompt text or full local
paths. Existing caller-owned fields are preserved, and a per-key rate cap
disables the hint before it can create high-cardinality cache churn. If OpenAI
rejects the fields with a relevant 4xx response, the proxy retries once
without those hints while preserving any server-state rewrite. Debug/flight
telemetry records only content-free fields: applied/reason, retention,
stable-prefix token estimate, and stable-prefix hash.

CodexChatGPT backend routes do not receive these fields until T140 captures
live request acceptance.

`slimference gain --proxy` includes a content-free prompt-cache heat map grouped
by stable-prefix hash. Each row records request count, hint applied/skipped
counts, maximum stable-prefix token estimate, provider cached tokens, provider
cache read tokens, and cache create tokens. JSON exposes `prompt_cache_heat`,
CSV exposes heat-key count plus top hash/cached-token totals, and text output
prints the hottest five hashes. These rows explain cache behavior; provider
cache credits remain provider/accounting evidence, not claimed local deletion.

### Reversibility-by-default (T76)

Layer 1 archives original block content via `internal/contentarchive`
before any lossy mutation; the rewritten block carries an `archive_id`
reference and the proxy opportunistically re-injects archived content
when a follow-up request quotes a `local-archive://<id>` URI. T76
lets `structure_preview` ship default-on (T74 risk closed) and
unblocks T100 cross-direction coordinator + T103 tool-definition
pruning. Telemetry: `/admin/status.content_archive`.

### Quality calibration loop (T77)

Three lightweight signals run alongside existing analytics, none of
them require an LLM round-trip: a per-session re-read detector, a
rolling prompt-cache hit-ratio drop alarm, and a net-savings tally
that subtracts an estimate of cache-invalidation cost from raw
savings. Surfaced via `/admin/status.quality`.

### Cross-direction L1/L2 coordinator (T100)

`[compression.tuning] coordinator_enabled` lets Layer 1 skip heavy
sub-layers on the prefix that Layer 2 will summarise. Cheap passes
(ANSI strip, JSON compact) always run. Default off until corpus data
validates the trade-off. Skipped-block counter at
`/admin/status.coordinator.skipped_total`. HTTP request handling passes this as
request-scoped `Layer1CompressOptions`, not as shared compressor state, so
parallel requests cannot inherit each other's coordinator policy.

### L1 message-level fan-out (T104)

`[compression.tuning] coordinator_parallel` runs `compressMessage`
concurrently per message in the compressible prefix, bounded by
`runtime.GOMAXPROCS(0)`. The `archiveOriginal` recorder is mutex-
protected, the `coordinator_skipped` counter is atomic, and the receiver-local
session/coordinator fields are serialized per Compress call so the hot path stays
race-clean. Default off until benchmarks show real-body wins. Note: shipped at
message granularity, not the
spec's stage-partitioned sub-layer concurrency; reopens as T104b
if message-level granularity turns out to be the wrong knob.

### Mid-exchange summary (T99)

`[compression.tuning] mid_exchange_enabled` activates an
in-progress summary block when the current exchange exceeds
`mid_exchange_threshold_tokens` (default 10000). Detection looks
for completed tool-use cycles (`assistant[tool_use]` ->
`user[tool_result]` -> `assistant`) inside the live exchange and
collapses the range to `[in-progress summary, anchor=msg #N]`.
Because this is model-facing context replacement, it is blocked unless Layer 2
is enabled and `[compression.summary].allow_model_facing_replacement = true` is
explicitly set. Default off. The product direction remains deterministic context
ledger shadowing, not summary-as-truth.

### Layer 4 tool-definition pruning (T103)

`[compression.tuning] tool_prune_enabled` activates the per-session
tool-usage tracker + body-rewrite pass. Tool definitions idle
beyond `tool_prune_idle_threshold_turns` (default 20) are removed from `tools[]` for Anthropic
(`tools[].name`) and OpenAI / CodexChatGPT (`tools[].function.name`
or top-level `tools[].name`). Telemetry at
`/admin/status.tool_prune.{sessions,pruned_total,reattach_total,
miss_total,retry_total,always_keep_total,disabled_sessions,
tokens_saved_sum}`. Default off.

T151/T268 make the pruner soak-safe enough for wider testing: shell,
edit, read, safety, browser, and MCP tool classes are always kept, and
`tool_prune_always_keep = []` can add project-specific exact tool names with
case-insensitive matching.
Focused tool-heavy proof runs can enable the pruner without editing the config
file via `SLIMFERENCE_TOOL_PRUNE_ENABLED=1`, shorten the proof-only idle window
via `SLIMFERENCE_TOOL_PRUNE_IDLE_THRESHOLD_TURNS=1`, and provide
comma-separated project keeps via `SLIMFERENCE_TOOL_PRUNE_ALWAYS_KEEP`.
The Codex WSS Phase-F path uses the same strict pruner for prompt/user-turn
request bodies. WSS tool-call frames feed tool-name usage into the session
tracker, but actual `tools[]` mutation only happens on prompt/user turns with a
known Codex tool schema. Unknown or mixed schemas stay byte-equal.
Pruned definitions are archived by session and tool name. A later tool-name
mention, safe alias (`GetWeather` -> "weather", `send_email` -> "email"), or
command-family hint in current user/system/developer instruction text reattaches
the definition before pruning runs again. Historical assistant text and tool
stdout/stderr do not trigger reattach, so old logs cannot silently add schema
tokens back to a later unrelated turn.
Reattached definitions are appended in deterministic tool-name order to avoid
avoidable prompt-cache churn, and the reattached tool names count as active for
the same prune decision so the idle pass cannot immediately remove the recovered
tool again. Reattach is schema-safe: cached pruned definitions are only consumed
after a successful safe reattach, and malformed or unnameable existing `tools`
entries full-pass unchanged instead of being rewritten. If the upstream returns
a conservative missing-tool 4xx, including common provider phrasings such as
unknown tool, no tool named, not in available tools, tool/function not found, or
not a valid function, the proxy retries once with the full pre-prune schema,
records miss/retry telemetry, and disables future pruning for that session
bucket. `slimference
gain --proxy` includes tool-prune saved-token, pruned-tool, reattach, miss, and
retry totals from the decision log.

### Posttool cross-session repetition marker (T93)

The `slimference posttool` hook records each `(session_id,
tool_name, command, output)` tuple in `~/.slimference/repetition.db`.
The row also keeps `first_turn_id` and `last_turn_id` for debug/provenance, but
the identity stays session-wide so useful cross-turn repetition detection is not
accidentally reduced to one turn.
On the third (and later) occurrence, the captured output is
replaced with `[tool output identical to msg #N (seen M times)]`
before the archive write. Counters at
`/admin/status.repetition`. The `slimference filter` subprocess
case is intentionally skipped because no `session_id` is available
there (would need extra hook plumbing).

### Configurable system prompt (T86 + T87 + T92)

`[compression] prompt_override_path` points at a file whose contents
replace the compile-time summariser system-prompt header. Optional
`# version: <tag>` line is recorded in
`/admin/status.summarization.active_prompt_version`. The few-shot
example block rotates per request (Go / Python / TypeScript / Rust)
based on the input transcript (T87). Every bullet must end with a
`[msg:N]` lineage marker (T92); compliance rate is exposed at
`/admin/status.summarization.lineage_marker_rate`.

### Robust CoT stripping + deterministic repair (T89 + T90)

`StripCoTTags` removes the canonical 12-family reasoner-tag set
(`<think>`, `<thinking>`, `<reasoning>`, `<reason>`, `<analysis>`,
`<scratchpad>`, `<reflection>`, `<plan>`,
`<chain_of_thought>` / `<chain-of-thought>`, `<inner_thought>`,
`<inner_monologue>`) at fixed-point. When the validator rejects a
summary, deterministic repair (header strip, `*` / `1.` -> `- `
normalisation, preamble trim) runs before paying for a retry call.
Counters at `/admin/status.summarization.cot_tag_counts` and
`.repair_*_total`.

### Codex evidence corpus and regression gate (T75)

`tests/fixtures/codex/` is the checked-in Codex evidence corpus directory:
synthetic request fixtures used by the proxy compression tests, a
`session-smoke.jsonl` log used by the reporting path, and a single
`codex-metadata.json` (schema_version=1) declaring the corpus provenance
(scrubbing method, Codex version, hooks/layers, scenarios) and a
`regression_gate` baseline.

`go run ./scripts/benchmarks codex-smoke-gate <dir>` aggregates the corpus
and asserts the baseline (min request count, min savings ratio, per-layer
min saved tokens, provider/route counts). It is wired as the final step of
`go run ./scripts/ci` so the smoke fixture cannot regress without failing
local CI. The synthetic numbers in this corpus are a regression backstop,
not a Codex savings claim; real claims still need a 10-20 session live
corpus that is intentionally not captured until the operator allows it.

### Anthropic version negotiation (T62)

`[proxy] anthropic_versions = ["2023-06-01", ...]` whitelists trusted
header values. Unknown versions downgrade via
`anthropic_unknown_behavior`:

- `conservative` (default): skip L1 + L2, still use L3 response cache.
- `passthrough`: no compression at all.
- `full`: trust the unknown version (opt-in risk).

Rate-limited warn fires ≤ 1× per minute on unknown versions. Count is
exposed at `/admin/status.anthropic_version.unknown_seen_total`.

---

## 9. Install and integration

`docs/install.md` is the install/uninstall SSOT. The current Phase H path is
Codex-first and scoped:

- hook callouts in `~/.codex/hooks.json`
- per-process Codex CLI traffic through
  `slimference codex run -- <prompt>`; explicit
  `--transport=wss` enables scoped Responses WebSockets through the raw
  local WSS frontdoor before live default promotion
- optional shared Codex CLI/App traffic through
  `slimference codex enable` / `slimference codex disable`, which writes
  only a marker-owned `slimference-codex` provider block in
  `~/.codex/config.toml`; explicit `codex enable --transport=wss` writes
  `supports_websockets=true`

The normal user-facing commands are `slimference install`,
`status --preflight`, `codex run`, `codex enable`, `codex disable`,
`codex status`, `codex certify wss`, `uninstall`, and
`status`. Global transparent lab commands are `cert-trust`,
`root-arm --global-chatgpt-hosts`, transparent `enable`, transparent `disable`, and
`root-disarm`.
Default install is Codex-only. Claude Code remains in tree, but is parked:
`--with-claude` is a compatibility no-op, the app policy forces
`claude_code=false`, `/admin/apps` rejects enabling it, and the SNI router
always passes `api.anthropic.com` through.

`internal/integrate` and `slimference integrate` are legacy/advanced
diagnostics for config-patch flows. `slimference codex run` is the current
scoped one-shot CLI path; `slimference codex enable` is the shared scoped
CLI/App route. The older proxy lifecycle and
transparent-proxied helpers remain advanced diagnostics. No default install,
TUI setup action, or primary certification path should depend on persistent
`OPENAI_API_BASE`, persistent `HTTPS_PROXY`, macOS System Network Proxy
settings, or persistent legacy `openai_base_url`.

The TUI exposes the same scoped lifecycle: Setup shows install state, scoped
Codex route state, daemon controls, and a direct `[r]` toggle for
`slimference codex enable` / `slimference codex disable`. Apps shows
per-app routing policy, with Claude Code parked. Stats/Savings counters come
from `/admin/state`. Global transparent controls remain labelled as lab-only.

### Legacy `integrate install`

`slimference integrate` is legacy/advanced config-patch mode and is not the
Phase H path. While Claude is parked, the command normalizes `all` to Codex,
rejects `--client=claude` without writing files, and never writes
`ANTHROPIC_BASE_URL`.

| Client / Surface | Wire point                                 | File            |
|------------------|--------------------------------------------|-----------------|
| Codex            | `openai_base_url` + `chatgpt_base_url`     | config.toml     |
| Hooks            | Optional Codex lifecycle + tool hooks      | hooks.json etc. |

Every edit uses fenced marker comments:

```
# >>> slimference integration >>>
openai_base_url = "http://127.0.0.1:8990/backend-api/codex"
chatgpt_base_url = "http://127.0.0.1:8990/backend-api/"
# <<< slimference integration <<<
```

First touch of an existing file leaves a timestamped backup
`.slim-backup-<ts>`.

### Optional Codex hook layer

`slimference hook install codex` is separate from transparent proxy setup and
legacy config-patch integration. It writes `~/.codex/hooks.json`, executable
scripts under `~/.slimference/hooks/`, and only the official
`[features] hooks = true` flag in `~/.codex/config.toml`; it does not
write `openai_base_url` or `chatgpt_base_url`.

The installed events are `SessionStart`, `PreToolUse`, `PermissionRequest`,
`PostToolUse`, `UserPromptSubmit`, and `Stop`. `PostToolUse` is Bash-only by
default; write tools (`apply_patch`, `Edit`, `Write`) and MCP calls are not
post-processed because their current ROI is negative without per-tool output
contracts. `SessionStart` records hook state without injecting context unless
`SLIMFERENCE_CODEX_HOOK_MODE=debug` is set, and `PreToolUse` does not
block/retry Bash commands unless `SLIMFERENCE_CODEX_HOOK_MODE=aggressive` is set
deliberately.
`PostToolUse` records turn state and archives raw Bash output. The default
`SLIMFERENCE_CODEX_HOOK_MODE=auto` emits visible `continue:false` replacement
only for Bash outputs with at least 600 original tokens, at least 400 saved
tokens, and at least 45% savings. `compact` / `aggressive` force visible
replacement for any changed output; `silent` keeps archive-only behaviour. It fail-opens on unknown
payload shapes so Codex never sees hook crashes for unsupported tool results:
missing or non-string tool-output fields are skipped and recorded as telemetry.
Tiny outputs below `hooks.codex_posttool_min_tokens` are skipped before the
heavy compaction/archive path. Both the generated shell hook and the Go
entrypoint enforce a fail-open watchdog via
`hooks.codex_posttool_timeout_seconds` / `SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS`;
timeout telemetry is recorded as `timeout_fail_open`.
The primary no-chat-noise saving path is the Codex proxy route, not visible hook
feedback. The hook path also owns the safe T126
cross-tool mini path: raw `git status` path lists are recorded in the
file-backed current turn, and a later same-session/same-turn/same-CWD
`git diff --name-only` with the same exact path fingerprint is replaced by an
explicit marker. Diff hunks, `--name-status`, `git ls-files`, non-git output,
and standalone `slimference filter` runs are not touched. For large
AST-compacted Go file reads, the archive context also prints
`slimference expand-body <archive-id> <symbol>` so an omitted function or method
body can be recovered from the archived original. Unsupported/fail-open fields
such as `PreToolUse.updatedInput` remain disabled until a live Codex build
proves they are honored.

### Parked Claude Code hook code

Claude Code hook code is retained for audit/history, but no public product
entrypoint activates it. `slimference hook install claude`,
`slimference hook remove claude`, `slimference integrate --client=claude`,
`slimference readhook claude`, and top-level `slimference claudeposttool`
are parked/rejected. The TUI row is parked, the apps manager refuses
`claude_code=true`, and `api.anthropic.com` remains outside the hosts patch.
Claude Code optimization is intentionally delegated to RTK for now.

Persistent hook/read-cache/repetition/tool-archive storage now uses the shared
`internal/sessions.SafeSessionID` convention for non-empty session ids and
`internal/sessions.SafeTurnID` for turn metadata. Blank sessions remain
no-op/anonymous depending on the store, and blank turn ids degrade to the prior
session-only behaviour so missing provider metadata never creates unbounded
cross-session state.

### TOML scope safety

The Codex fence is inserted **before the first `[table]` header** in
`config.toml`, not at EOF. TOML scoping rules make any key=value after
a `[header]` belong to that table — an EOF-append would silently nest
our keys inside the last `[projects.*]` section and Codex would never
see them at root. `insertBeforeFirstTable` guarantees top-level scope.

### Duplicate-key safety

TOML forbids a key appearing twice at the same table level. If the user
has a manual `openai_base_url` or `chatgpt_base_url` at unambiguous
top-level scope, `stripConflictingTopLevelKeys` removes it before
writing the fence. Keys nested inside tables are preserved (they are
dead from Codex's POV anyway; not our call to touch).

### Shell-rc flavour detection

`DetectRCFile` picks by `$SHELL` match + existence:

```
$SHELL=/bin/zsh   → ~/.zshrc
$SHELL=/bin/bash  → ~/.bashrc, or .bash_profile if present
$SHELL=.../fish   → ~/.config/fish/config.fish
otherwise         → ~/.zshrc (macOS default)
```

Fish uses `set -gx VAR value`; zsh / bash use `export VAR=value`.

### Verbs

```
slimference                         # TUI
slimference install                 # Codex-only install plan
slimference status --preflight      # scoped Codex readiness checks
slimference codex run -- <prompt>   # one-shot CLI with fail-open direct
slimference codex run --transport=wss -- <prompt>
slimference codex enable            # shared Codex CLI/App route
slimference codex enable --transport=wss
slimference codex disable           # remove shared route
slimference cert-trust              # global lab: open Keychain Access
slimference root-arm --global-chatgpt-hosts
slimference enable                  # global lab: enable SNI-peek daemon mode
slimference disable                 # global lab: disable SNI-peek daemon mode
slimference root-disarm             # global lab: remove hosts + pfctl
slimference status [--json]         # current setup state
slimference uninstall               # reverse install plan
slimference integrate status        # legacy/config-patch detection
```

### Doctor integration

`slimference doctor` appends an "Integration / Fallbacks" block with
the same detection output so a single command covers config, upstream
reachability, CLI drift, and the auto-integration state.

---

## 10. Bypass and Fallback

### Master bypass (T67)

`Proxy.bypassMode` (atomic.Bool) short-circuits every
`isLayerEnabled` and `isProviderEnabled` check. When on, the proxy
still accepts every connection and forwards bytes unchanged — a pure
transparent relay. Useful when a request feels off and you want to
rule Slimference out instantly.

Controls:

- TUI hotkey `B` (flash toast confirms state; header shows `⚠ BYPASS`).
- `slimference bypass on|off|status` CLI (talks to the admin endpoint).
- Admin POST `/_slimference/admin/bypass {"enabled": true}`.

State is read via `AdminStatus.Bypass`.

### Daemon-down safety

`launchd` KeepAlive (T68) reshapes:

```
<key>KeepAlive</key>
<dict>
  <key>SuccessfulExit</key>    <false/>
  <key>Crashed</key>           <true/>
</dict>
<key>ThrottleInterval</key>    <integer>2</integer>
```

- Clean `service stop` → stays stopped.
- Process crash → restart in ≤ 2 s → SDK retry papers over the gap.
- Crash loop → throttled to 2 s minimum to avoid CPU burn.

### Post-install health probe

`slimference service install` polls `/admin/health` for up to 10 s and
reports `ok` or `degraded + troubleshooting hint`. Probe is
injectable in tests (`healthProbeFn`).

### Shutdown-timeout guard (T60)

`Proxy.Shutdown(ctx)` returns `ErrShutdownTimeout` when a worker
ignores context cancellation. A goroutine pprof dump is written to
`~/.slimference/shutdown-hang-<ts>.pprof`. Headless mode maps the
error to exit code 6 (T44). Nil `ctx` is tolerated
(`context.Background` substituted).

### Failure-mode matrix

See `docs/integration.md` for the full table. Summary:

| Scenario                    | Client impact                        | Recovery                          |
|-----------------------------|--------------------------------------|-----------------------------------|
| Daemon crashed              | 1× ECONNREFUSED, SDK retries         | none                              |
| Restart loop                | some reqs fail                       | `integrate remove` + shell reload |
| Binary moved / deleted      | persistent ECONNREFUSED              | manual cleanup from docs          |
| Want compression off        | —                                    | TUI `B` or `bypass on` CLI        |
| Panic button                | —                                    | `integrate emergency-off`         |

---

## 11. Security

### Secrets detector

`internal/security` scans every request body + response body for 12
built-in patterns (AWS access key, GitHub token, Anthropic key, OpenAI
key, JWT, etc.) plus user patterns. Modes:

- `off`: scanning disabled.
- `warn`: matches logged, content unchanged.
- `redact`: matches replaced with `[REDACTED:<pattern>]`.
- `block`: matches cause 400 BadRequest from the proxy.

Config: `[security] mode`, `[security.allowlist] patterns`.

### Per-session suspend (T59)

`Detector.SuspendUntil(t)` + 1 h hard-cap disables scanning for a
bounded window without re-deploying. Surfaced via admin:

```
POST /_slimference/admin/security/suspend
Body: {"suspend_seconds": 600}
→ {"active": true, "until_unix_sec": ..., "mode": "redact"}
```

`GET` reports current state. Negative / zero seconds clear the
suspension (`time.Time{}`). Scanning is fully lazy-expired: the next
call after the deadline sees the cleared state.

### File permissions

- Config file written with `0o644`.
- launchd env file (`.env`) written with `0o600` — contains
  `MINIMAX_API_KEY`.
- SQLite files (`filter.db`) created with standard perms.

### What Slimference does NOT do

- No transparent interception unless the operator installs the local CA and
  arms the macOS System-HTTPS-Proxy.
- No modification of provider certificates. Transparent mode signs local leaf
  certificates from Slimference's local root CA, then validates upstream
  provider certificates on the outbound connection.
- No SOCKS/WebRTC interception; microphone/audio UDP paths are expected to
  bypass transparent mode.
- No traffic inspection for non-allowlisted hosts; those CONNECT requests are
  raw-relayed.

---

## 12. Observability

### Admin API

Base path `/_slimference/admin`:

| Endpoint              | Method   | Purpose                                       |
|-----------------------|----------|-----------------------------------------------|
| `/status`             | GET      | Full state snapshot.                          |
| `/provider`           | POST     | `{"provider": "anthropic", "enabled": false}` |
| `/layer`              | POST     | `{"layer": 2, "enabled": true}`               |
| `/flush`              | POST     | Flush caches.                                 |
| `/bypass`             | GET/POST | Master bypass (T67).                          |
| `/security/suspend`   | GET/POST | Per-session secrets-off (T59).                |
| `/health`             | GET      | Liveness probe (plain 200 OK).                |

### `/admin/status` fields

- `status`, `service`, `version`, `listen_port`.
- `layers`, `providers`, `queue_depth`, `cache_entries`.
- `analytics`: snapshot (tokens, ratios, requests).
- `analytics_queue`: capacity, depth, enqueued_total, dropped_total (T42).
- `recent_requests`: last 20 `RequestMetrics`.
- `layer0`: per-filter runtime observability: attempts, matches, misses,
  panics, bytes in/out, bytes saved, hit rate, and average runtime.
- `layer2`: queue depth, compressing, last run, cache size.
- `read_cache`, `checkpoints`, `tool_archive`: subsystem stats.
- `provider_health`: per-provider health.
- `prompt_cache.breakpoints_injected_total` (T45).
- `pipeline`: array of `PhaseSnapshot {name, count, p50_ms, p95_ms,
  avg_ms, max_ms, sample_size}` for `l1`, `l2`, `l3`, `upstream`,
  `total` (T58).
- `anthropic_version`: whitelist + unknown-behavior + count (T62).
- `bypass`: current master-bypass state (T67).

### Structured logging

`internal/slogutil` ships `JSON` logger with rotation (10 MB × 5).
`SLIMFERENCE_LOG_LEVEL=debug|info|warn|error`. Format-switchable via
`--log-format text|json` in headless mode.

### Decision chain (debug)

`internal/debug/decisions.go` records per-request layer breakdowns
into a ring buffer (default 100) + optional JSONL log. Inspect via:

```
slimference debug last            # newest entry (--json for machine)
slimference debug tail 30         # 30 newest rows
slimference debug summary week    # aggregate SubLayerBreakdown
slimference debug replay file.jsonl
slimference debug paths           # where everything lives
```

### Pipeline histograms (T58)

15 ns / observation on an M1. 200-sample rolling ring per phase;
percentiles on demand.

---

## 13. TUI

`internal/tui` is a BubbleTea UI with a Launch Center as the default view.
The top-level user surface is intentionally small: Launch Codex CLI, Launch
Codex App, Savings, Status, and Manage Slimference. The implementation reuses
the existing Stats, Apps, Debug, and Setup views behind those entries instead
of creating a second TUI.

Launch Codex CLI opens the proven scoped wrapper path with
`transport=auto`. Launch Codex App launches the process-local
`--transport=app-server` Desktop path, whose hidden shim rewrites the
`thread/start` `modelProvider` so the Desktop conversation rides the same
`websocket_phasef` savings route as the CLI (verified 2026-05-22 via the daemon
decisions log; the Desktop app-server holds loopback sockets to `:8990` with no
direct `chatgpt.com` socket). Capability gating from `codex desktop status` still
exists, but note the gate currently reads the sampled WSS delta counters, which
lag and under-report; the reliable green signal is the decisions-log
`route_mode=websocket_phasef`. Historical proxy/CA failures remain diagnostic
proof state. Normal Finder/Spotlight Codex.app launches remain direct.
Manage Slimference owns one product-level install/repair surface for Codex CLI
and Desktop together. Per-app rows are route policy/capability state, not
separate install states. Manage also owns daemon start/stop/restart/repair,
route controls, CA, lab controls, and the guided "Repair Codex CLI WSS savings"
action that calls the same recert core as the CLI/background path. Old macOS
`U`/`UE` or `dyld_start` Slimference processes are shown as reboot-only stale
processes when detected; the current healthy daemon PID remains the actionable
state.

The Launch Center labels the Codex route in hard product terms: `WSS savings
active` means Phase-F mutation is certified for the current tuple; `WSS route
ready` means Desktop reaches the route but that specific proof did not show
mutation; `WSS native bridge` / `WSS bridge/fallback` means Codex stays on the
native WebSocket path while savings repair or fallback is active. The status
surface includes recert attempt id, started/finished/last-success/retry times,
last error, and the bounded recert log path when available.

The main product panel reads the `/admin/state.savings.product` rollup through
the same local or remote adapter path as the rest of the TUI. It shows route
state, billable input saved, output-wire bytes, cache hit/miss totals,
read-delta/repeated-output/chunk hits, and safety or host-budget attention. Raw
parser matrices, policy internals, and mechanism debug counters stay in debug
surfaces; the normal view does not invent a second mixed savings headline. It
renders explicit zero values for output-wire and provider-cache savings instead
of hiding absent counters, so `0` and "not shown" are never confused. The TUI
caches product status in the model and refreshes it on ticks/events instead of
fetching during render; host-budget attention slows the next tick from 500 ms to
2 s. Product-signal selection is handled by the pure `PresentProductStatus`
presenter before Bubble Tea styling, so route/savings/safety projection is
unit-testable without starting the TUI and debug-only WSS internals cannot drift
into the default product panel unnoticed.

### Keybindings

Auto-generated in `docs/tui-keybindings.md` from
`internal/tui/keys.go` (T64). Drift-check test fails if they diverge.

| Category    | Keys        | Action                         |
|-------------|-------------|--------------------------------|
| Navigation  | `←/→/h/l`   | previous / next view           |
| Navigation  | `↑/↓/j/k`   | move up / down                 |
| Navigation  | `enter`     | execute highlighted action     |
| Views       | `s`         | stats view                     |
| Views       | `d`         | debug log view                 |
| Views       | `a`         | apps view; Codex CLI/Desktop toggles; Claude row parked until explicit Claude hosts opt-in |
| Setup       | `r`         | enable/disable scoped Codex CLI/App route |
| Layers      | `1/2/3`     | toggle Layer N                 |
| Actions     | `f`         | flush caches                   |
| Actions     | `b`         | **toggle bypass** (T67)        |
| Actions     | `q`/`ctrl+c`| quit                           |

### Bypass badge

When bypass is on, the header renders `⚠ BYPASS` so it is visible from
every view. A flash toast echoes the new state on toggle.

### Remote mode

`newRemoteProxyAdapter` (in `cmd/slimference/remote_proxy.go`) talks
to a running daemon via the admin API rather than driving a local
`Proxy` instance. Used when you run `slimference` against a daemon
started by `service install`.

The Stats view renders Layer 0 parser telemetry from the same admin status
snapshot: total attempts/matches/misses/panics, runtime hit rate, bytes saved,
and the top parser filters by saved bytes. This is runtime observability; the
persisted billing-style Layer 0 savings view remains `slimference gain
--by-parser`.

---

## 14. Configuration Reference

Order of precedence (highest wins):

1. CLI flag (`--config <path>`) — T46.
2. `SLIMFERENCE_CONFIG` env var.
3. `$XDG_CONFIG_HOME/slimference/config.toml`.
4. `~/.slimference/config.toml` (legacy — supported with deprecation
   warn).
5. Built-in defaults.

`slimference doctor` reports the resolved path + source so operators
can tell at a glance which file was read. `ResolveConfigPath(opts)`
surfaces the same info programmatically via `LoadInfo`.

### Top-level blocks

```toml
[proxy]
listen_address = "127.0.0.1"
listen_port    = 8990
ipv6           = false
anthropic_versions         = ["2023-06-01"]   # T62 whitelist
anthropic_unknown_behavior = "conservative"   # conservative|passthrough|full

[upstream.anthropic]     base_url = "https://api.anthropic.com"
[upstream.openai]        base_url = "https://api.openai.com"
[upstream.codex_chatgpt] base_url = "https://chatgpt.com"           # T66

[compression]
layer1_enabled                       = true
layer2_enabled                       = false
layer3_enabled                       = true
sliding_window                       = 6
min_messages_for_compression         = 5
min_tokens_for_layer2                = 15000              # T54 (was 30000)
layer2_latency_budget_ms             = 0                  # T54 opt-in
layer2_latency_projection_multiplier = 1.2
layer2_latency_ema_alpha             = 0.2
structure_min_tokens                 = 500
dedup_similarity_threshold           = 0.85               # scalar fallback

  [compression.tuning]
  loop_detection    = false                  # T37
  structure_preview = true                   # T76 archive-backed default
  incremental_staircase = [ ... ]            # T27
  dedup_staircase = [                        # T53
    { msg_count_le = 10,       threshold = 0.88 },
    { msg_count_le = 20,       threshold = 0.85 },
    { msg_count_le = 40,       threshold = 0.82 },
    { msg_count_le = 1000000,  threshold = 0.78 },
  ]

    [compression.tuning.tool_compressor]     # T61
    aggressive_after_multiplier = 2
    git_moderate_diff_limit     = 60
    test_max_failure_lines      = 40

  [compression.minimax]
  api_key_env = "MINIMAX_API_KEY"
  base_url    = "https://api.minimax.io/v1"
  model       = "MiniMax-M2.7"
  temperature = 0
  top_p       = 1
  enable_reasoning_split = true

  [compression.summary]
  mode = "balanced"     # strict | balanced | fast (T36)

[cache]
response_cache_max_entries = 100
response_cache_ttl_seconds = 300

[security]
mode = "redact"              # off | warn | redact | block
  [security.allowlist]
  patterns = []

[analytics]
log_dir = "~/.slimference/analytics"

[filter]
passthrough_max_chars = 4000
filter_db             = ""
tee_dir               = ""

[hooks]
slimference_command = "slimference"
exclude_commands    = []
codex_posttool_timeout_seconds = 4
codex_posttool_min_tokens      = 800

[debug]
level           = "info"
format          = "jsonl"
max_entries     = 100
decisions_log   = ""
```

### Environment variable overrides

`config.go::applyEnvOverrides` handles:

```
SLIMFERENCE_LISTEN_ADDRESS, SLIMFERENCE_LISTEN_PORT,
SLIMFERENCE_UPSTREAM_{ANTHROPIC,OPENAI,CODEX_CHATGPT}_BASE_URL,
SLIMFERENCE_COMPRESSION_SLIDING_WINDOW,
SLIMFERENCE_SECRETS_MODE, SLIMFERENCE_LOGGING_LEVEL,
SLIMFERENCE_HOOK_SLIMFERENCE_COMMAND,
SLIMFERENCE_CODEX_POSTTOOL_TIMEOUT_SECONDS,
SLIMFERENCE_CODEX_POSTTOOL_MIN_TOKENS,
SLIMFERENCE_DEBUG_{DECISIONS_LOG,LEVEL,FORMAT,MAX_ENTRIES},
SLIMFERENCE_FILTER_{PASSTHROUGH_MAX_CHARS,DB,TEE_DIR}
```

Plus the runtime toggles: `SLIMFERENCE_HEADLESS=1`,
`SLIMFERENCE_CONFIG=<path>`.

---

## 15. CLI Reference

```
slimference                         Start TUI (requires TTY)
slimference --no-tui                Headless foreground proxy
slimference <subcommand> [flags]
slimference help [subcommand]
```

### Subcommands

| Verb          | Purpose                                                                |
|---------------|------------------------------------------------------------------------|
| `integrate`   | Legacy/advanced config-patch mode; Codex-only while Claude is parked.  |
| `bypass`      | on, off, status — master bypass via admin API.                         |
| `service`     | install, uninstall, start, stop, restart, status, logs; start/restart wait for daemon status (launchd). |
| `daemon`      | Run as long-lived daemon (invoked by launchd; users prefer `--no-tui`).|
| `proxy`       | Transparent CA/daemon/System-HTTPS-Proxy lifecycle plus Codex env helpers. |
| `codex`       | Scoped Codex run, enable/disable/status, WSS certify, Desktop status/launch. |
| `doctor`      | Full diagnostic sweep + integration checks.                            |
| `filter`      | Layer-0 filter wrapper: `slimference filter -- <cmd>`.                 |
| `rewrite`     | Rewrite captured output (used by PreToolUse hook).                     |
| `posttool`    | Codex PostToolUse entry point (stdin JSON).                            |
| `codexhook`   | Codex lifecycle hook entry point for session, permission, prompt, stop. |
| `readhook`    | Codex Read-hook entry point: `slimference readhook codex`.             |
| `expand`      | Retrieve archived tool result by id (T40).                             |
| `expand-body` | Retrieve one Go function/method body from an archived AST read (T125). |
| `checkpoint`  | Smart-compaction checkpoint tools: list, show, restore (T39).          |
| `hook`        | install, remove, verify, status, check-upstream (manual hook mgmt).    |
| `gain`        | Report Layer-0, by-command/by-parser, prompt-cache, output, or proxy-flight telemetry.|
| `stats`       | Analytics snapshots (today/week/month/prompt-cache).                   |
| `savings`     | Unified savings view (L0 + proxy flights + L3) per period; --json / --csv (T80).|
| `compress-preview` | Dry-run the L1 pipeline against a body; --diff / --json (T82).    |
| `watch`       | Live ticker against /admin/status; Ctrl-C to stop (T79).               |
| `filter --stream` | Streaming-aware Layer-0 wrapper for `tail -f` style inputs (T94).  |
| `debug`       | paths, last, summary, tail, replay, flight last/tail/replay/export.    |
| `config`      | init, show.                                                            |
| `test`        | minimax, anthropic, openai, intercept.                                 |
| `completion`  | Emit bash completion.                                                  |
| `trust`       | Trust-model tools (from RTK port).                                     |
| `version`     | Print version.                                                         |

### Flight recorder

`slimference debug flight` reads the same normalized flight records that the
proxy and TUI use. A flight record is generated from each persisted
`RequestSummary` and records route/source, host/path/provider, layer list,
estimated input before/after, provider-reported input/cache/output usage,
output-reduce metadata, `previous_response_id` state, errors, privacy state,
and proxy overhead. `last`, `tail`, and `replay` support `--json`; `export`
writes JSONL by default and CSV with `--csv` or an `.csv` target path.

The recorder is privacy-first: before a request summary is retained or flushed
to `[debug].decisions_log`, bearer auth, API-key/token/password/cookie
assignments, `sk-*` keys, user-home paths, and temp paths are redacted. Raw
request/response bodies are not captured by the flight recorder.

The TUI Debug view renders a `FLIGHT RECORDER` block sourced from the same
records: recent route/source/layers, billable savings estimate, provider cache
tokens, output tokens, bypass count, and slowest request.

### WebSocket inspection

Transparent WebSocket transport is fail-open. `internal/wscompact` preserves
raw frames for unknown, binary, malformed, unsupported-extension, and
schema-drift cases. For negotiated `permessage-deflate` with a supported
profile, `internal/proxy/wsmitm` inflates complete text messages, runs the
Phase F adapter on the JSON envelope, and re-encodes with RSV1 only when a
mutation actually happens. Unmodified compressed messages are forwarded
byte-equal after their plaintext has advanced the destination-side rolling
dictionary, so later context-takeover mutations remain decodable. Reassembled
compressed payloads and inflated plaintext payloads are size-bounded; hitting
either cap fails open to byte-equal forwarding and disables compressed mutation
for that direction without parser degradation. For known
Codex WSS conversation envelopes, `PhaseFDispatcher` attaches a Phase F
adapter:

- client-to-server request payloads run stale-read aging, obsolete-read prune,
  stop-sequence guards, proxy Layer 0 captured-output compaction, and be-terse
  when existing config/cohort gates allow
- server-to-client output item frames teach the adapter session-local tool-call
  metadata, so later client-to-server `function_call_output` frames can compact
  tool output even when Codex splits the request state across WSS messages
- request-body summaries record repeated resolved read/tool keys as a re-read
  canary, so drift analysis can see context-recall pressure without logging raw
  tool output
- server-to-client text deltas run repdet
- terminal response payloads stay byte-equal on WSS to avoid double-counting
  streaming repdet savings or corrupting final code/patch text
- WSS streamcut is intentionally disabled until T236 proves a terminal-safe
  Codex WSS early-cut sequence. HTTP/SSE streamcut is unchanged.

WSS frame-shape inspection is content-free. `wscompact.FrameSummary` records
route, direction, opcode, payload size, JSON top-level shape, top-level field
names, top-level field types, protocol message type, and a stable shape hash
derived from those schema facts. `wscompact.ShapeRegistry` stores counts plus
mutation eligibility and fallback behavior for each observed shape. Arbitrary
JSON and non-Codex routes are inspect-only and do not mark WSS mutation
shape-known for planner gating. Only registered Phase-F-compatible
request/response shapes on the Codex responses route can become
mutation-capable, and even those remain subject to route certification and live
corpus confidence. Unknown or inspect-only shapes stay byte-equal bridge
candidates.

`/_slimference/admin/state` `.wss` reports whether the engine is active,
whether frames are only forwarded byte-equal, whether parser degradation
occurred, how many compressed messages were inspected/mutated/bypassed,
Phase-F request/response event counts, and how many frames were re-encoded
after mutation.

Scoped Codex WSS has an additional pre-`net/http` frontdoor on the same
loopback listener (`:8990`). It intercepts only
`GET /backend-api/codex/responses` WebSocket upgrades whose offered
subprotocol list includes `responses_websockets`. The captured Upgrade
header is forwarded upstream with its original order, casing, subprotocol
text, and unknown headers intact; only Host and absolute request-targets are
normalised for the real upstream. Everything else is replayed into the normal
HTTP server unchanged. This restores the old transparent dispatcher's
raw-header property without reintroducing global `chatgpt.com` routing.

Codex Desktop app-server shim launch reuses the same loopback listener without
arming global transparent mode. Codex.app honors `CODEX_CLI_PATH` when it
starts its Rust `codex app-server`; the launcher points that variable at the
Slimference binary only for the spawned Codex.app process. The hidden
`slimference app-server` shim validates its scoped env, removes its own shim
variables, and runs the real Codex binary as `codex app-server` with a
process-local provider block
(`model_providers.slimference-codex.base_url=http://127.0.0.1:8990/backend-api/codex`,
`requires_openai_auth=true`, `supports_websockets=true`, `wire_api=responses`).

Implemented in `cmd/slimference/codex_desktop_app_server_shim.go`. The shim is a
thin stdin JSON-RPC mediator (not a bare exec): it spawns the real Codex
app-server, passes stdout/stderr straight through (no added latency on streaming
responses), and inspects only the client->server stdin stream, which Codex
Desktop frames as newline-delimited JSON. The single rewrite is on `thread/start`
requests: Codex Desktop sends `modelProvider: null`, which resolves to the
account default provider (`openai` -> chatgpt.com direct) and overrides the
config default; the shim rewrites a default (null/absent) `modelProvider` to
`slimference-codex`. It fails open on any ambiguity (non-JSON, no `params`,
explicit non-null provider, or a realtime/voice thread via
`config["features.realtime_conversation"]`), returning the original bytes
byte-identical so the stream is never corrupted and voice is never touched.

Discovery and proof (2026-05-22): a loopback tee proxy captured the real frames,
and the daemon decisions log (`SLIMFERENCE_DEBUG_DECISIONS_LOG`) recorded both the
CLI and the Desktop app-server (driven with the full Electron feature-flag
`config`) as `route_mode=websocket_phasef` on `/backend-api/codex/responses`. The
Desktop and CLI WSS frames are byte-identical `permessage-deflate`. So the Desktop
conversation rides the same Phase-F savings route as the certified CLI; token
savings materialise on real (compressible) turns. Earlier "zero-byte /
`byte_bridge_only`" readings were sampled-counter artifacts plus trivial test
prompts with nothing to mutate (the same caveat as the CLI smoke). Normal Desktop
remains direct and no-drawback; Browser ChatGPT, ChatGPT.app, computer-use, voice,
and Claude Code are untouched.

Desktop savings proof (2026-05-29): after Codex CLI drifted to 0.135.0, the
official scoped recert path restored `auto=wss_phasef`. A real Codex.app
app-server-shim proof then launched PID 77770 through `slimference codex desktop
prove --manual`; the user prompted three separate `cat` reads of a 76540-byte
target and Codex returned `DESKTOP_T247_0135_DONE`. After quitting Codex.app to
flush the WSS session, `slimference codex desktop prove --finish --json` returned
`desktop_app_server_phasef_proven`, `desktop_savings=true`,
`frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
`phasef_bridged=4`, `compressed_messages_inspected=294`, and zero
parse/degrade/compression errors. This is the current Desktop savings gate:
route-ready still means launch-eligible only; `desktop_app_server_phasef_proven`
is the measured Desktop savings proof.

The older `--transport=proxy --with-ca-env` branch remains an advanced
diagnostic path for future Codex builds, but it is not the preferred Desktop
product route. Its 2026-05-22 proof reached CONNECT and removed Chromium's
direct-socket bypass, yet still produced zero application bytes because the
Desktop client did not accept the local CA/root-store path.
`slimference codex desktop status` reports daemon reachability, WSS counters,
whether a live Desktop app-server proof has been observed, and any legacy
proxy failure state without letting historical daemon-wide counters become a
Desktop savings claim.

WSS auto-promotion is local-proof gated. `slimference codex certify wss`
reads `/admin/state`, refuses to write when WSS parse failures, degraded
sessions, compression errors, byte-bridge-only state, or missing mutation are
present, and writes `~/.slimference/codex-wss-cert.json` only with
`frames_reencoded>0`, `compressed_messages_mutated>0`, and daemon reachability.
`--transport=auto` consumes that proof through `internal/codexroute` using the
explicit ladder `wss_phasef -> wss_bridge -> http -> direct`. Version drift now
sets `needs_recert=true` and starts the shared recert path after daemon health is
green; if a clean byte-equal WSS bridge proof exists, the active user session
stays on WSS bridge while repair runs instead of jumping directly to HTTP.

`slimference codex recertify wss` is the shared repair core for CLI, background
auto-recert, and TUI Manage. It creates a temporary repo, runs real Codex CLI
turns through scoped WSS, evaluates only the `/_slimference/admin/state`
`.wss` delta window,
and writes either the Phase-F cert or the lower-risk
`~/.slimference/codex-wss-bridge.json` proof. It persists bounded repair state in
`~/.slimference/codex-wss-recert.json` and a 2 MiB rotating log at
`~/.slimference/logs/codex-wss-recert.log`. Bridge mode bypasses Phase-F
handlers and streamcut; it is compatibility mode, not a savings claim.
`/admin/state`, `slimference codex status`, and the TUI expose the latest recert
status, attempt id, start/finish/last-success/retry timestamps, last error, and
log path. Background auto-recert is still guarded by lock/backoff and never
keeps mutating after a version drift without fresh proof.

### Compression planner

`internal/planner` is the deterministic safety governor for cross-layer
coordination. It turns request facts (provider/model/route, input/output token
size, content classes, live-corpus confidence, manual disables, recent-edit
state, provider cache support, L2 policy, output-reduce/tool-prune cooldown,
and WebSocket shape confidence) into per-layer decisions for L0, L1, L2, L3,
Layer 4 output/tool controls, and WebSocket transport. The package is pure: same facts produce
the same `CompressionPlan`, every decision carries action, reason, expected
saving, risk, and confidence, and operator-disabled layers stay disabled.
The proxy derives recent-edit state from the current request plus file-backed
hook turn state, so read-only follow-up requests can still preserve recently
edited files. Live-corpus confidence defaults to `unknown`, can be asserted via
`[compression.tuning] planner_live_corpus_confidence`, or derived from
`planner_live_corpus_metadata_path` metadata. WebSocket shape confidence is fed
by the inspect-only `wscompact.ShapeRegistry`; it records observed JSON frame
shapes without changing bytes, and it exposes mutation confidence only for
registered Phase-F-compatible shapes rather than arbitrary JSON envelopes.
Layer 4 cooldown is sourced from the T141 output-reduce tracker and the T151
tool-prune session bucket; the planner marks it as a `cheap_only`
`quality_cooldown_soften_layer4` decision because the runtime softens Layer 4
rather than blindly continuing aggressive behavior. Output-reduce task-shape
selection now bypasses unproven detail-sensitive shapes instead of merely
capping them to `standard`: code edits, new-file generation, debugging, reviews,
tool-result reasoning, command-output relay, final summaries, read-only
analysis, deep explanations, and planning. Those shapes need complete evidence
or exact workflow content more than maximal terse output. The planner mirrors
the runtime output-reduce guard for its own summaries: exact replies,
command-output relay, repair follow-ups, unproven detail shapes, and low-ROI
direct-answer tasks bypass Layer 4 in the plan. Tool-schema
pruning runs only after strict schema extraction: if any `tools[]` entry cannot
be named for the provider shape, the request keeps the full schema instead of
partially pruning a mixed/unknown tool surface.

Product status separates provider-cache savings from local input/output-wire
savings. `/admin/state.savings.product` carries provider-cache read/create tokens
from analytics, billable Layer-0 input savings, output-wire savings, cache hit
counts, analytics proof-loss counters, and safety/host-budget state as distinct
fields so the TUI does not present a mixed headline number. Proof-critical
analytics loss is surfaced as product safety pressure rather than hidden as a
debug-only queue statistic. Host-budget demotion is wired into the Codex Layer-0
policy: if the latest product host-budget snapshot is exceeded, WSS/HTTP Codex
tool-output reducers full-pass until the process is back inside budget.
The reducer hot path reads this as an atomic state bit, avoiding fresh RSS/state
directory scans per frame. Oversized state trees that cannot be fully measured
within the bounded scan limit are treated as budget pressure. Phase-F request
metadata for session id, previous-response id, and model is derived from the raw
request map already parsed for message extraction, so normal WSS request handling
does not repeatedly unmarshal the same frame just to fill planner and recovery
fields.

The proxy hot path attaches this plan to `debug.RequestSummary` and normalized
`flight` records for upstream, local cache, transparent CONNECT, and direct
WebSocket routes. Planner summaries include content-free `content_classes`
labels such as `websocket`, `tool_output`, `source_file`, `json`, and
`repeated_tool_output`. Codex WSS Phase-F records both the upgrade-level route
record and per client request-body planner summaries, so decisions logs can show
real message counts, token deltas, previous-response state, output-reduce
reason, and proof-gated L2/L3 candidates without logging frame payloads. For
HTTP compression requests, the same request-local plan now also controls the
first behavior gates: L0 proxy compaction skips planner
`bypass`, L1 skips planner `bypass` and uses cheap-only mode for planner
`cheap_only`, L1/L2 coordination keys off the planner's L2 `run` decision, and
L2 cache apply/background enqueue skip hard L2 bypasses (operator-disabled,
external policy disabled, recent-edit window). Classical Layer 2 summaries can
only reach planner `run` when Layer 2 is enabled and the explicit legacy
`allow_model_facing_replacement` gate is set; otherwise long-context Layer 2
stays a context-ledger shadow candidate instead of replacing conversation
truth. Soft below-ROI L2 bypasses still fall through to Layer2's session cache
and candidate scoring so the planner does not suppress already-proven cache
wins. Layer-local fallbacks remain active; the planner is an early governor,
not the only safety mechanism.

`slimference plan inspect` dry-runs the same planner without sending upstream
traffic. It accepts provider/model/route/token/cache/WebSocket facts, can
estimate input tokens from a request file or stdin, and prints either a compact
human table or JSON. This is the fixture-facing entry point for comparing
planned versus actual outcomes before any planner decision becomes behavior
controlling.

`scripts/benchmarks benchmark-corpus` replays recorded `plan` objects from
request summaries and compares them with observed layer execution. The report
counts requests with plans, decisions, expected planner savings, expected-active
versus observed-active actions, missed active actions, bypass/tunnel actions
that still saw activity, and safety-blocked requests. Category metadata can set
planner thresholds so future default-on changes have measurable evidence.
It also emits an observed layer-combination matrix keyed by stable labels
(`L0`, `L1`, `L2`, `L3`, `L4`, `WS`, or `none`) with request count, saved
tokens, output tokens, and errors. This is factual corpus accounting, not a
simulated alternate-run replay. Category metadata can additionally declare
`scenario_validators` (`tool_heavy`, `cache_reuse`, `output_reduce`,
`output_reduce_ab`, `planner_alignment`, `websocket`, `low_error`,
`host_budget_ok`, `layer_combo_diversity`, `l2_summary`, `ocrl_full_history`)
so a category fails unless the intended optimization behavior is actually
present in the captured request summaries; unknown validator names fail closed.

`scripts/benchmarks benchmark-corpus --promotion-check` is the stricter
release/default-on gate. It ignores synthetic categories and fails closed unless
the corpus contains at least five `codex_cli` sessions, five `codex_desktop`
sessions, and real `live_operator` coverage for `repeat_read`, `ranged_read`,
`search_loop`, `git_status`, `test_failure`, `apply_patch_edit_read`,
`large_tool_output`, and `long_workday`. Every real category must also declare
`client_family`, `workload_class`, explicit zero error budget, explicit
re-read-canary budget, explicit latency budget, and a positive savings floor.
This keeps unit tests and synthetic fixtures useful while preventing a default
promotion from vague or one-sided evidence.
`wss-proof-export-corpus` appends deduplicated content-free proof rows to
existing categories and recalculates category gates from the combined records,
so new weaker rows cannot replace stronger existing proof.

`go run ./scripts/verify -mode release-proof-plan` prints the deterministic
operator ceremony for a release/default-on decision. The runbook starts from a
clean CI and synthetic-corpus baseline, opens a `workday-savings` window, lists
the scoped CLI and Desktop product launch paths, expands every required
live-corpus workload for both `codex_cli` and `codex_desktop`, then finishes
with `wss-proof-matrix --require-live-token-delta` and
`benchmark-corpus --promotion-check`. The strict matrix mode requires real
admin-state `live_delta` rows; replay bytes remain visible but cannot stand in
for product token savings. The command is content-free and plan-only: it does
not start capture, read payloads, or create fixtures. This keeps proof
collection manual, reviewable, and reproducible.
Unattended CLI capture collection uses
`go run ./scripts/utils codex-capture-run`, which owns the daemon foreground
process, sets `SLIMFERENCE_WSS_AB_CAPTURE`, waits for `/health`, runs scoped
Codex, records before/after admin-state deltas, replays with fail-on-lost
semantics, and appends an optional `wss-proof-matrix` row. The matrix row stores
live `billable_input_tokens_saved`, provider-cache read/create token deltas, and
safety counters (`parse_failures`, `degraded_sessions`, `compression_errors`,
`analytics_proof_events_dropped`) next to replay bytes. Release proof treats live
billable input-token savings and provider-cache read tokens as separate product
signals; replay bytes are retained only as a model-facing regression/safety
proxy. Any proof-critical analytics loss fails the release proof gate; low-value
analytics drops remain visible but are not product-proof blockers. The runner supports
`--codex-timeout` for bounded proof runs, `--exit-marker` /
`--exit-marker-count` for unattended shutdown, and `--quiet-codex-output` for
machine-readable runs without Codex TUI noise. Passing
`--resource-profile-proof <bundle-dir>` turns the same managed run into the
automated CLI host-resource proof: the runner defaults `--capture` to
`<bundle-dir>/frames.jsonl` and `--matrix-row` to
`<bundle-dir>/matrix.jsonl`, writes aggregate admin snapshots before/after,
`ps` snapshots before/after, a macOS `sample` file, and
`workday-finish.json`, then appends the content-free matrix row. This is the
preferred release-proof path for CLI workloads because it avoids detached
background daemons that do not inherit the capture environment reliably.
Expected-reducer validation is evidence-first: when a focused run misses an
expected reducer, the tool appends the matrix row and then exits non-zero. This
keeps negative live evidence such as missing hits or host-budget attention
auditable while still preventing a failed focused proof from passing.
Interactive Desktop proofs use `go run ./scripts/utils wss-proof-live-row`
after the operator-driven Codex.app prompts finish. The tool reads the current
content-free admin state/status snapshots, enforces the requested reducer
signals such as `tool_prune`, `tool_prune_tokens_saved`, or `host_budget_ok`,
and appends a matrix row without reading raw WSS frame payloads. This closes
Desktop cases where `codex-capture-run` cannot own the app process but the proof
still needs reducer-specific live counters before export into
`tests/fixtures/live_corpus`.
Focused `wss-proof-matrix` runs with `--required-workload` evaluate only rows
matching the requested workload classes. When `--expected-reducer` is also
passed, those command-line reducer expectations are authoritative for the
focused proof, so older exploratory rows in the same matrix cannot pollute a
single-mechanism closeout. Unfocused release-proof mode still validates every
row exactly as recorded.
`go run ./scripts/verify -mode host-resource-plan -client codex_cli|codex_desktop`
prints the T272 resource/profile ceremony for the only remaining host-budget
proof class. For CLI, the plan now prints the single automated
`codex-capture-run --resource-profile-proof` command. For Desktop, where the
app prompts remain operator-driven, the plan still prints the manual
admin-state snapshots before and after the workload via `aggregate-savings
--json`, `ps` RSS/CPU rows, `workday-savings finish --json`, a macOS `sample`
CPU profile for the Slimference process, and a WSS matrix row requiring
`host_budget_ok` plus a positive live economic token signal. Local
billable-input deletion, provider-cache read tokens, and output-side evidence
stay separate in the proof row. It deliberately does not enable a pprof HTTP
listener or open a new runtime surface; profiling stays operator-triggered and
file-based.
`go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures
<clean-release-matrix.jsonl> --json` writes the explicit release-claim matrix.
It reads proof rows only, never raw WSS frames, and skips historical diagnostic
rows, host-budget issue rows, expected-zero rows with local savings, safety
issue rows, and rows without an economic signal. It may normalize stale
expected-reducer labels only when the same row has current live reducer
evidence, so a release report cannot pass by aggregate count alone.
`go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl>
--resource-profile-proof <codex-cli-resource-proof-bundle-dir>
--resource-profile-proof <codex-desktop-resource-proof-bundle-dir>` produces the final content-free release proof
summary. It reads proof-matrix rows only and never raw WSS frames. The report
keeps local billable-input token deletion, request-side bytes, output-wire
bytes, provider-cache read/create tokens, tool-prune schema tokens,
output-reduce input overhead, output-reduce observed provider-output tokens,
output-reduce net-observed diagnostics, host-budget rows, and safety rows as
separate fields. Output-reduce net-observed is deliberately not a
counterfactual savings percentage: a focused output-reduce proof must show
guarded injection, observed output-token accounting, bounded input overhead,
host-budget OK, and zero safety errors, while a concrete output-token savings
claim still requires a matching no-directive A/B baseline. It fails closed
without validated CLI and Desktop resource/profile proof bundles
containing `admin-before.json`, `admin-after.json`, `ps-before.txt`,
`ps-after.txt`, `workday-finish.json`, `slimference.sample.txt`, and
`matrix.jsonl`; the JSON files must prove host-budget OK and zero WSS
parse/degrade/compression deltas, and each local `matrix.jsonl` must contain a
positive `host_resource_long_workday` row with `host_budget_ok` for the matching
client. Rows with their own expected reducers must satisfy those reducer
expectations inside the bundle; a positive provider-cache or local-savings
delta cannot mask a missed mechanism-specific proof.
This keeps a green host-budget snapshot from being mistaken for final resource
certification.
`go run ./scripts/utils wss-output-reduce-ab-report <matrix.jsonl>
--min-net-tokens=1 --json` is the content-free output-reduce counterfactual
gate. It pairs matrix rows by `ab_pair_id` and `ab_variant` (`baseline` or
`directive`), requires the same client and workload class, requires provider
output-token observations in both rows, requires guarded output-reduce injection
only in the directive row, subtracts directive input overhead, and fails on
safety errors, output-reduce downgrades, host-budget violations, non-positive
output-token reduction, net tokens below the configured floor, or an injected
directive row that has no positive `output_reduce_input_overhead_tokens`.
`codex-capture-run` and `wss-proof-live-row` can stamp those A/B fields into
matrix rows; the report still reads only content-free proof counters, never raw
prompts, model text, or tool output.
The first focused CLI direct-answer/status A/B passed on 2026-06-05 after
fixing proof accounting to record general provider output tokens from WSS usage
frames and model-facing directive overhead instead of JSON re-marshal byte
churn: baseline `987` provider output tokens, directive `768`, directive input
overhead `23`, output saved `219`, net saved `196`, `22.19%` output-token
reduction, `lost=0`, host budget `ok`, and zero WSS safety errors. That is a
real positive pair for the tested workload, not a universal output-reduce
percentage. The content-free pair is committed as
`tests/fixtures/live_corpus/cli_output_reduce_ab_direct_answer/output_reduce_ab_report.json`;
`benchmark-corpus --maxx-check` now requires an `output_reduce_ab` workload with
positive net A/B tokens, so a plain output-reduce injection row can no longer
satisfy the max-out gate by itself. Broader CLI/Desktop task-shape pairs are
still required before promoting a cross-workload output-reduce savings claim.
An autonomous CLI explanation-shape A/B did not generalize the win: after
compacting the standard preservation directive from `111` to `46` overhead
tokens, the pair still failed net-positive (`baseline=222`, `directive=248`,
`net=-72`). Explanation/deep-analysis and other detail-sensitive shapes now
bypass output-reduce injection by default until future paired A/B evidence proves
positive net savings without repair/re-ask signals; the current positive product
evidence is the direct-answer/status shape.
Historical host-budget attention rows or superseded expected-zero anomalies stay
visible in the report and now fail the release gate by row id. Current readiness
must be proven from a clean matrix or a focused release bundle instead of
letting stale diagnostic rows hide behind aggregate counts.
The WSS dispatcher includes active in-flight Phase-F sessions in `/admin/state`
snapshots, so capture deltas can see `frames_reencoded`,
`compressed_messages_mutated`, and `phasef_mutations` before the WebSocket
session fully tears down. Completed sessions are then folded into the same
monotonic dispatcher counters without double-counting.

Layer-0 mechanism cost is exposed as debug/audit telemetry, not product UI
noise. `/admin/state.savings.proxy_layer0_latency` reports rolling p50/p95/max
and average duration by route for `total`, `read_delta`, `structured_filter`,
`repeated_output`, and `chunk_dedup`. This lets release proof compare savings
against local host cost without logging payloads or charging the hot path with a
new per-frame disk probe. The hot path also uses the total Layer-0 duration as a
bounded runtime safety signal: repeated frames over the 25 ms budget demote
managed Codex reducers to full-pass until fast frames recover the gate.
Exact o200k token counts are reserved for real before/after savings guards and
positive savings claims. WSS no-op planner rows and output-reduce size gating
use cheap byte/4 estimates because those paths do not claim local billable
savings. Output-reduce proof gates use exact provider-reported output-token
usage when available and separately count injected instruction overhead. This
avoids loading the heavy BPE encoder for no-mutation frames while keeping every
reported token saving exact.

### Global flags

```
--no-tui / --headless   Run proxy foreground, no BubbleTea.
--port <n>              Override listen port.
--no-layer1/2/3         Disable Layer N.
--sliding-window <n>    Override L1 sliding window.
--log-level <lvl>       debug | info | warn | error.
--config <path>         Override config file path (T46).
-h / --help             Show help.
-V / --version          Print version.
```

### Exit codes (headless)

| Code | Meaning                                |
|------|----------------------------------------|
| 0    | Clean shutdown.                        |
| 1    | Boot or config error.                  |
| 2    | Bad flags / non-TTY without `--no-tui`.|
| 6    | Shutdown timeout (T60).                |

---

## 16. Installation

### From source (macOS M-series, recommended)

```bash
go run ./scripts/build --install
slimference doctor
slimference install
slimference status
```

`--install` writes the new binary to a same-directory temporary file and then
atomically renames it over `~/.local/bin/slimference`, so new processes never
observe a partially copied executable.

### From a release archive

```bash
curl -fsSL <url>/slimference_<version>_darwin_arm64.tar.gz | tar -xz -C /tmp
install -Dm755 /tmp/slimference_<version>_darwin_arm64/slimference \
    "$HOME/.local/bin/slimference"
```

### Linux systemd (community-supported path)

```bash
./scripts/service/linux/install.sh
journalctl --user -u slimference -f
```

See `docs/deploy/linux-systemd.md` for the full walk-through.

### Docker (reference only)

`scripts/service/docker/Dockerfile` ships a multi-stage distroless
image. Build:

```bash
docker build -f scripts/service/docker/Dockerfile \
    --build-arg VERSION=2.3.0 \
    --build-arg COMMIT=$(git rev-parse --short HEAD) \
    -t slimference:2.3.0 .
```

---

## 17. Build and Release

Primary target is **macOS on Apple M-series (darwin/arm64)**. Cross-
build support for the other three combinations stays in the release
script but is opt-in.

### Default build (primary target only)

```bash
go run ./scripts/release --version v2.3.0
```

Produces:

```
dist/slimference_2.3.0_darwin_arm64/slimference
dist/slimference_2.3.0_darwin_arm64.tar.gz
dist/SHA256SUMS
```

### All targets

```bash
go run ./scripts/release --version v2.3.0 --targets=all
```

Adds `darwin_amd64`, `linux_arm64`, `linux_amd64`.

### Hand-picked subset

```bash
go run ./scripts/release --version v2.3.0 \
    --targets=darwin/arm64,linux/amd64
```

### `ldflags` injection

Both `main.version` (for backward compat) and
`github.com/slimference/slimference/internal/buildinfo.Version` (the
canonical source read by `--version` and `doctor`) are set. Without
the buildinfo injection, `--version` would print the compile-time
default from `version.go` and ignore the tag.

### Reproducibility

- `-trimpath` strips absolute paths.
- `-s -w` strips debug sections.
- `go run ./scripts/build` is the canonical local build helper and
  always emits the single Slimference binary with those flags. It does
  not split the product into multiple runtime binaries.
- `go run ./scripts/build --install` installs through temp-file plus atomic
  rename instead of truncating the existing executable in place.
- `CGO_ENABLED=0` — Slimference has no C dependencies (SQLite via
  `modernc.org/sqlite` is pure Go).

### Release checklist

Full process in `docs/release-process.md`.

---

## 18. Testing Strategy

- **Unit tests**: `*_test.go` alongside every file. Target: high,
  behavior-significant coverage of production code (internal/ + cmd/).
- **Integration**: `tests/integration/` with `//go:build integration`
  tag; covers the full pipeline against a stub upstream.
- **TypeScript supplemental**: `tests/ts/` with `bun:test` for schema
  + CLI contract checks.
- **Race detector**: `go test -race ./...` green; required gate.
- **Coverage gate**: `scripts/coverage` fails CI below the threshold and runs
  package plus intra-package coverage serially to keep proxy shutdown/resource
  tests deterministic.
- **Benchmark harness**: `scripts/benchmarks` runs the canonical
  micro-benchmarks under `go test -bench`.

### Coverage headline

Current formal release gate: `go run ./scripts/ci` runs
`go vet ./...` plus `go run ./scripts/coverage -min=95.0`. `go vet ./...`
is the default static-analysis gate for all module packages, including
`scripts/` and `docs/`, because Slimference's repo tooling carries release
proof logic. No optional `staticcheck` or `golangci-lint` dependency is
required for normal CI. The coverage step is an aggregate gate for the
configured Go coverage profile. Individual package lines can report less than
the aggregate threshold while the total gate remains green; do not describe
that as a release failure unless `scripts/ci` itself exits non-zero. The hard
rule is behavior-significant testing: new or changed complex logic, product
paths, safety branches, routing/fallback decisions, and regression risks need
real failable tests. The intent is to keep meaningful product/safety paths
covered without spending engineering time on artificial coverage-chasing tests,
unreachable OS-dependent cleanup branches, or always-green assertions.

### Benchmarks

`internal/compression/bench_test.go`:
- `BenchmarkCompress_{small,medium,large,code}`: full L1 pipeline.
- `BenchmarkStripANSI`, `BenchmarkStripComments`,
  `BenchmarkExtractStructure`: per-sub-layer hot paths.

`internal/filter/bench_test.go`: filter hot paths.

`internal/proxy/layer0_bench_test.go`: Codex/WSS Layer-0 hot paths for
large git status compaction and repeated read-delta. T272 profiling found
repeated exact o200k tokenization as the dominant pre-cache cost for 64 KB
repeat-read frames; the bounded token-count cache keeps that path in the
sub-millisecond to low-millisecond range on Apple M1 benchmark runs. The
2026-06-04 full benchmark surface measured WSS repeated git-status at about
788 us/op and WSS repeated read 64 KB at about 814 us/op after the latest
safety hardening.

`internal/readcache/bench_test.go`: full-file and ranged read repeat-cache
hot paths, including archive-backed unchanged decisions. The same 2026-06-04
run measured full-repeat 64 KB at about 446 us/op and ranged-repeat 16 KB at
about 167 us/op.

`internal/chunkdedup/bench_test.go`: FastCDC chunking and partial-overlap
chunk-reference encoding. The same 2026-06-04 run measured FastCDC chunking
256 KB at about 211 us/op and partial-overlap 64 KB store/encode at about
2.03 ms/op.

`internal/contentarchive/bench_test.go`: archive write and archive expansion
for 64 KB-class payloads. Archive reads stay cheap for recovery, while 64 KB
archive writes measured about 29.4 ms/op on the full 3 s run; archive writes
therefore remain a bounded recovery path, not a blind default mutation tax.

`internal/planner/bench_test.go`: runtime planner decision overhead for a
large Codex WSS tool-output shape. The 2026-06-04 run measured the Codex WSS
large-tool decision at about 267 ns/op.

Race coverage is part of the host-resource closeout evidence. The focused
savings/safety race pass covers `internal/contextledger`, `filter`,
`readcache`, `chunkdedup`, `toolprune`, `outputreduce`, `proxy/wsmitm`,
`quality`, and `hostmetrics`, and the full repository gate
`go test -race ./...` is green after the Codex recert helper-process timeout was widened for
race instrumentation. Full live CLI/Desktop resource/profile bundles are now
the release proof: the final CLI and Desktop bundles pass `release-proof-report`
with host-budget OK, zero WSS safety deltas, positive economic token evidence,
and the required local matrix rows.

`internal/analytics/phase_hist_test.go::BenchmarkPhaseHistogram_Record`:
phase recorder overhead (~15 ns/op on M1).

---

## 19. Package Map

```
cmd/slimference/              Entry point + every CLI subcommand.
  main.go                     Flag dispatch, subcommand router.
  codex_cmd.go                Codex route CLI core: run, enable, disable, status, certify.
  codex_desktop_proof_cmd.go  Codex Desktop app-server proof/status flow.
  tui_proxy_adapter.go        In-process proxy adapter for the Bubble Tea TUI.
  help.go + help_test.go      --help content; golden-file drift check (T64).
  headless.go                 --no-tui runner with signal traps (T44).
  integrate_cmd.go            integrate install|remove|status|emergency-off (T65).
  bypass_cmd.go               bypass on|off|status via admin API (T67).
  remote_proxy.go             TUI adapter talking to a remote daemon.

internal/proxy/               HTTP server + request pipeline.
  proxy.go                    New(), ServeHTTP(), toggles, admin router.
  handler.go                  Hot path, upstream relay, overflow recovery, compression orchestration.
  handler_workers.go          Compression worker, analytics worker, health JSON, periodic flush.
  handler_shutdown.go         Graceful shutdown, drain timeout, shutdown pprof dump.
  handler_accessors.go        TUI/admin accessors, cache flush, tool-prune helper extraction.
  provider.go                 detectProviderWithUA + request/response reconstruction.
  admin.go                    AdminStatus snapshot + /admin/* handlers.
  version_negotiation.go      Anthropic version whitelist (T62).

internal/compression/         Layer 1 sub-layers + Layer 1 pipeline.
  layer1.go                   Compress() orchestrator, dedup staircase (T53).
  prompt_cache.go             Breakpoint injection (T45).
  tool_compressor.go          Tuning knobs + filter set (T61).
  preview.go                  Structure-aware preview (T38).
  loop_detect.go              Loop-nudge Jaccard detector (T37).

internal/summarization/       Layer 2 OpenAI-compatible summarizer client.
  layer2.go                   Summarisation + cache + staircase (T27, T36).
  latency_estimator.go        EMA + ShouldRunLayer2 decision (T54).

internal/caching/             Layer 3 response cache + file watcher.

internal/analytics/           Rolling snapshots + phase histograms (T58).

internal/integrate/           Auto-integration (T65).
  integrate.go                Marker-fence block primitives.
  shellrc.go                  rc-file detection + write.
  codex_toml.go               config.toml fence writer with scope safety.
  detect.go                   Per-client + daemon detectors.
  install.go                  Install / Remove / DiffPreview.

internal/daemon/              launchd plumbing (macOS).
  daemon.go                   InstallLaunchd + plist + FormatStatus (T68).

internal/hooks/               Claude + Codex hook installers.
internal/filter/              Layer-0 pipeline + 24 filters + SQLite.
internal/security/            Secrets detector + per-session suspend (T59).
internal/tui/                 BubbleTea UI + keybinding registry (T64).
internal/readcache/           Read-hook delta cache (T37).
internal/toolarchive/         Large tool-result archive + expand (T40).
internal/checkpoints/         Smart-compaction checkpoints (T39).
internal/config/              Load / ResolveConfigPath / LoadOptions (T46).
internal/tokens/              tiktoken wrapper + per-provider calibration.
internal/debug/               Decision-chain recorder + replay.
internal/sessions/            Session logs, response-state, and file-backed T138 hook turn-state.
internal/resilience/          Retry + backoff + rate limiter.
internal/slogutil/            Rotating JSON log handler.
internal/buildinfo/           Build-time Version + Commit (ldflags-set).
internal/types/               Shared types (Provider, Message, ContentBlock).
internal/util/                Generic helpers.

scripts/release/              Cross-build + tar + SHA256 (T47).
scripts/service/linux/        systemd unit + install.sh (T48).
scripts/service/docker/       Distroless Dockerfile.
scripts/benchmarks/           Benchmark runner.
scripts/coverage/             Coverage gate.
scripts/utils/                Offline session/decision/filter/proof reports and
                              local generated-artifact hygiene guard.

docs/
  documentation.md            This file.
  integration.md              Operator guide for integrate + bypass.
  layer0-exit-codes.md        Layer-0 exit-code matrix (T63).
  deploy/linux-systemd.md     Linux install walk-through (T48).
  release-process.md          Release cut process (T47).
  todo.md, todo/              Task tracker.
```

For the detailed dependency graph see `docs/map.md`.
