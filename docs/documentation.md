# Slimference - Technical Documentation

Version: 2.3.0
Last updated: 2026-05-13

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
6. [Layer 2 - OpenAI-Compatible Summarisation](#6-layer-2-openai-compatible-summarisation)
7. [Layer 3 - Response Cache](#7-layer-3-response-cache)
8. [Provider Support](#8-provider-support)
9. [Auto-Integration (Claude Code + Codex)](#9-auto-integration-claude-code--codex)
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

Slimference is a Go reverse proxy plus a set of CLI shims that reduce token
usage when working with Claude Code and OpenAI Codex. It sits on
`127.0.0.1:8990`, receives HTTP requests from both clients, applies four
layers of compression to the conversation history, forwards the result to
the real upstream (`api.anthropic.com`, `chatgpt.com`), and streams the
response back unchanged.

The proxy is **transparent**: request shape, headers, streaming semantics,
and response bytes are preserved end-to-end. Only the conversation body
bytes shrink.

### Why it works

| Problem                                          | Slimference answer                          |
|--------------------------------------------------|---------------------------------------------|
| Large tool outputs repeated across turns         | Exact + MinHash dedup (Layer 1)             |
| Long sessions exceed the context window          | Adaptive summarisation via MiniMax (L2)     |
| Identical requests re-cost tokens                | Response cache + prompt-cache breakpoints   |
| Verbose shell / git / test output                | 24 built-in filters + TOML DSL (Layer 0)    |
| Compression costs latency on small requests      | Thresholds + latency-budget guard (T54)     |

### Client support

- **Claude Code**: via `ANTHROPIC_BASE_URL=http://127.0.0.1:8990` (supported
  since Claude Code 1.0). Full proxy access to request + response bodies.
- **Codex**: default path is transparent mode (`slimference proxy
  install|enable`): local CA + daemon + macOS HTTPS proxy, with no Codex
  config mutation. Legacy/config-patch mode remains available via
  `slimference integrate install --client codex`, which writes
  `openai_base_url` + `chatgpt_base_url` in `~/.codex/config.toml`.
- **Generic OpenAI API**: `api.openai.com/v1/chat/completions` routed by
  path detection when no Codex UA is present.

### Design invariants

- **Explicit transparent MITM only when armed**: legacy/config-patch mode
  terminates plain HTTP on loopback. Transparent mode is opt-in via
  `slimference proxy install|enable`; when armed it uses the local trusted CA
  to terminate allowlisted LLM HTTPS hosts and re-dials upstream with normal
  certificate validation.
- **Passthrough on failure**: if any layer errors, the original body is
  forwarded. See section 10.
- **Bypass switch**: a single atomic flag collapses every provider + layer
  toggle to off, making the proxy a pure relay.
- **`encoding/json` only**: no third-party JSON library.
- **Hot path budget ≤ 5 ms**: all Layer 1 sub-layers benchmarked.

---

## 2. Architecture

```
┌─────────────┐        ┌─────────────────────────────────────┐
│ Claude Code │──env──▶│                                     │
└─────────────┘        │     slimference (127.0.0.1:8990)    │
┌─────────────┐        │                                     │
│    Codex    │──cfg──▶│  ┌──────── request pipeline ────┐   │
└─────────────┘        │  │ detect → L1 → L2 → L3 cache  │   │──HTTPS──▶ api.anthropic.com
                       │  └──────────────────────────────┘   │
                       │                                     │──HTTPS──▶ chatgpt.com
                       │  ┌──────── response pipeline ───┐   │
                       │  │ stream relay + cache update  │   │──HTTPS──▶ api.openai.com
                       │  └──────────────────────────────┘   │
                       │                                     │
                       │  admin, TUI, analytics, debug       │
                       └─────────────────────────────────────┘
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
11. **Layer 2** — MiniMax summarisation when `len(tokens) >=
    min_tokens_for_layer2` AND latency budget permits (T54).
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

Layer 0 runs **outside** the HTTP proxy, invoked by Claude Code's and
Codex's hook mechanisms. `~/.codex/hooks.json` and Claude's settings
file point at `~/.slimference/hooks/*.sh`, which invoke
`slimference filter`, `slimference rewrite`, and `slimference posttool`.

### Pipeline

`internal/filter/pipeline.go::RunPipeline` runs:

1. Exec the tool command; capture stdout + stderr + exit code.
2. ANSI strip on stdout.
3. 24 built-in filters tried in priority order: git-status, git-diff,
   git-log, git-show, build-output, test-output, dotnet, ruby, search,
   ls, tree, strip-comments-file-read, lint, format, psql,
   package-manager, container, gh list, glab list, log dedup, aws
   json, python traceback, terraform plan, json minify.
4. Fallback: `FirstMatchingTOMLRule` applies user-defined 8-stage
   rules from `~/.slimference/filters.toml`.
5. Truncate with a short `[truncated …]` hint to
   `passthrough_max_chars` (default 4000; spec+.md §4.6).
6. Emit to stdout + write the raw bytes to the tee dir for recovery.
7. Record the run in `filter.db` (SQLite).

### Exit-code contract

See `docs/layer0-exit-codes.md` for the complete matrix. The invariant:
Slimference **never** swallows a non-zero child exit. Filter failures
are degradation signals, not status translations.

### Token-saving reporting

`slimference gain [today|week|month|all]` aggregates rows from
`filter.db` into a summary with savings percentages. `--by-command`
breaks it down per argv[0]; `--by-parser` groups persisted Layer-0
savings by parser/tool family; `--cache` reports persisted provider
prompt-cache read/create counters; `--output` reports persisted T130
output-reduce telemetry without inventing a savings baseline, including T141
profile tier and task-shape buckets. `--csv` / `--json` for machine
consumption.

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
| 4 | Exact dedup + MinHash/LSH          | `dedup.go` + `dedup_minhash.go`|
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

### Reversible path dictionary (T143a)

`semantic_dictionary.go` aliases repeated absolute local paths inside one
tool-result block only when the embedded legend plus aliases are strictly
shorter. It preserves reversibility by prepending a small dictionary such as
`[P1]=/Users/.../file.go`, then replacing repeated body occurrences with
`[P1]`. It is deliberately narrow: known local filesystem roots only,
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
Negative-saving bypass still applies before any compacted block is used.

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

## 6. Layer 2 - OpenAI-Compatible Summarisation

`internal/summarization/layer2.go` calls the configured
OpenAI-compatible `/v1/chat/completions` endpoint to summarise old tool
outputs and conversation tails. The default endpoint is MiniMax M2.7,
but `[compression.minimax]` is now only a historical section name:
`base_url`, `model`, and `api_key_env` can point at another compatible
provider without code changes.

### Decision rule (T54)

```
if tokens < min_tokens_for_layer2:       skip "below_threshold"
if budget_ms > 0 and projected > budget: skip "latency_budget"
else:                                    run
```

Where projected latency = EMA(observed MiniMax latencies) ×
`layer2_latency_projection_multiplier`. The EMA seeds with 400 ms so
the guard is conservative on a cold start.

Default `min_tokens_for_layer2 = 15000` (was 30 k pre-T54). The
latency-budget guard is opt-in; `layer2_latency_budget_ms = 0`
disables it.

T129 default state: fresh configs enable Layer 2. Existing configs with
`layer2_enabled = false` stay disabled. The first interactive startup
with Layer 2 enabled records an explicit acknowledgement under
`~/.slimference/policy/layer2-default-on-ack.json`; non-interactive
startup warns without blocking. `slimference layer2 acknowledge` records
the marker manually, and `slimference layer2 status` prints the ack
state.

Provider/runtime knobs:

- `SLIMFERENCE_MINIMAX_BASE_URL`, `SLIMFERENCE_MINIMAX_MODEL`, and
  `SLIMFERENCE_MINIMAX_API_KEY_ENV` override the summariser endpoint,
  model, and secret env var for fast provider swaps.
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
the *original* request body + pertinent headers. Hits skip Layer 1 and
Layer 2 entirely and serve the cached upstream response.

### Invalidation

`internal/caching/file_watcher.go` watches every file referenced by
recently-cached tool calls (via fsnotify). A write invalidates every
cache entry whose key was computed from a body mentioning that path.

### Prompt-cache breakpoints (T45)

`internal/compression/prompt_cache.go::OptimizeCacheBreakpoints` places
up to 4 `cache_control: {type: "ephemeral"}` markers on the messages of
the stable prefix. T45 spreads them EVENLY at ~25/50/75/100 percent
depths of the eligible prefix rather than clustering at the tail. That
creates overlapping cache layers: a small late edit still hits the
earlier layers, and a large prefix change only invalidates the layers
it spans.

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
- Responses-style `input` arrays map `message`, `function_call`, and
  `function_call_output` items into `types.Message`.
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
OpenAI API traffic. When enabled and the estimated input size crosses
`min_tokens`, Slimference may add a privacy-safe hashed `prompt_cache_key`
and a model-gated `prompt_cache_retention` value. Existing caller-owned
fields are preserved, generated keys never contain raw prompt text or full
local paths, and a per-key rate cap disables the hint before it can create
high-cardinality cache churn. If OpenAI rejects the fields with a relevant
4xx response, the proxy retries once without those hints. CodexChatGPT
backend routes do not receive these fields until T140 captures live request
acceptance.

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
`/admin/status.coordinator.skipped_total`.

### L1 message-level fan-out (T104)

`[compression.tuning] coordinator_parallel` runs `compressMessage`
concurrently per message in the compressible prefix, bounded by
`runtime.GOMAXPROCS(0)`. The `archiveOriginal` recorder is mutex-
protected and the `coordinator_skipped` counter is atomic so the
hot path stays race-clean. Default off until benchmarks show
real-body wins. Note: shipped at message granularity, not the
spec's stage-partitioned sub-layer concurrency; reopens as T104b
if message-level granularity turns out to be the wrong knob.

### Mid-exchange summary (T99)

`[compression.tuning] mid_exchange_enabled` activates an
in-progress summary block when the current exchange exceeds
`mid_exchange_threshold_tokens` (default 10000). Detection looks
for completed tool-use cycles (`assistant[tool_use]` ->
`user[tool_result]` -> `assistant`) inside the live exchange and
collapses the range to `[in-progress summary, anchor=msg #N]`.
Today the summary content is a deterministic placeholder; a live
MiniMax-driven content path is tracked as T99b. Default off.

### Layer 4 tool-definition pruning (T103)

`[compression.tuning] tool_prune_enabled` activates the per-session
tool-usage tracker + body-rewrite pass. Tool definitions idle
beyond the threshold are removed from `tools[]` for Anthropic
(`tools[].name`) and OpenAI / CodexChatGPT (`tools[].function.name`
or top-level `tools[].name`). Telemetry at
`/admin/status.tool_prune.{sessions,pruned_total,reattach_total,
tokens_saved_sum}`. Default off. Forward-path-only in this
release: if the model invokes a pruned tool, the upstream sees the
reduced `tools[]`. Reattach (T103b) is tracked separately.

### Posttool cross-session repetition marker (T93)

The `slimference posttool` hook records each `(session_id,
tool_name, command, output)` tuple in `~/.slimference/repetition.db`.
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

## 9. Auto-Integration (Claude Code + Codex)

`internal/integrate` and `slimference integrate` (T65) implement the explicit
legacy/config-patch path. The preferred Codex CLI/App path is transparent mode:
`slimference proxy install` installs the local CA + daemon support and
`slimference proxy enable` arms macOS HTTPS proxy routing without touching
`~/.codex`.

The TUI exposes the same transparent lifecycle. The Setup view starts with
**Install transparent proxy (CA + daemon)** and **Arm system HTTPS proxy**;
Codex/Claude hooks are legacy fallback steps after that. The Dashboard can arm
or disarm transparent mode directly. Setup shortcuts are `[a]` arm/disarm,
`[u]` uninstall transparent, `[p]` daemon start/stop, `[o]` restart, `[e]`
enable autostart, and `[w]` disable autostart. The status shown there is a
cached snapshot of CA, keychain trust, launchd, system proxy, daemon
reachability, and networksetup health.

For CLI-only split testing, `slimference proxy env codex --proxied` prints a
non-mutating command that points only that Codex CLI process at
`127.0.0.1:8990`; the Codex App remains direct as long as macOS
System-HTTPS-Proxy is off. The helper launches Codex with a per-process custom
provider named `slimference-codex`, sets its base URL to
`http://127.0.0.1:8990/backend-api/codex`, marks it
`requires_openai_auth=true`, and sets `supports_websockets=false`. That avoids
the current Codex CLI WebSocket retry/fallback delay and sends HTTP Responses
traffic directly through Slimference's zstd-aware compression pipeline. Default
direct mode still tunnels Codex's WebSocket transport byte-for-byte.

### What `integrate install` does

| Client / Surface | Wire point                                 | File            |
|------------------|--------------------------------------------|-----------------|
| Claude Code      | `export ANTHROPIC_BASE_URL=...`            | shell rc        |
| Codex            | `openai_base_url` + `chatgpt_base_url`     | config.toml     |
| Hooks            | Optional Codex lifecycle + tool hooks      | hooks.json etc. |

Every edit uses fenced marker comments:

```
# >>> slimference integration >>>
export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
# <<< slimference integration <<<
```

First touch of an existing file leaves a timestamped backup
`.slim-backup-<ts>`.

### Optional Codex hook layer

`slimference hook install codex` is separate from transparent proxy setup and
legacy config-patch integration. It writes `~/.codex/hooks.json`, executable
scripts under `~/.slimference/hooks/`, and only the official
`[features] codex_hooks = true` flag in `~/.codex/config.toml`; it does not
write `openai_base_url` or `chatgpt_base_url`.

The installed events are `SessionStart`, `PreToolUse`, `PermissionRequest`,
`PostToolUse`, `UserPromptSubmit`, and `Stop`. `PostToolUse` is the primary
token-saving hook path: raw output is archived and compact feedback is emitted
with Codex's documented replacement shape. Unsupported/fail-open fields such
as `PreToolUse.updatedInput` remain disabled until a live Codex build proves
they are honored.

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
slimference proxy install                     # default transparent Codex path
slimference proxy enable                      # arm system HTTPS proxy
slimference proxy env codex --direct          # print Codex CLI direct env command
slimference proxy env codex --proxied         # print CLI-only proxy env command
slimference proxy env codex --transparent-proxied # print CLI CONNECT/MITM env command
slimference                                    # TUI control plane for install/arm/disarm
slimference integrate status                  # detect legacy/config-patch state
slimference integrate install                 # legacy wire-up
slimference integrate install --dry-run       # show diff, no writes
slimference integrate install --client=codex  # one client only
slimference integrate remove                  # clean teardown
slimference integrate emergency-off           # remove + stop daemon + uninstall launchd
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

`internal/tui` is a BubbleTea UI with three primary views: Dashboard
(default), Stats, Debug. Hooks + service lifecycle are managed from
within.

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
| Providers   | `c`         | toggle Claude Code             |
| Providers   | `x`         | toggle Codex                   |
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
layer2_enabled                       = true
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
| `integrate`   | status, install, remove, emergency-off; wire Claude + Codex + hooks.   |
| `bypass`      | on, off, status — master bypass via admin API.                         |
| `service`     | install, uninstall, start, stop, restart, status, logs (launchd).      |
| `daemon`      | Run as long-lived daemon (invoked by launchd; users prefer `--no-tui`).|
| `proxy`       | Transparent CA/daemon/System-HTTPS-Proxy lifecycle plus Codex env helpers. |
| `doctor`      | Full diagnostic sweep + integration checks.                            |
| `filter`      | Layer-0 filter wrapper: `slimference filter -- <cmd>`.                 |
| `rewrite`     | Rewrite captured output (used by PreToolUse hook).                     |
| `posttool`    | Codex PostToolUse entry point (stdin JSON).                            |
| `codexhook`   | Codex lifecycle hook entry point for session, permission, prompt, stop. |
| `readhook`    | Claude Read-hook entry point.                                          |
| `expand`      | Retrieve archived tool result by id (T40).                             |
| `checkpoint`  | Smart-compaction checkpoint tools: list, show, restore (T39).          |
| `hook`        | install, remove, verify, status, check-upstream (manual hook mgmt).    |
| `gain`        | Report Layer-0, by-command/by-parser, prompt-cache, or output telemetry.|
| `stats`       | Analytics snapshots (today/week/month/prompt-cache).                   |
| `savings`     | Unified savings view (L0 + L1/2 + L3) per period; --json / --csv (T80).|
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

Transparent WebSocket transport remains byte-for-byte by default. The
`internal/wscompact` package adds an inspect-only frame reader that can be
attached to the tunnel without mutation. It preserves raw bytes, decodes
frame metadata, reassembles fragmented text messages for shape inspection,
records opcode/direction/payload length/JSON top-level keys/message type, and
marks RSV/compressed-extension frames as inspect-only blockers. This is the
T142 foundation for future WebSocket message-boundary compression; mutation
is still blocked on live Codex frame-shape evidence.

### Compression planner

`internal/planner` is the deterministic safety governor for cross-layer
coordination. It turns request facts (provider/model/route, input/output token
size, content classes, live-corpus confidence, manual disables, recent-edit
state, provider cache support, L2 policy, output-reduce cooldown, and
WebSocket shape confidence) into per-layer decisions for L0, L1, L2, L3,
output-reduce, and WebSocket transport. The package is pure: same facts produce
the same `CompressionPlan`, every decision carries action, reason, expected
saving, risk, and confidence, and operator-disabled layers stay disabled.
Output-reduce cooldown is sourced from the T141 auto-tune tracker before
profile selection; the planner marks it as a `cheap_only`
`quality_cooldown_soften_profile` decision because the runtime softens the
profile rather than fully disabling output reduction.

The proxy hot path now attaches this plan as dry-run advice to
`debug.RequestSummary` and normalized `flight` records for upstream, local
cache, transparent CONNECT, and direct WebSocket routes. This does not yet
override layer execution; it gives `debug flight`, the TUI, and corpus replay a
single explanation surface for "why this request was compressed or skipped".
Actual layer behavior remains guarded by the existing layer-local fallbacks
until T146 planned-vs-actual evidence is available.

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
simulated alternate-run replay.

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
go build -o $HOME/.local/bin/slimference ./cmd/slimference
slimference doctor
slimference proxy install
slimference proxy enable
```

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
- `CGO_ENABLED=0` — Slimference has no C dependencies (SQLite via
  `modernc.org/sqlite` is pure Go).

### Release checklist

Full process in `docs/release-process.md`.

---

## 18. Testing Strategy

- **Unit tests**: `*_test.go` alongside every file. Target: 100% of
  production code (internal/ + cmd/).
- **Integration**: `tests/integration/` with `//go:build integration`
  tag; covers the full pipeline against a stub upstream.
- **TypeScript supplemental**: `tests/ts/` with `bun:test` for schema
  + CLI contract checks.
- **Race detector**: `go test -race ./...` green; required gate.
- **Coverage gate**: `scripts/coverage` fails CI below the threshold.
- **Benchmark harness**: `scripts/benchmarks` runs the canonical
  micro-benchmarks under `go test -bench`.

### Coverage headline

2.3 total: **96.5%** across all packages. Production code in
`internal/*` and `cmd/*` is effectively at 100%. The 3.5 pp gap lives
in `scripts/{release,ci,coverage,benchmarks,utils}` tooling packages
whose `main()` shells out to external binaries and is exercised
manually or via the operator-driven release pipeline.

### Benchmarks

`internal/compression/bench_test.go`:
- `BenchmarkCompress_{small,medium,large,code}`: full L1 pipeline.
- `BenchmarkStripANSI`, `BenchmarkStripComments`,
  `BenchmarkExtractStructure`: per-sub-layer hot paths.

`internal/filter/bench_test.go`: filter hot paths.

`internal/analytics/phase_hist_test.go::BenchmarkPhaseHistogram_Record`:
phase recorder overhead (~15 ns/op on M1).

---

## 19. Package Map

```
cmd/slimference/              Entry point + every CLI subcommand.
  main.go                     Flag dispatch, subcommand router.
  help.go + help_test.go      --help content; golden-file drift check (T64).
  headless.go                 --no-tui runner with signal traps (T44).
  integrate_cmd.go            integrate install|remove|status|emergency-off (T65).
  bypass_cmd.go               bypass on|off|status via admin API (T67).
  remote_proxy.go             TUI adapter talking to a remote daemon.

internal/proxy/               HTTP server + request pipeline.
  proxy.go                    New(), ServeHTTP(), toggles, admin router.
  handler.go                  Hot path + overflow recovery + shutdown (T60).
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
internal/sessions/            Session logs, response-state, and T138 turn-state owner.
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
scripts/utils/                Offline session/decision/filter reports.

docs/
  documentation.md            This file.
  integration.md              Operator guide for integrate + bypass.
  layer0-exit-codes.md        Layer-0 exit-code matrix (T63).
  deploy/linux-systemd.md     Linux install walk-through (T48).
  release-process.md          Release cut process (T47).
  todo.md, todo/              Task tracker.
```

For the detailed dependency graph see `docs/map.md`.
