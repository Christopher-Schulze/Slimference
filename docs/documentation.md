# Slimference - Technical Documentation

Version: 2.0.2
Last updated: 2026-04-17

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Layer 0: Pre-Entry Filtering (CLI)](#3-layer-0-pre-entry-filtering-cli)
4. [Layer 1: Deterministic Compression](#4-layer-1-deterministic-compression)
5. [Layer 2: MiniMax Summarization](#5-layer-2-minimax-summarization)
6. [Layer 3: Response Caching](#6-layer-3-response-caching)
7. [Layer 4: Concurrency Pipeline](#7-layer-4-concurrency-pipeline)
8. [Provider Support](#8-provider-support)
9. [Security](#9-security)
10. [Debug & Observability](#10-debug--observability)
11. [TUI Dashboard](#11-tui-dashboard)
12. [Configuration](#12-configuration)
13. [CLI Commands](#13-cli-commands)
14. [Installation and Setup](#14-installation-and-setup)
15. [Testing Strategy](#15-testing-strategy)
16. [Package Structure](#16-package-structure)
17. [Synergy Optimizations & Cascade Effects](#17-synergy-optimizations--cascade-effects)
18. [Audit Baseline and Remediation Tracking](#18-audit-baseline-and-remediation-tracking)

---

## 1. Overview

Slimference is a transparent HTTP reverse proxy written in Go that intercepts
outgoing requests from LLM CLI tools (Claude Code, OpenAI Codex), applies
multi-layer token compression to the conversation history, and forwards optimized
requests to the real upstream API. Responses stream back to the CLI unmodified.
The supported agent surface in this release is Claude Code and Codex.

### Problem

LLM APIs are stateless. Every request includes the full conversation history.
Token accumulation is O(N^2): the cost of message N is proportional to N times
the average message size. A 30-message coding session may send 10x more tokens
than actually needed for context.

### What Slimference does

- Intercepts POST /v1/messages (Anthropic) and /v1/chat/completions (OpenAI)
- Applies four compression layers to old message history
- Forwards compressed request to real API; streams response back unchanged
- Tracks token savings, latency improvement, and cost metrics in a TUI dashboard

### Savings numbers

| Session length | Input token savings | Effective usage multiplier |
|----------------|---------------------|---------------------------|
| Short (5-10 messages) | 30-50% | ~1.5x |
| Medium (15-25 messages) | 55-65% | ~2-2.5x |
| Long (30+ messages) | 65-67% | ~3x |

Typical concrete impact: ~70-90 messages before hitting Claude Code rate limits,
vs ~30 messages without the proxy. TTFT at message 30 drops from ~4s to ~1.5s.

### Design principles

- Zero-downside guarantee: compression is skipped if quality would degrade.
  Uncompressed passthrough is always the fallback.
- Additive-only transformation: the proxy only removes or replaces tokens.
  It never adds content the user or model did not produce.
- Transparency: every compression decision is logged. The TUI debug view shows
  exactly what was changed and why.
- Graceful degradation: if MiniMax is unavailable, new Layer 2 jobs are skipped.
  If regex extraction fails on a file, that file passes through uncompressed.
  Every layer is independently optional and hot-togglable.
- Complete provider invisibility: the proxy is architecturally undetectable by
  upstream providers. Headers, auth tokens, and response formats are passed
  through verbatim.

---

## 2. Architecture

Slimference operates at two distinct points in the token lifecycle:

**Layer 0 - Pre-Entry (CLI):** Shell commands executed by LLM agents are intercepted
*before* output enters the conversation. `slimference filter` runs the subprocess,
filters stdout through 24+ specialized built-in filters, and returns compact output
to the agent. Savings are permanent - the conversation never sees the raw output.

**Layers 1-3 - Post-Entry (HTTP Proxy):** Conversation history already in the
context is compressed in-flight on every API request. Layer 1 is synchronous and
deterministic (<1ms). Layer 2 uses MiniMax M2.7 for semantic summarization (async,
pre-computed). Layer 3 caches responses.

```
LLM Agent (Claude Code / Codex)
        |
        | invokes tool (e.g. "Bash git status")
        v
+---------------------+    LAYER 0 PATH (pre-entry)
|  slimference filter  |  <- hook intercepts shell commands
|  (Layer 0 CLI)      |  <- runs subprocess + 24 built-in filters
+---------------------+  <- compact output returned to agent
        |
        | agent adds compact tool_result to conversation
        v
+---------------------+    LAYERS 1-3 PATH (post-entry, each API request)
|    Slimference Proxy |  localhost:8990
|  proxy.ServeHTTP    |
+---------------------+
        |
        +--- Layer 1: Deterministic Compression  (<1ms, synchronous)
        |     ANSI strip -> JSON compact/comment strip -> dedup (SHA256+MinHash)
        |     -> structure extraction -> delta encoding -> tool classifier
        |     -> tool compressor -> success short-circuit -> image replacement
        |     -> repeated collapse -> graph pruning -> pre-filter tag
        |
        +--- Layer 2: check MiniMax summary cache
        |     hit: use pre-compressed summary for old messages
        |     miss: skip, enqueue async job for next request
        |
        +--- Layer 3: response cache + Anthropic prompt cache breakpoints
        |
        +--- reconstruct request (Anthropic or OpenAI wire format)
        |
        v
Upstream API (Anthropic / OpenAI)
        |
        | SSE stream (unmodified, byte-for-byte relay)
        v
LLM Agent (response displayed)
        |
        +--- post-response: cache, update analytics, enqueue L2 async job
```

### Request flow (step by step)

1. CLI sends HTTP POST to localhost:8990
2. `proxy.ServeHTTP` reads body (up to 32 MB), detects provider from URL path
3. For non-compressible paths (not /v1/messages or /v1/chat/completions): passthrough
4. If provider is toggled off via TUI: passthrough without compression
5. `handleCompressibleRequest` runs the full pipeline
6. Layer 1 runs synchronously in <1ms
7. Layer 2 cache checked; pre-compressed summary used if available
8. Layer 3 response cache checked
9. Request reconstructed (Anthropic or OpenAI wire format) and forwarded
10. SSE stream relayed byte-for-byte to the CLI
11. After stream complete: cache response, update analytics, enqueue Layer 2 job

### Goroutine model

| Goroutine | Owner | Purpose |
|-----------|-------|---------|
| TUI event loop | BubbleTea | Renders dashboard, handles keyboard input |
| Proxy server | net/http | Accepts connections; spawns one goroutine per request |
| compressionWorker | proxy.Proxy | Reads CompressJob from channel (cap 4), runs Layer 2 |
| analyticsWorker | proxy.Proxy | Reads AnalyticsEvent (cap 256), updates in-memory counters |
| cacheJanitor | proxy.Proxy | Periodic TTL-based eviction from response cache |
| analyticsPeriodicFlush | proxy.Proxy | Writes JSONL analytics to disk every 30 min |

All goroutines respect context cancellation. `Proxy.Shutdown` sends a shutdown
signal and waits for all goroutines to exit with a sync.WaitGroup.

### Key data flow invariants

- The original request body is stashed in request context before compression.
  On a 413 (context overflow) response from upstream, the original body is
  replayed with aggressive compression (retry-on-overflow).
- Provider toggle state and layer enable/disable state are stored in
  `[2]atomic.Bool` and `[3]atomic.Bool` respectively. No mutex is needed
  in the hot path; the TUI writes atomically via `SetProviderEnabled`/`SetLayerEnabled`.
- Analytics events flow only one way: hot path -> analyticsQueue channel ->
  analyticsWorker -> Analytics struct. The hot path never takes the analytics mutex.

---

## 3. Layer 0: Pre-Entry Filtering (CLI)

Layer 0 intercepts shell commands *before* their output enters the conversation.
It runs as a subprocess interceptor via shell hooks. Because it filters at entry
time, savings are permanent and accumulate across the entire session.

### How it works

1. Shell hook intercepts agent tool invocation (e.g. `Bash git status`)
2. Hook calls `slimference filter -- git status`
3. slimference spawns the subprocess, captures stdout/stderr
4. ANSI escape codes stripped from stdout
5. Built-in filter dispatched (priority: built-in > TOML > passthrough truncation)
6. Filtered output returned to agent with original exit code
7. Run recorded to SQLite (`filter_runs` table) for analytics

### Filter dispatch priority

Built-in filters run first. If no built-in matches, TOML rules from
`.slimference/filters.toml` (project) and `~/.slimference/filters.toml` (user)
are checked in order. Finally, passthrough truncation limits output to
`passthrough_max_chars` (default 2000; 0 = unlimited).

### Built-in filters (24 modules)

| ID | Command family | What it does |
|----|---------------|-------------|
| F01 | `git status` | Porcelain -> staged/worktree/untracked counts |
| F02 | `git log` | Empty -> `[git log] empty`; compact commit list |
| F03 | `git diff` | Empty -> `[git diff] empty`; stats+hunk compression |
| F04 | `git show` | Empty -> `[git show] empty`; stat summary |
| F05 | `git push/pull/fetch/merge/rebase` | up-to-date detection -> one-liner |
| F06 | `cat/head/tail <file>` | Comment stripping for known language extensions |
| F07 | Build tools (go build, cargo, tsc, etc.) | Error extraction; empty -> ok |
| F08 | Test runners (go test, cargo test, pytest, jest, etc.) | Failure-focus; empty -> ok |
| F09 | Linters (golangci-lint, eslint, ruff, clippy, etc.) | Violation grouping |
| F10 | Search (rg, grep, fd, git grep) | No-match -> one-liner; result limiting |
| F11 | `ls`, `tree` | Empty -> one-liner; summary for large listings |
| F12 | Package managers (npm, cargo, pip, etc.) | Progress strip; empty -> ok |
| F13 | Docker/K8s (docker ps, kubectl get, helm) | Compact table output |
| F14 | JSON stdout | `json.Compact` (only if shorter) |
| F15 | `docker logs`, `kubectl logs` | Consecutive duplicate line collapse |
| F16 | AWS CLI JSON | Strip `ResponseMetadata`/`SdkHttpMetadata` |
| F17 | ANSI/progress | Strip via `compression.StripANSICodes` |
| F18 | `gh`, `glab` | Empty list -> one-liner |
| F19 | `psql` | Empty -> `[psql] ok` |
| F20 | `dotnet build/test/publish` | Error extraction; empty -> ok |
| F21 | Ruby (`rake`, `rspec`, `rubocop`) | Failure-focus; empty -> ok |
| F22 | `go test -json` | JSON event parsing, failure-only output |
| F23 | `mypy`, `pyright`, `basedpyright` | Empty -> ok; error grouping |
| F24 | Formatters (prettier, gofmt, rustfmt, etc.) | Empty -> ok; file list |

### Hook system

`slimference hook install <agent>` currently supports `claude` and `codex`.

- Claude Code installs `~/.claude/hooks/slimference-rewrite.sh` and merges a
  `PreToolUse` command hook into `~/.claude/settings.json` without replacing
  unrelated user hooks.
- Codex installs `~/.slimference/hooks/codex-pre-tool.sh` and
  `~/.slimference/hooks/codex-post-tool.sh`, merges `PreToolUse` and
  `PostToolUse` entries into `~/.codex/hooks.json`, patches
  `~/.codex/config.toml`, and keeps a legacy AGENTS.md helper block for older
  setups. Removal is conservative: Slimference-managed config lines are removed
  without stripping unrelated user `codex_hooks` or other `[features]` entries.

### TOML Filter DSL

Custom filters can be defined in `.slimference/filters.toml` (project-level) or
`~/.slimference/filters.toml` (user-level). The DSL supports 8 transform stages:
`strip_ansi`, `replace` (regex->string), `match_output` + `unless`, `strip_lines_matching`,
`keep_lines_matching`, `truncate_lines_at`, `head_lines`/`tail_lines`, `max_lines`, `on_empty`.

### Tee recovery system

When a subprocess exits non-zero, the raw unfiltered output is saved to
`~/.slimference/tee/` and a recovery hint is added to the filtered output.
This prevents data loss when a command fails mid-execution.

### SQLite tracking

Every filter run is recorded to `~/.slimference/filter.db` (`filter_runs` table):
`command`, `project_path`, `input_tokens`, `output_tokens`, `savings_pct`, `created_at`.
Query with `slimference gain today|week|month|all` or `slimference debug tail`.

---

## 4. Layer 1: Deterministic Compression

Layer 1 is pure Go, zero external dependencies, zero latency added, zero risk.
It runs synchronously on every request. Total execution time target: <1ms.
All transformations are deterministic and reversible in principle.

Only messages OUTSIDE the sliding window (default: last 5 exchanges) are
eligible for compression. The current message, system prompt, and recent
messages are always passed through unmodified.

### Sub-layer pipeline (execution order)

Layer 1 runs 14 sub-layers in order per message, then two cross-message passes:

| # | Sub-layer | File | What it does |
|---|-----------|------|-------------|
| L1.7 | ANSI strip | `ansi_strip.go` | Remove escape codes and progress bars |
| L1.1 | JSON compact | `json_compact.go` | `json.Compact` on valid JSON tool results |
| L1.2 | Comment strip | `comment_strip.go` | Language-aware comment/whitespace removal (10 langs) |
| L1.3 | Dedup | `dedup.go`, `dedup_minhash.go` | SHA256 exact + MinHash near-duplicate (k=128, Jaccard 0.85) |
| L1.4 | Structure extract | `structure.go` | Regex-based function/type signature extraction |
| L1.5 | Delta encoding | `delta.go` | Unified diff for repeated file reads |
| L1.8 | Tool classifier | `tool_classifier.go` | Classify tool_result by type (git/test/build/lint/etc.) |
| L1.9 | Tool compressor | `tool_compressor.go` | RTK-style per-type filtering (stats, failure focus, limits) |
| L1.10 | Success short-circuit | `success_shortcircuit.go` | "0 errors/all pass" -> one-liner |
| L1.11 | Image replacement | `image_replace.go` | base64 image data -> text descriptor |
| L1.6 | Prompt cache | `prompt_cache.go` | Anthropic cache_control breakpoint injection |
| L1.12 | Repeated collapse | `repeated_collapse.go` | Identical tool calls -> reference (cross-message) |
| L1.13 | Graph pruning | `graph_pruning.go` | Prune stale read ops when later edit+read exists |
| L1.14 | Pre-filter tag | `prefilter_tag.go` | Skip redundant ops on Layer 0 compact output |

L1.1/L1.2 are mutually exclusive (JSON takes priority over comment strip).
L1.12 and L1.13 run after the per-message loop over all messages.
L1.14 fires early and skips L1.1, L1.2, and L1.4 for Layer 0 compact markers.

### 4.1 JSON Minification (L1.1)

Removes whitespace from JSON tool results using `encoding/json.Compact`.
Only applied to content that passes `json.Valid`. Never applied to user or
assistant messages. Lossless.

Expected savings: 10-25% on JSON-heavy tool results.

### 4.2 Code Comment and Whitespace Stripping (L1.2)

Language-aware removal of single-line comments, multi-line comments, and
excess blank lines from code content in tool results.

Supported languages: Go, TypeScript, JavaScript, Rust, C, C++, Java, Ruby, Shell, Python, CSS, HTML, YAML, TOML.

Language is inferred from the file extension in tool call metadata.
Applied only to messages older than the sliding window.

Expected savings: 5-15% on code-heavy sessions.

### 4.3 Hash-Based Content Deduplication (L1.3)

Detects identical or near-identical content appearing multiple times.

Exact deduplication: SHA256 hash replaced with `[Content identical to message N]`.
Near-duplicate detection: MinHash signatures with Jaccard similarity (threshold 0.85).

Expected savings: 10-20% in typical sessions.

### 4.4 Regex-Based Code Structure Extraction (L1.4)

Replaces large code files in old tool results with structural summaries
(function signatures, type definitions, imports). Uses regex patterns, not
tree-sitter (CGO avoided for build simplicity).

Supported: Go, TypeScript, JavaScript, Rust, Python.
Minimum file size: 500 tokens (configurable).

Expected savings: 40-60% on large code files.

### 4.5 Delta Encoding for File Revisions (L1.5)

Tracks file contents across the conversation. Subsequent appearances of the
same file are replaced with a Myers unified diff against the previous version.

Expected savings: variable; highest for sessions with many edit cycles.

### 4.6 Prompt Cache Optimization (L1.6)

Anthropic-only: injects `cache_control: {type: "ephemeral"}` at stable
breakpoints in the compressible prefix. This enables server-side prompt caching
for old messages, reducing TTFT and upstream token cost.

### 4.7 ANSI Strip (L1.7)

Removes ANSI escape codes, progress bars, and cursor movement sequences
from tool results using `compression.StripANSICodes`.

### 4.8 Tool Classifier (L1.8)

Classifies each tool result by content type using tool name lookup first,
then content pattern matching. Output types: GitOutput, TestOutput,
BuildOutput, LintOutput, FileRead, SearchResult, JSONData, LogOutput,
DirListing, CommandOutput, Unknown.

### 4.9 Tool Output Compressor (L1.9)

Applies RTK-style per-type filters to classified tool results in old messages.
Compression aggressiveness scales with message age (aggressive when age > 2x sliding window):

- Git: stats extraction, diff truncation, commit header preservation
- Test: failure focus, passing line removal, summary preservation
- Build: error/warning extraction, cap at 20 errors in aggressive mode
- Lint: violation grouping, cap at 20 violations
- Log: consecutive duplicate collapse, tail limiting
- Dir: file/dir count summary
- Search: result limiting with "N more matches" indicator

### 4.10 Success Short-Circuit (L1.10)

Detects "0 errors", "all tests passed", "build succeeded", "nothing to commit"
and similar success patterns in old tool results. Replaces with a one-liner.

Expected savings: high for long sessions with many successful build/test runs.

### 4.11 Image Base64 Replacement (L1.11)

Replaces base64-encoded image data in tool results with text descriptors.
Attempts to extract PNG/JPEG dimensions from image headers.
For terminal screenshots, attempts to extract readable text from printable bytes.

Output: `[Image: {type}, {dimensions}, message N]`

### 4.12 Repeated Tool Collapse (L1.12)

Detects tool calls with identical name+input+result appearing multiple times.
Replaces subsequent occurrences with `[Identical to {tool} result in message N]`.
Only fires when the replacement is shorter than the original content.

### 4.13 Conversation Graph Pruning (L1.13)

Builds a file operation dependency graph (Read, Edit, Write nodes).
Prunes a Read at message i when an Edit/Write at message j>i exists and
a later Read at message k>j covers the same file. Safety-checked: does not
prune if any later message text-references the message index.

### 4.14 Pre-Filtered Content Tagging (L1.14)

Detects Layer 0 compact markers on the first line of tool results
(`[git status] ...`, `[git diff] empty`, `[ok]`, `[N matches]`, etc.).
When detected, skips JSON compact (L1.1), comment strip (L1.2), and
structure extraction (L1.4) - these would be redundant on already-compact
content and could mangle the compact format.

Performance savings: ~0.5ms per pre-filtered tool result.

### 3.6 Anthropic Prompt Cache Optimization

For Anthropic requests, `cache_control: {"type": "ephemeral"}` breakpoints
are injected at optimal positions in the message array to enable Anthropic's
server-side prompt caching. This reduces prefill cost on repeated API calls
with the same prefix.

Applied to the system prompt and the oldest compressible message block.

---

## 5. Layer 2: MiniMax Summarization

Layer 2 uses MiniMax M2.7 (OpenAI-compatible endpoint) to intelligently
summarize groups of old messages. It operates asynchronously: summaries are
pre-computed during idle time and stored in a cache. When a request arrives,
the cached summary is applied instantly (no latency). If no summary is cached,
Layer 2 is skipped for that request, and a compression job is enqueued for the
next request.

### 4.1 Sliding Window

The last `sliding_window` exchanges (default: 5) are always excluded from
summarization. Only messages older than this window are candidates.
Anchor messages are also excluded regardless of position (see 4.2).

Layer 2 only activates when:
- At least `min_messages_for_compression` messages exist (default: 8)
- Total input tokens exceed `min_tokens_for_layer2` (default: 30,000)

### 4.2 Anchor Point Detection

Certain messages are marked as anchors and are NEVER summarized:

| Anchor type | Detection trigger |
|-------------|------------------|
| AnchorEdit | Contains file edit/write tool use |
| AnchorError | Contains error trace or failure output |
| AnchorDecision | User confirmed or rejected a plan |
| AnchorArchitect | Contains architecture decisions |
| AnchorConfig | Contains config or env changes |

Anchor detection is performed in `internal/summarization/anchor.go`. Messages
between two anchors may be summarized as a group; anchors themselves are
preserved verbatim in the reconstructed message array.

### 4.3 MiniMax API Client

The client sends summarization requests to the MiniMax M2.7 endpoint using
the OpenAI-compatible chat completions format. Rate-limited to 10 RPM by
default (`rate_limit_rpm`). Connect timeout: 5s. Response timeout: 30s.
Up to 3 retries on transient failures.

The API key is read from the environment variable named by `api_key_env`
(default: `MINIMAX_API_KEY`). If the key is not set, Layer 2 stops scheduling
new summarization jobs and all requests fall through to Layer 1 only.

### 4.4 Summary Cache

The `SummaryCache` struct holds the most recent compressed summary. An
`atomic.Bool` flag (`Compressing`) prevents duplicate concurrent compression
jobs. A summary has a configurable refresh interval (default: 30 min,
`summary_refresh_interval_seconds`).

The cache is thread-safe. The proxy hot path reads from it without locking.

### 4.5 Quality Validation

Before a summary is accepted, `CompressionValidator.Validate()` runs 5 checks:

1. **File path preservation**: >90% of file paths found in the original messages
   (regex: `[./][a-zA-Z0-9_\-/]+\.[a-zA-Z]{1,6}`) must appear verbatim in the summary.
2. **Function name preservation**: >80% of function names found in code blocks
   (extracted only from ``` fences, matching `func`, `def`, `fn` declarations)
   must appear in the summary.
3. **Error string preservation**: >50% of error/panic/fatal phrases from the
   original must appear in the summary (max 40-char prefix match).
4. **Minimum length**: summary token estimate must be >5% of original token count.
5. **Maximum length**: summary token estimate must be <40% of original token count.

Checks use the lightweight summarization token estimator from the Go
implementation, not tiktoken. Checks run in order; the first failure is
returned with a descriptive reason. If validation fails, the summary is
discarded and Layer 2 falls back to Layer 1 only.

### 4.6 Strict summary mode

`[compression.summary].strict = true` is the default. In strict mode:

- code-like blocks are fenced before summarization so identifiers survive the
  prompt boundary more reliably
- validation retries include explicit preservation instructions
- summaries are accepted only if the preservation and ratio checks pass

### 5.6 Progressive Compression (50+ message sessions)

For sessions with 50 or more messages, `progressive.go` applies multi-tier
compression:

- Tier 1 (messages 1-20): aggressive summarization at target ratio 0.10
- Tier 2 (messages 21-40): moderate summarization at target ratio 0.20
- Tier 3 (messages 41+): light summarization (near sliding window)

This prevents a single massive MiniMax call and allows incremental refinement
of older history.

### 5.7 Adaptive Sliding Window (L2.8)

The sliding window size is dynamically adjusted (base +/- 2) based on session
complexity. Complexity score: `0.3*uniqueFiles + 0.4*anchorDensity + 0.3*toolDiversity`.

- Score > 0.7: window expanded by +2 (more context preserved)
- Score < 0.3: window shrunk by -2 (more aggressive compression)
- Range clamped to `[max(3, base-2), base+2]`

High-complexity sessions (many files, many anchors, diverse tools) automatically
get a larger window to preserve more working context.

### 5.8 Tool Result Priority Classification (L2.9)

Before summarization, each tool result is assigned a priority that shapes the
MiniMax prompt:

- **HIGH**: anchor messages, build/test failures, error traces
- **MEDIUM**: file reads, search results
- **LOW**: successful builds, clean test runs, directory listings

The `SummarizationHint(messages)` function produces a structured annotation
injected into the MiniMax summarization prompt, directing the model to preserve
HIGH-priority content verbatim while compressing LOW-priority content more aggressively.

---

## 6. Layer 3: Response Caching

Layer 3 caches full API responses for identical forwarded requests. The cache
key is provider-aware and derived from the canonical forwarded JSON request
body after whitespace and object-key ordering are normalized. Semantically
relevant headers (`Authorization`, `x-api-key`, provider version/beta headers,
organization/project headers) are folded into the key in normalized form, with
secret-bearing values hashed before storage. Explicitly stochastic requests
(`stream=true`, `temperature>0`, `0<top_p<1`, `n>1`) are not cached. On a cache
hit, the stored response is replayed to the CLI without forwarding to the
upstream API, but the request is still accounted for in analytics and debug
summaries as a processed cache hit.

### LRU Cache

Implementation: `internal/caching/response_cache.go`. Uses an LRU eviction
policy with a configurable maximum entry count (default: 100) and TTL
(default: 300s / 5 minutes). The cache is keyed by SHA256 of the effective
forwarded request plus the normalized cache-relevant headers.

Thread-safe via sync.RWMutex. Read path (lookup) acquires only a read lock.

### fsnotify File Watcher

`internal/caching/file_watcher.go` uses the `fsnotify` library to watch
working directories for file changes. Dependency-like file paths are extracted
from request bodies and stored with each cache entry. Before a file-dependent
response is cached, every referenced dependency path must be successfully
armed in the watcher. When a watched file is modified, entries whose tracked
dependency paths match are invalidated. This prevents serving stale responses
after the user edits a referenced file.

The file watcher is optional: if initialization fails, if a dependency watch
cannot be installed, or if the max watched-directory cap would prevent arming
the dependency path, Slimference skips caching that file-dependent response
instead of risking a stale Layer 3 hit. TTL-based expiry still applies to
entries that were safely admitted.

---

## 7. Layer 4: Concurrency Pipeline

Layer 4 is the Go concurrency model that decouples the hot request path from
expensive background operations.

### Key channels

| Channel | Buffer | Direction | Purpose |
|---------|--------|-----------|---------|
| compressQueue | 4 | proxy -> compressionWorker | Async Layer 2 jobs |
| analyticsQueue | 256 | proxy -> analyticsWorker | Analytics event fan-out |
| shutdownCh | 0 | main -> workers | Graceful shutdown signal |

### compressionWorker

Reads `types.CompressJob` from `compressQueue`. Each job contains the message
array from the most recently completed request. The worker calls `layer2.RunCompressionJob`,
which calls MiniMax, validates the summary, and stores it in `SummaryCache`.

If the queue is full when a new job is submitted, the job is dropped (non-blocking
send). This prevents the hot path from ever blocking on slow MiniMax responses.

### analyticsWorker

Reads `types.AnalyticsEvent` from `analyticsQueue` and calls `analytics.Record`.
The ring buffer and all counters in `Analytics` are protected by a single mutex
that is only held inside `Record`. The hot path never takes this mutex; it only
does a **non-blocking** channel send (`select { case q <- event: default: }`).
If the queue is full the event is silently dropped - analytics are best-effort
and must never block HTTP handlers. During shutdown, the worker drains already
queued events before exiting so final request counters and recent-request
history are not lost.

### Graceful shutdown

`Proxy.Shutdown(ctx)` is idempotent via `sync.Once`: calling it multiple times
(e.g. concurrent signal + TUI quit) is safe and produces no panic. The first
caller runs the full shutdown sequence: `server.Shutdown(ctx)`, close of `shutdownCh`,
`wg.Wait()` with timeout, final analytics JSONL flush, `FileWatcher.Close()`.
The watcher close path is itself idempotent, so repeated internal cleanup is
also safe. Subsequent callers return immediately.

---

## 8. Provider Support

### Anthropic

- Endpoint: `/v1/messages`
- Auth: `x-api-key` header and `anthropic-version` header passed through verbatim
- Message format: `{"role": "user"|"assistant", "content": [...]}`
- Content types handled: `text`, `tool_use`, `tool_result`, `image`
- SSE event format: `event: content_block_delta` with `delta.text` increments
- Prompt cache: `cache_control: {"type": "ephemeral"}` injected at Layer 1
- Token counting: output token count extracted from `usage.output_tokens` in
  the final SSE `message_delta` event

### OpenAI

- Endpoint: `/v1/chat/completions`
- Auth: `Authorization: Bearer <token>` header passed through verbatim
- Message format: `{"role": "user"|"assistant"|"system", "content": "..."}`
- Content types handled: `text`, `tool_calls`, `tool` (tool results)
- SSE event format: `data: {"choices": [{"delta": {"content": "..."}}]}`
- Token counting: extracted from `usage.completion_tokens` in the final chunk

### OAuth passthrough

For subscription-based CLIs (Claude Code Max, GitHub Copilot), the CLI manages
OAuth tokens internally and includes them in the `Authorization` header. The
proxy passes these headers through without modification. No API key configuration
is required for OAuth-authenticated CLIs; only a network intercept is needed.

### Provider invisibility contract

The proxy MUST be architecturally undetectable by upstream providers:

1. All original headers from the CLI are forwarded verbatim (including
   `User-Agent`, `anthropic-version`, `x-api-key`, `Authorization`)
2. No proxy-identifying headers are added (`X-Forwarded-For`, `Via`, etc.)
3. TLS connections to upstream use the system certificate store
4. SSE chunks are relayed byte-for-byte without buffering or reframing
5. HTTP status codes and error bodies from upstream are passed through unchanged
6. The proxy never modifies the `model` field or any request parameter other
   than the `messages` array

---

## 9. Security

### Secret detection

`internal/security` scans all message content for secrets before forwarding
to the upstream API. Detection runs after Layer 1 and before the upstream
request is sent.

### Default patterns (13 built-in rules)

| Pattern name | Detection method |
|---|---|
| AWS Access Key | `AKIA[0-9A-Z]{16}` |
| AWS Secret Key | key=value regex |
| GitHub Token | `ghp_` prefix |
| GitHub Fine-grained Token | `github_pat_` prefix |
| Anthropic API Key | `sk-ant-` prefix |
| OpenAI API Key | `sk-` prefix (48+ chars) |
| Stripe Key | `sk_`/`pk_` + test/live |
| Generic Bearer Token | Authorization header value |
| Password in Config | key=value for password/secret/token |
| Env Secret Value | DATABASE_URL, REDIS_URL, etc. |
| RSA Private Key | PEM header |
| SSH Private Key | OpenSSH PEM header |
| High-Entropy String | 40+ char base64 with Shannon entropy >4.5 bits |

### Detection modes

| Mode | Behavior |
|------|----------|
| `redact` (default) | Replace matched text with `[REDACTED:PatternName]` |
| `warn` | Log warning, pass through unchanged |
| `block` | Return 400 error to CLI, request not forwarded |
| `off` | No scanning |

### Custom patterns

Users can add custom regex patterns via `[[secrets.custom_patterns]]` in
config. Each pattern requires a `name` and `regex` field. An optional
`allowlist` array of literal substrings skips matches containing those strings
(useful for test fixtures or known-safe values).

### Shannon entropy filter

The high-entropy string pattern includes a minimum entropy threshold (4.5 bits).
Random-looking strings below the threshold (e.g., test placeholders) are not
flagged. This reduces false positives significantly.

---

## 10. Debug & Observability

### Decision Recorder

Every proxy request is recorded as a `RequestSummary` in an in-memory ring buffer
(last 100 requests). Each summary contains:
- Request metadata (provider, model, timestamp, request ID)
- Token counts before/after each layer
- Per-sub-layer savings breakdown (`layer1_breakdown` map)
- Layer 2 application details and cache hit status

The ring buffer can be flushed to a JSONL file by setting `decisions_log` in
`[debug]` config or `SLIMFERENCE_DEBUG_DECISIONS_LOG` env var.

### CLI commands

```bash
# Show last proxy request decision tree
slimference debug last

# Show last 5 requests (JSON)
slimference debug last 5 --json

# Show Layer-0 filter.db summary
slimference debug summary today|week|month|all

# Show newest 20 filter_runs from SQLite
slimference debug tail [N] [--json]

# Preview session JSONL file stats
slimference debug replay session.jsonl

# Show resolved paths (config, filter.db, tee, decisions log)
slimference debug paths
```

### Log levels

| Level | Content | Cost per request |
|-------|---------|-----------------|
| trace | Every sub-layer decision per block | ~500-2000 tokens |
| debug | Per-request summary with layer breakdown | ~200-400 tokens |
| info | Session-level aggregates | ~50-100 tokens |
| warn/error | Problems only | ~0-50 tokens |

Set via `[logging] level = "debug"` or `SLIMFERENCE_LOGGING_LEVEL`.

### Request-scoped logging

Every call inside `handleCompressibleRequest` uses a logger enriched with
per-request fields:

```go
log := slog.With("req_id", reqID, "provider", provider, "model", model)
```

Key `debug`-level events emitted per request (all include req_id):

| Event | Fields |
|-------|--------|
| `request started` | `messages`, `orig_tokens` |
| `layer1 applied` | `json_saved`, `dedup_saved`, `structure_saved`, `total_saved` |
| `layer2 applied` | `saved` |
| `request_processed` | `input_orig`, `input_comp`, `saved`, `output`, `ratio`, `layers`, `latency_ms` |
| `cache hit` | (none; req_id sufficient) |

### Layer 0 filter debug logging

`filter/pipeline.go` emits debug events for every filter run:

| Event | Fields |
|-------|--------|
| `layer0 exec` | `argv0`, `exit_code`, `stdout_bytes`, `stderr_bytes` |
| `layer0 filter applied` | `filter` (e.g. `"git_status"`), `in_bytes`, `out_bytes` |
| `layer0 passthrough` | `in_bytes` |
| `layer0 result` | `argv0`, `in_tokens`, `out_tokens`, `savings_pct` |

---

## 11. TUI Dashboard

The TUI is built with BubbleTea (event loop) and Lipgloss (styling). It runs
in the alternate screen buffer and updates every 500ms via a tick command.
The proxy pushes `proxyEventMsg` to the TUI program for immediate refresh on
each new request. Version is displayed as `SLIMFERENCE v2.0.2` and is sourced
from `internal/buildinfo.Version`, which also feeds the CLI and `/health`
endpoint.

### Main view layout

The main view uses a two-column layout separated by a `│` divider:

```
 SLIMFERENCE v2.0.2                              ◷ 12m 34s  :8990
 ────────────────────────────────────────────────────────────────
 PROVIDERS              │ SAVINGS
  ● Claude Code  [ON] ● │  35%  12.4K → 8.1K  4.3K saved
  ● Codex        [ON] ○ │  ████████░░░░░░  35%
                         │  +12 msgs  ~1.2s TTFT
 LAYERS                  │
  [1] Deterministic ● ON │ LIVE
      struct · delta ·   │  15:04:23  Claude  sonnet  -35%
  [2] MiniMax       ● ON │  15:04:19  Codex   gpt-4o  hit
  [3] Cache         ● ON │  Waiting for requests...
      hits: 3/10          │
                         │
 HOOKS                   │
  Hooks: claude ✓         │
 ────────────────────────────────────────────────────────────────
 [c] claude · [x] codex · [1-3] layers · [s] stats · [q] quit
```

**Left column** (32-36 chars): PROVIDERS section with health dots, LAYERS section
with per-layer savings and subtitle (e.g. `struct · delta · dedup`), HOOKS section
(only rendered when at least one hook is installed).

**Right column** (remainder): SAVINGS section with big compression percentage,
progress bar, and gain line (`+N msgs  ~X.Xs TTFT`). Below that: LIVE request
log or QUICK START onboarding panel.

**QUICK START panel** is shown in the right column when `TotalRequests == 0` and
no hooks are installed - displays the two `slimference hook install` commands and
step-by-step instructions.

**Header**: `SLIMFERENCE v2.0.2` (gold, bold) aligned left, session duration and
port right-aligned. Separated from the body by a `─` horizontal rule.

**Footer**: styled keyboard hints `[c] claude · [x] codex · [1-3] layers · ...`
using purple Key style and dim-gray separator dots. Separated by a `─` rule above.

### Views

| Key | View | Content |
|-----|------|---------|
| (default) | Main | Two-column: providers+layers+hooks left, savings+live right |
| `s` | Stats | Detailed per-provider stats, layer savings breakdown, latency table |
| `d` | Debug | Scrolling session log tail (30 entries) with level-colored output |

### Key bindings

| Key | Action |
|-----|--------|
| `c` | Toggle Claude Code (Anthropic) compression on/off |
| `x` | Toggle Codex (OpenAI) compression on/off |
| `1` | Toggle Layer 1 on/off |
| `2` | Toggle Layer 2 on/off |
| `3` | Toggle Layer 3 on/off |
| `s` | Switch to stats view (toggle back to main) |
| `d` | Switch to debug log view (toggle back to main) |
| `f` | Flush all caches (response cache + Layer 2 summary cache) |
| `q` / `Ctrl+C` | Graceful shutdown |

Toggles are applied immediately via atomic.Bool writes on the Proxy struct.
A flash message confirms the new state for 2 seconds.

### Lipgloss color palette and styles

**Colors:**
- Purple (ANSI 99): panel titles, key hints, borders
- Green (ANSI 78): savings, ON indicators, BigSaved
- Green dim (ANSI 34): progress bar filled blocks
- Gold (ANSI 220): main title, flash messages
- Orange (ANSI 215): warnings
- Red (ANSI 203): errors
- Blue (ANSI 75): INFO log level
- Cyan (ANSI 87): active border highlight, highlight values
- Grays (ANSI 240-255): secondary and muted text

**Named styles (Styles struct):**
- `PanelTitle`: purple bold - section headers inside panels (PROVIDERS, LAYERS, etc.)
- `Divider`: dim gray - `│` vertical column separator
- `HorizRule`: dim gray - `─` horizontal rule lines
- `Key`: purple bold - keyboard hint brackets `[c]`
- `KeySep`: dim gray - `·` dot separator between key groups
- `BigSaved`: green bold - large compression percentage (e.g. `35%`)
- `SetupCmd`: cyan on dark background - quick-start command blocks
- `SetupTitle`: gold bold - QUICK START heading

### Hook status

The HOOKS section in the left column is rendered only when at least one hook is
installed. Each tool shows `✓` (green) or `-` (muted gray):

```
Hooks: claude ✓  codex ✓
```

If neither hook is installed and no requests have arrived, the right column shows
the QUICK START onboarding panel instead of the live log.

Hook status is read once at startup via `hooks.InstalledStatus(home)`:
- **Claude Code**: checks for `~/.claude/hooks/slimference-rewrite.sh`
- **Codex**: checks for a coherent `~/.codex/hooks.json` install, with a legacy
  AGENTS.md marker accepted only as a fallback signal

The result is passed to the TUI via `model.SetHookStatus(tui.HookStatus{...})`
before the BubbleTea program starts.

### Session log

The debug view reads from `sessions.SessionLogger`, which maintains a 200-entry
ring buffer. New entries are pushed from the proxy hot path without blocking
(subscriber channel buffer: 50; drops on overflow). The TUI subscribes via
`SessionLogger.Subscribe()` and formats entries as:
`HH:MM:SS LEVEL component: message key=value...`

Sends to subscriber channels use `trySend()` which wraps the send in `recover()`.
This prevents a panic when `Unsubscribe()` closes a channel while `Log()` still
holds a stale reference to it from a prior lock acquisition.

---

## 12. Configuration

Config file location: `~/.slimference/config.toml` (default).
Override: `SLIMFERENCE_CONFIG=/path/to/config.toml`.
Precedence: CLI flags > environment variables > config file > defaults.

Generate default config: `slimference config init`

### [proxy]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `listen_address` | string | `"127.0.0.1"` | Bind address for the HTTP listener |
| `listen_port` | int | `8990` | Port number (1-65535) |
| `ipv6` | bool | `false` | Enable IPv6 dual-stack |

### [upstream.anthropic]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `base_url` | string | `"https://api.anthropic.com"` | Anthropic API base URL |

### [upstream.openai]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `base_url` | string | `"https://api.openai.com"` | OpenAI API base URL |

### [compression]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `layer1_enabled` | bool | `true` | Enable deterministic compression |
| `layer2_enabled` | bool | `true` | Enable MiniMax summarization |
| `layer3_enabled` | bool | `true` | Enable response caching |
| `sliding_window` | int | `5` | Number of most-recent exchanges never compressed |
| `min_messages_for_compression` | int | `8` | Minimum messages before any compression runs |
| `min_tokens_for_layer2` | int | `30000` | Minimum tokens before Layer 2 activates |
| `structure_min_tokens` | int | `500` | Minimum file size for structural extraction |
| `structure_languages` | []string | `["go","typescript","javascript","rust","python","c","cpp","java","ruby","shell"]` | Languages for structure extraction (10 supported) |
| `dedup_similarity_threshold` | float | `0.85` | Jaccard similarity threshold for near-dedup |

### [compression.minimax]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `base_url` | string | `"https://api.minimax.io/v1"` | MiniMax API base URL |
| `api_key_env` | string | `"MINIMAX_API_KEY"` | Env var name holding the API key |
| `model` | string | `"minimax-m2.7"` | Model identifier |
| `temperature` | float | `0` | Summarization temperature (low = deterministic) |
| `max_retries` | int | `3` | Retries on transient failures |
| `connect_timeout_seconds` | int | `5` | TCP connect timeout |
| `response_timeout_seconds` | int | `30` | Full response timeout |
| `rate_limit_rpm` | int | `10` | Max requests per minute to MiniMax |

### [compression.summary]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `target_ratio` | float | `0.20` | Target summary length as fraction of original |
| `max_ratio` | float | `0.40` | Maximum acceptable summary length ratio |
| `min_ratio` | float | `0.05` | Minimum acceptable summary length ratio |
| `strict` | bool | `true` | Enable strict summary formatting and validation |

### [cache]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `response_cache_max_entries` | int | `100` | LRU capacity |
| `response_cache_ttl_seconds` | int | `300` | Response cache TTL (5 minutes) |
| `summary_refresh_interval_seconds` | int | `1800` | Layer 2 summary refresh interval (30 min) |

### [usage]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `estimated_prefill_speed` | int | `50000` | Tokens/second for TTFT improvement estimates |

### [secrets]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `mode` | string | `"redact"` | Detection mode: `redact`, `warn`, `block`, `off` |
| `custom_patterns` | []object | `[]` | Additional `{name, regex}` patterns |
| `allowlist` | []string | `[]` | Literal substrings exempt from detection |

### [analytics]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `dashboard` | bool | `true` | Enable TUI dashboard |
| `log_dir` | string | `"~/.slimference/analytics"` | JSONL analytics log directory |
| `dashboard_refresh_seconds` | int | `2` | TUI tick interval |

### [logging]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `level` | string | `"debug"` | Log level: `debug`, `info`, `warn`, `error` |
| `format` | string | `"json"` | Log format: `text` or `json` (JSONL for machine consumption) |
| `file` | string | `"~/.slimference/logs/slimference.jsonl"` | Log file path; empty = stderr only |

The log file is managed by `internal/slogutil.RotatingWriter`: 10 MB per file,
5 rotated copies (`.1` - `.5`). Old files are renamed atomically on rotation.
Rotation is goroutine-safe. When `file` is set, all `slog.*` calls (from all
packages) go to the rotating JSONL file; stderr output is suppressed.
Every log line includes `req_id`, `provider`, and `model` fields when emitted
from the hot path, enabling per-request filtering with `jq`.

```bash
# Watch live compressed-request logs:
tail -f ~/.slimference/logs/slimference.jsonl | jq 'select(.msg == "request_processed")'

# Filter by request ID:
jq 'select(.req_id == "a3f7c2b1d4e8f609")' ~/.slimference/logs/slimference.jsonl
```

### Environment variable overrides

| Env var | Config key |
|---------|------------|
| `SLIMFERENCE_CONFIG` | Config file path |
| `SLIMFERENCE_LISTEN_ADDRESS` | proxy.listen_address |
| `SLIMFERENCE_LISTEN_PORT` | proxy.listen_port |
| `SLIMFERENCE_UPSTREAM_ANTHROPIC_BASE_URL` | upstream.anthropic.base_url |
| `SLIMFERENCE_UPSTREAM_OPENAI_BASE_URL` | upstream.openai.base_url |
| `SLIMFERENCE_COMPRESSION_SLIDING_WINDOW` | compression.sliding_window |
| `SLIMFERENCE_SECRETS_MODE` | secrets.mode |
| `SLIMFERENCE_LOGGING_LEVEL` | logging.level |
| `MINIMAX_API_KEY` | compression.minimax API key |

---

## 13. CLI Commands

### slimference (no arguments)

Starts the proxy server and the BubbleTea TUI dashboard. Blocks until the user
presses `q` or sends SIGINT/SIGTERM.

CLI flags (all optional, override config file and env vars):

| Flag | Description |
|------|-------------|
| `--port <n>` | Listen port override |
| `--sliding-window <n>` | Layer 1 sliding window size |
| `--no-layer1` | Disable Layer 1 (deterministic compression) |
| `--no-layer2` | Disable Layer 2 (MiniMax summarization) |
| `--no-layer3` | Disable Layer 3 (response caching) |
| `--log-level <level>` | Log level: `debug`, `info`, `warn`, `error` |

```
slimference
slimference --port 9000 --no-layer2
slimference --log-level debug --sliding-window 3
```

### GET /health

Returns the current proxy status as JSON. All fields are live (not cached).

```json
{
  "status": "ok",
  "service": "slimference",
  "version": "2.0.2",
  "layers": {"1": true, "2": true, "3": false},
  "providers": {"anthropic": true, "openai": true},
  "queue_depth": {"compress": 0, "analytics": 0},
  "cache_entries": 12,
  "minimax_configured": true
}
```

| Field | Description |
|-------|-------------|
| `status` | Always `"ok"` when the proxy is running |
| `service` | Always `"slimference"` |
| `version` | Binary version string |
| `layers` | Live on/off state for layers 1, 2, 3 |
| `providers` | Live on/off state for anthropic, openai |
| `queue_depth.compress` | Items pending in the async Layer 2 compression queue |
| `queue_depth.analytics` | Items pending in the analytics event queue |
| `cache_entries` | Current number of entries in the Layer 3 response cache |
| `minimax_configured` | `true` if `MINIMAX_API_KEY` is set |

### slimference config init

Writes a default config file to `~/.slimference/config.toml`. Exits without
overwriting if the file already exists.

```
slimference config init
```

### slimference config show

Loads and prints the fully resolved configuration as JSON (including env
variable overrides applied).

```
slimference config show
```

### slimference test minimax

Checks that `MINIMAX_API_KEY` is set and that the MiniMax endpoint is
reachable.

```
MINIMAX_API_KEY=sk-xxx slimference test minimax
```

### slimference test anthropic

Checks that the Anthropic base URL is reachable (HTTP GET, any status).

```
slimference test anthropic
```

### slimference test openai

Checks that the OpenAI base URL is reachable.

```
slimference test openai
```

### slimference test intercept <claude|codex>

Starts a minimal HTTP listener on the configured port and waits for the
specified CLI to send a request. Prints request details on receipt.
Useful for verifying that the CLI is correctly pointing at the proxy.

```
# Terminal 1:
slimference test intercept claude

# Terminal 2:
ANTHROPIC_BASE_URL=http://127.0.0.1:8990 claude "say hi"
```

### slimference doctor

Runs all diagnostic checks in sequence and prints a pass/fail report:
- Config file location
- Listen port
- MiniMax API key presence
- Anthropic upstream reachability
- OpenAI upstream reachability
- Analytics log directory writability

```
slimference doctor
```

### slimference stats <today|week|month>

Reads JSONL analytics logs and prints an aggregated stats table:
messages sent, tokens original/saved, cache hits, MiniMax calls,
secrets redacted.

```
slimference stats today
slimference stats week
slimference stats month
```

### slimference filter -- <cmd> [args...]

Runs a subprocess and applies Layer 0 filters to stdout. Used internally by
shell hooks; can also be called directly for testing.

```bash
slimference filter -- git status
slimference filter -- go test ./...
```

Exit code propagated from the subprocess. On subprocess failure, raw output
is saved to `~/.slimference/tee/` and a recovery hint appended to filtered output.

### slimference hook install|remove <agent>

Manages shell hooks for LLM agent interception. Supported agents:
`claude`, `codex`.

```bash
slimference hook install claude    # install Claude Code PreToolUse hook
slimference hook install codex     # install Codex PreToolUse + PostToolUse hooks and patch config.toml
slimference hook remove claude     # uninstall one target
slimference hook remove codex
```

### slimference hook verify|status

Manages shell hooks for LLM agent interception. Supported agents:
`claude`, `codex`.

```bash
slimference hook verify            # verify installed hook files and config coherence
slimference hook status            # show installed/missing for supported agents
```

### slimference rewrite <json>

JSON stdin hook path: reads JSON tool invocation from stdin, applies command
rewriting rules, prints rewritten JSON or deny response.

Exit codes: 0=allow, 1=usage+JSON, 2=deny, 3=sudo-ask.

### slimference posttool

Reads a Codex PostToolUse hook payload from stdin, compacts captured Bash
output, and emits `hookSpecificOutput.additionalContext` when the compacted
form adds signal.

```bash
cat posttool.json | slimference posttool
```

### slimference gain <today|week|month|all>

Reads `~/.slimference/filter.db` and shows Layer 0 token savings summary.

```bash
slimference gain today
slimference gain week --json
slimference gain all --by-command
slimference gain today --project /path/to/project
```

### slimference debug last [N] [--json]

Shows the last N proxy request decision trees from the in-memory ring buffer.

```bash
slimference debug last          # last request
slimference debug last 5 --json # last 5 as JSON
```

### slimference debug summary <today|week|month|all>

Aggregates Layer 0 filter.db entries and prints a savings summary table.

### slimference debug tail [N] [--json]

Shows the N most recent entries from filter.db (default: 20).

### slimference debug paths

Shows resolved paths for config file, filter.db, tee directory, decisions log.

### slimference debug replay <session.jsonl>

Preview mode: shows statistics from a session JSONL file without replaying.

### slimference version

Prints the version string.

```
slimference version
# slimference v2.0.2
```

### Offline savings reports (`scripts/utils`)

Offline reporting helpers under `scripts/utils/` aggregate persisted savings
data without talking to any provider:

```bash
go run ./scripts/utils session-report ~/.slimference/analytics/2026-04-17.jsonl
go run ./scripts/utils decision-report ~/.slimference/logs/decisions.jsonl --json
go run ./scripts/utils filter-report ~/.slimference/filter.db --csv
go run ./scripts/utils combined-report ~/.slimference/analytics/2026-04-17.jsonl \
  ~/.slimference/logs/decisions.jsonl \
  ~/.slimference/filter.db
```

`session-report` reads analytics JSONL, `decision-report` reads debug
`RequestSummary` JSONL, `filter-report` queries Layer 0 SQLite tracking, and
`combined-report` merges proxy savings with estimated Layer 0 savings into one
offline view. Each subcommand supports plain text by default plus `--json` and
`--csv`.

---

## 14. Installation and Setup

### Prerequisites

- Go 1.24 or later
- A MiniMax API key (for Layer 2; optional but recommended)

### Step 1: Build

```bash
cd /path/to/Slimference
go mod tidy
go build -o slimference ./cmd/slimference
```

### Step 2: Generate default config

```bash
./slimference config init
# Writes ~/.slimference/config.toml
```

### Step 3: Set MiniMax API key

```bash
export MINIMAX_API_KEY=your-minimax-key
# Or add to ~/.bashrc / ~/.zshrc
```

### Step 4: Run diagnostics

```bash
./slimference doctor
```

All checks should pass. If `MiniMax API key` fails, Layer 2 will be
disabled but the proxy will still run.

### Step 5: Configure Claude Code

```bash
./slimference hook install claude
export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
```

The hook installs Claude's PreToolUse rewrite script. Add the base URL export
to your shell profile so it persists across terminal sessions. When Claude Code
starts, it will send all API requests through the proxy.

### Step 6: Configure OpenAI Codex

```bash
./slimference hook install codex
```

This writes Codex `PreToolUse` and `PostToolUse` entries into
`~/.codex/hooks.json`, patches `~/.codex/config.toml` with
`openai_base_url = "http://127.0.0.1:8990"` and `codex_hooks = true` if those
keys are not already present, and keeps a legacy `AGENTS.md` fallback block for
older Codex versions. `slimference hook remove codex` removes only
Slimference-managed config additions and preserves unrelated user-owned
`[features]` entries.

### Step 7: Start the proxy

```bash
./slimference
```

The TUI dashboard opens. Start using Claude Code or Codex normally.
Watch the token savings counter climb.

### Verify interception

```bash
# With proxy running in another terminal:
slimference test intercept claude
# In another terminal:
ANTHROPIC_BASE_URL=http://127.0.0.1:8990 claude "say hi"
```

---

## 15. Testing Strategy

### Unit tests (per package)

Each package has a `*_test.go` file with table-driven tests following the
standard Go pattern (`[]struct{name, input, want}`). Tests use interface
injection for all external dependencies; no concrete type is mocked directly.

Key packages and their test focus:

| Package | Test focus |
|---------|-----------|
| compression | Layer 1 transforms: JSON compact, comment strip, dedup, delta |
| security | Pattern matching, entropy filter, allowlist, all modes |
| config | TOML parsing, env overrides, validation edge cases |
| tokens | Token counting accuracy vs known inputs |
| analytics | Counter accumulation, snapshot correctness, ring buffer |
| summarization | Anchor detection (5 types), validator 5 checks (file paths, func names, errors, min/max length) |
| caching | LRU eviction, TTL expiry, file watcher invalidation |
| sessions | Ring buffer, subscriber fan-out, format output |

### Integration tests

Integration coverage added in `internal/proxy/handler_compressible_test.go`:

- `TestServeHTTP_promptCacheBreakpointsInjected`: starts a real httptest.Server
  as upstream, sends a 7-exchange Anthropic conversation, verifies that
  `cache_control: {type: "ephemeral"}` breakpoints appear in the upstream request.
  Covers the full `CompressiblePrefixEnd` + `OptimizeCacheBreakpoints` pipeline.

Planned additions:

- `summarization/integration_test.go`: real MiniMax call (skipped if
  `MINIMAX_API_KEY` not set)

To run the repository proof stack:

```bash
go test ./...
go test -race ./...   # race detector (all packages clean)
go test -count=1 -cover ./cmd/... ./internal/...
go run ./scripts/ci
bun test tests/ts
```

**Current proof status:** `cmd/...` and `internal/...` are at `100.0%` Go
coverage, `go test -race ./...` is green, and `tests/ts` is green.

**Race detector note:** Three
`internal/caching` tests that interact with the OS kqueue/inotify backend
(`TestFileWatcher_run_chmodEventIgnored`, `TestFileWatcher_run_errorsChannelClose`,
`TestFileWatcher_scheduleDebounce_callsOnChange`) are serialized (no `t.Parallel()`)
because the OS-level event backend cannot be safely shared across concurrent tests.

### Test integrity rules

- Tests test REAL behavior of REAL code. No mocking that bypasses logic.
- If a test fails, the code is wrong, not the test.
- Every test must be able to fail when the code is broken.
- No `assert(true)`, no hardcoded expected values matching fake output.

---

## 16. Package Structure

```
github.com/slimference/slimference
|
+-- cmd/slimference/
|   main.go                  Entry point: CLI dispatch, TUI startup, proxy adapter wiring,
|                            all subcommands (filter, hook, rewrite, gain, debug, stats, etc.)
|
+-- internal/
    |
    +-- types/
    |   types.go             Core shared types: Message, ContentBlock, RingBuffer,
    |                        AnalyticsEvent, RequestMetrics, Provider, AnchorType,
    |                        CompressionLevel, CompressJob, ToolResultType, ToolResultPriority
    |
    +-- config/
    |   config.go            Config struct, Load(), env overrides, validation, ExpandHomePath()
    |   defaults.go          Defaults(), DefaultTOML()
    |
    +-- tokens/
    |   counter.go           Token counting via tiktoken cl100k_base
    |   usage.go             UsageTracker: per-provider token accumulation
    |
    +-- security/
    |   patterns.go          DefaultPatterns (13 rules), SecretPattern, shannonEntropy
    |   secrets.go           Detector struct, ScanMessages(), scanText(), allowlist
    |
    +-- compression/
    |   layer1.go            DeterministicCompressor orchestrator, Compress(), Reset()
    |   json_compact.go      JSON minification via encoding/json.Compact (L1.1)
    |   comment_strip.go     Language-aware comment/whitespace removal, 14 languages (L1.2)
    |   dedup.go             SHA256 exact dedup + MinHash near-dedup (L1.3)
    |   dedup_minhash.go     MinHash signature generation and Jaccard estimation
    |   structure.go         Regex-based code structure extraction, 5 languages (L1.4)
    |   delta.go             File revision tracking and Myers unified diff (L1.5)
    |   prompt_cache.go      Anthropic cache_control breakpoint injection (L1.6)
    |   ansi_strip.go        ANSI escape code and progress bar removal (L1.7)
    |   tool_classifier.go   Tool result type classification by name+content (L1.8)
    |   tool_compressor.go   RTK-style per-type output compression (L1.9)
    |   success_shortcircuit.go  Success pattern -> one-liner replacement (L1.10)
    |   image_replace.go     base64 image replacement with text descriptor (L1.11)
    |   repeated_collapse.go Identical tool call deduplication across messages (L1.12)
    |   graph_pruning.go     File operation dependency graph pruning (L1.13)
    |   prefilter_tag.go     Layer 0 compact marker detection, skips redundant ops (L1.14)
    |   lang.go              Language detection from file extension
    |
    +-- summarization/
    |   layer2.go            Layer2 coordinator: ApplyToMessages(), RunCompressionJob()
    |   minimax.go           MiniMax M2.7 API client (OpenAI-compatible endpoint)
    |   anchor.go            Anchor point detection (edit/error/decision/config/architect)
    |   validator.go         Summary quality validation (5 checks, min compression ratio)
    |   cache.go             SummaryCache with atomic Compressing flag
    |   progressive.go       Multi-tier compression for 50+ message sessions
    |   adaptive_window.go   Complexity-driven window sizing: 0.3*files + 0.4*anchors + 0.3*tools (L2.8)
    |   priority.go          HIGH/MEDIUM/LOW priority classification + SummarizationHint (L2.9)
    |
    +-- caching/
    |   response_cache.go    LRU response cache with canonical full-request SHA256 key
    |   file_watcher.go      fsnotify watcher for path-based cache invalidation
    |
    +-- analytics/
    |   collector.go         Analytics struct, Record(), Snapshot(), ProviderStats
    |   persistence.go       JSONL logging, ReadDailyStats(), ReadWeeklyStats()
    |   gain.go              slimference gain: filter savings by period/command/project
    |
    +-- resilience/
    |   retry.go             HTTP retry with exponential backoff and jitter
    |   health.go            Upstream health checks and reachability probes
    |   latency.go           Per-provider running-average latency tracking
    |
    +-- sessions/
    |   logger.go            SessionLogger: ring buffer (200 entries), Subscribe(), Format()
    |   export.go            AggregateFromSnapshots(), FormatStatsTable(), session export
    |
    +-- debug/
    |   session.go           SessionFileStats(), ReplaySession(): JSONL parse + replay
    |   decisions.go         Recorder ring buffer, DecisionEntry, RequestSummary types,
    |                        Last(), Aggregate(), flushJSONL()
    |
    +-- filter/
    |   engine.go            RunCommand(), EstimateTokensFromBytes()
    |   pipeline.go          RunPipeline(): ANSI strip + filter dispatch + truncation
    |   dispatch.go          ClassifyCommand()
    |   passthrough.go       TruncateStdoutWithHint()
    |   rewrite.go           Command rewriting engine + JSON stdin extraction
    |   permissions.go       DeniedShellCommand(), AskRequired(), SetExtraDenyPatterns()
    |   tee.go               WriteTeeRecovery(): raw output preservation on failure
    |   tracking.go          SQLite filter_runs: RecordFilterRun(), RecentFilterRuns(), etc.
    |   filters_toml.go      TOML DSL: 8-stage transform pipeline, project+user merge
    |   paths.go             Path resolution for filter.db, tee dir, project filters
    |   project_filters.go   Project-level filter DSL loading
    |   npx_argv.go          npx/pnpm exec/yarn argv normalization for dispatch
    |   pipeline_test.go     (+ extensive test suite in *_test.go files)
    |   builtin_compact_helpers.go  shared label/empty-line detection helpers
    |   builtin_git.go       F01-F05: git status/log/diff/show/push/pull compact
    |   builtin_read.go      F06: cat/head/tail with comment strip for known extensions
    |   builtin_build.go     F07: 30+ build tool filters (go, cargo, tsc, cmake, ...)
    |   builtin_testrun.go   F08: 40+ test runner filters (go test, pytest, jest, ...)
    |   builtin_lint.go      F09: 50+ linter filters (golangci-lint, eslint, ruff, ...)
    |   builtin_search.go    F10: rg/grep/fd/git grep compact output
    |   builtin_fs.go        F11: ls, tree compact listing
    |   builtin_pkg.go       F12: npm/cargo/pip/uv package manager output
    |   builtin_container.go F13: docker/kubectl/helm compact output
    |   builtin_json.go      F14: JSON stdout minification
    |   builtin_log.go       F15: docker/kubectl log deduplication
    |   builtin_aws.go       F16: AWS CLI JSON metadata stripping
    |   builtin_gh.go        F18: gh/GitHub CLI compact output
    |   builtin_glab.go      F18: glab/GitLab CLI compact output
    |   builtin_psql.go      F19: psql compact output
    |   builtin_dotnet.go    F20: dotnet build/test compact output
    |   builtin_ruby.go      F21: rake/rspec/rubocop compact output
    |   builtin_format.go    F24: 20+ formatter ok-detection (prettier, gofmt, ...)
    |
    +-- hooks/
    |   claude.go            Claude Code structured PreToolUse hook + settings.json merge/remove
    |   codex.go             Codex hooks.json PreToolUse/PostToolUse install, config patch,
    |                        legacy AGENTS.md helper block
    |   verify.go            InstalledStatus(home), VerifyReport(home)
    |
    +-- proxy/
    |   proxy.go             Proxy struct, New(), Start(), Shutdown(), toggle atomics,
    |                        DebugRecorder(), background goroutine lifecycle
    |   handler.go           handleCompressibleRequest() hot path, buildLayer1Breakdown(),
    |                        newRequestID(), context overflow retry, analytics emission
    |   provider.go          Provider detection, message extraction/reconstruction
    |   streaming.go         SSE relay, token counting from stream events
    |
    +-- tui/
    |   model.go             Model, Update(), Init(), ProxyInterface, SessionLoggerInterface,
    |                        ProxyConfigInterface, Layer2Status, HookStatus, SetHookStatus()
    |   views.go             renderMainView() (two-column layout), renderStatsView(),
    |                        renderDebugView(), renderHeader(), renderFooterBar(),
    |                        buildLeftPanel(), buildRightPanel(), renderHookStatus()
    |   styles.go            Lipgloss color palette and Styles struct (incl. PanelTitle,
    |                        Divider, HorizRule, Key, KeySep, BigSaved, SetupCmd, SetupTitle)
    |   components.go        Progress bar, badges, table renderer, log line renderer
    |   keys.go              KeyMap, DefaultKeyMap(), footerHelp()
    |
    +-- slogutil/
    |   rotating.go          RotatingWriter: goroutine-safe io.Writer; rotates at 10 MB,
    |                        keeps 5 copies; used by setupLogging() as slog.Handler backend
    |
    +-- util/
        safego.go            Safe goroutine launcher with panic recovery and slog logging

+-- scripts/
    +-- coverage/            Coverage gate and reporting scripts (Go)
    +-- benchmarks/          Benchmark runners (Go)
    +-- utils/               Miscellaneous CLI helpers (Go)
```

---

## 17. Synergy Optimizations & Cascade Effects

Each compression layer amplifies the effectiveness of all subsequent layers.
This is not a coincidence - it is the core architectural insight of Slimference.

### 17.1 Layer 0 - Layer 1 Cascade

**What happens:**
Layer 0 (slimference filter hook) compacts tool output in the CLI before it ever
enters the conversation. By the time Layer 1 sees a message in the proxy, each
tool_result contains a compact representation instead of raw output.

**Impact on Layer 1 sub-layers:**

| Sub-layer | Without L0 | With L0 |
|-----------|------------|---------|
| Dedup (L1.3) | Raw outputs differ by whitespace, timestamps → low hit rate | Compact formats are structurally identical → 3-5x higher dedup rate |
| MinHash (L1.3) | Near-duplicate detection across noisy raw output | Near-duplicate pairs emerge naturally from compact format |
| Delta (L1.5) | Large file reads dominate delta computation | File reads stripped to signatures; deltas are smaller and tighter |
| Prompt cache (L1.6) | Cache prefix shifts on every new tool output | Compact outputs are shorter and stable; prefix extends further |

**Concrete example:**

```
Without L0: go test ./... produces 8000 bytes of verbose output
            → L1 sees 8000 bytes, can compress to ~4000 (50%)
            → Total: 4000 bytes in conversation

With L0:    slimference filter sends "[go test] ok (45 tests)" = 26 bytes
            → L1 sees 26 bytes (already minimal)
            → Total: 26 bytes in conversation (99.7% reduction)
```

### 17.2 Response Cache Key Stability

**Problem without L0:**
Tool outputs contain timestamps, process IDs, and ordering-dependent content
that changes between runs even when the logical result is identical. This
causes cache misses for functionally identical requests.

**With L0:**
`TryCompactGitStatus`, `TryCompactBuildOutput`, etc. produce deterministic
single-line compact representations. The same git status always produces the
same compact string. The same successful build always produces `[tool] ok`.

**Impact on Layer 3 (Response Cache):**
The SHA256 cache key is computed from the effective forwarded request body plus
cache-relevant headers. With L0 pre-filtering, a "build succeeded, same files,
same account/project context" request produces the same key across multiple
invocations, increasing cache hit rate from roughly 5% to 30-40% in typical
coding sessions while still partitioning requests by auth/project/version
context.

### 17.3 MiniMax Input Reduction

**Context:**
Layer 2 (MiniMax) is invoked when the conversation exceeds the sliding window.
It sends old messages to MiniMax for summarization. Each MiniMax API call costs
tokens and latency.

**Without L0:**
Old messages contain raw `cat` file reads (large), raw test output (verbose),
raw git diffs (noisy). MiniMax receives 20K+ tokens for a typical 10-message
batch, costs ~0.5s per summarization.

**With L0:**
Old messages contain compact representations: `[git diff] 3 files changed`,
`[go test] ok`, `[cat] file.go 45 signatures`. MiniMax receives 2-3K tokens
for the same batch, costs ~0.1s per summarization.

**Cascading effect:**
- L0 reduces input to MiniMax by 5-10x
- MiniMax produces better summaries from compact structured input (less noise)
- Smaller summaries fit more context into the sliding window
- Fewer MiniMax calls are needed per session

### 17.4 Prompt Cache Prefix Extension

**How Anthropic prompt caching works:**
Anthropic caches a static prefix of the messages array. If the first N messages
are byte-identical between requests, Anthropic charges 10% of the token cost
for those N messages. The cache prefix breaks as soon as any message changes.

**Without L0+L1:**
Tool results contain timestamps, verbose output. The cache prefix breaks at
the first tool use because tool_result content changes between runs.

**With L0+L1:**
- L0 compacts tool outputs to deterministic strings
- L1 strips ANSI, deduplicates, and compresses
- The combined result: tool_results are short and stable
- The cache prefix extends past multiple tool uses

**Measured effect:**
In sessions longer than 10 messages, the prompt cache prefix typically covers
8-15 messages (vs 1-3 without compression), reducing effective token cost by
60-80% on cached messages.

### 17.5 Summary: Compression Multiplier Stack

```
L0 (CLI hook) → L1 (proxy, deterministic) → L2 (MiniMax) → L3 (cache)
   Baseline         Amplifies L0 savings       Reduces L2 cost    More hits

Typical 20-message session without any layer:  100,000 tokens
After L0 alone:                                 35,000 tokens (65% reduction)
After L0 + L1:                                  18,000 tokens (82% reduction)
After L0 + L1 + L2 (window 5):                  8,000 tokens (92% reduction)
After L0 + L1 + L2 + L3 (cache hit):            1,000 tokens (99% reduction)
```

The layers are not additive - they are multiplicative. Each layer's effectiveness
is bounded by the remaining information entropy, which previous layers have already
reduced. This is why the best savings require all layers running together.

---

## 18. Audit Baseline and Remediation Tracking

The repository completed the parity-hardening phase on 2026-04-17. The
implementation now matches the documented production-readiness target with
proof-bearing tests and release gates.

Tracking artifacts:

- `docs/audit-1.md` - fixed baseline audit for later comparison
- `docs/audit-2.md` - fresh-eyes post-remediation audit
- `docs/gap-analysis.md` - target-vs-reality matrix
- `docs/todo/t11-audit-remediation-program.md` - sequencing and closure rules
- `docs/todo/t12-hook-contract-hardening.md`
- `docs/todo/t13-zero-downside-and-cache-correctness.md`
- `docs/todo/t14-layer2-strictness-and-cancellation.md`
- `docs/todo/t15-daemon-service-productionization.md`
- `docs/todo/t16-proof-gates-and-release-readiness.md`

These files now form the evidence trail for the completed remediation program.
