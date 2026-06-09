# Slimference - Architecture Map

Last updated: 2026-06-05

## Module

`github.com/Christopher-Schulze/Slimference`

## Active Product Layers

| Layer | Purpose | Main packages |
| --- | --- | --- |
| Layer 0 | Pre-entry and Codex/WSS tool-output reduction | `internal/filter`, `internal/readcache`, `internal/chunkdedup`, `internal/proxy/layer0_proxy.go`, `internal/proxy/wsmitm_phasef.go` |
| Layer 1 | Deterministic compression | `internal/compression`, `internal/contentarchive` |
| Layer 2 | Response cache and provider-cache accounting | `internal/caching`, `internal/analytics`, `internal/proxy` |
| Layer 3 | Output and tool-surface reduction | `internal/outputreduce`, `internal/outstop`, `internal/beterse`, `internal/toolprune` |

The old semantic summary path is retired. There is no
`internal/summarization`, no `internal/contextledger`, no summary CLI, no
side-channel summarization provider path, and no model-facing context summary
or OCRL insertion in the product.

## Dependency Graph

```
types        <- shared request, provider, analytics, and content shapes
config       <- runtime defaults, TOML/env loading, product policy
compression  <- deterministic Layer 1 reducers
contentarchive <- local recoverable archive for deterministic/replayable refs
filter       <- Layer 0 CLI and parser reducers
readcache    <- session-scoped repeat/ranged read decisions
chunkdedup   <- recoverable content-defined chunk references
caching      <- local response cache
outputreduce <- safe output discipline
outstop      <- stream stop/merge helpers
beterse      <- terse-profile helpers
toolprune    <- tool-schema pruning and safety retry support
analytics    <- local JSONL/accounting reports
debug        <- decision and session replay views
planner      <- route/workload/safety policy bridge
wscompact    <- WebSocket frame inspection and shape facts
proxy        <- HTTP/WSS handler, reducers, cache, telemetry, admin state
tui          <- BubbleTea product surface through narrow adapter interfaces
cmd          <- CLI, TUI entry, install/status/codex/debug/report commands
```

## Key Packages

### Layer 0

- `internal/filter/pipeline.go`: command-output filtering pipeline.
- `internal/filter/builtin_*.go`: git, build, test, lint, search, fs, package,
  container, JSON, log, cloud, VCS, DB, language, and formatter compactors.
- `internal/filter/builtin_read.go`: safe first-read handling; repeat/range
  savings belong to readcache.
- `internal/filter/builtin_search.go`: grouped and repo-scoped search keys.
- `internal/readcache/`: archive-backed repeat and ranged-read decisions.
- `internal/chunkdedup/`: bounded recoverable chunk identity store.
- `internal/proxy/layer0_proxy.go`: shared Codex/HTTP/WSS reducer bridge.
- `internal/proxy/wsmitm_phasef.go`: Codex WSS Phase-F adapter.
- `internal/savingspolicy/`: route/workload/proof gates for safe mechanisms.

### Layer 1

- `internal/compression/layer1.go`: deterministic orchestrator.
- `internal/compression/json_compact.go`: JSON minification.
- `internal/compression/comment_strip.go`: comment/whitespace removal.
- `internal/compression/dedup.go` and `dedup_minhash.go`: exact and near-dedup.
- `internal/compression/structure*.go`: structure summaries.
- `internal/compression/delta.go`: revision deltas.
- `internal/compression/prompt_cache.go`: prompt-cache breakpoint hints.
- `internal/compression/tool_compressor.go`: tool-aware compaction.
- `internal/compression/repeated_collapse.go`: repeated call/result collapse.
- `internal/compression/recorder.go`: archive-backed mutation recorder.

### Layer 2

- `internal/caching/response_cache.go`: canonical request-keyed LRU.
- `internal/caching/file_watcher.go`: dependency invalidation.
- `internal/analytics/proxy_gain.go`: local reducer, provider-cache, and
  output-reduce accounting.

### Layer 3

- `internal/outputreduce/`: output discipline injection and auto-downgrade.
- `internal/outstop/`: stop/merge helpers.
- `internal/beterse/`: terse-profile logic.
- `internal/toolprune/`: tool-schema pruning and retry recovery.

### Core Proxy

- `internal/proxy/proxy.go`: lifecycle, routing state, toggles, admin state.
- `internal/proxy/handler.go`: HTTP hot path, zero-downside guards, overflow
  recovery, analytics emission, cache admission.
- `internal/proxy/provider.go`: provider and body-shape detection.
- `internal/proxy/streaming.go`: SSE relay and token accounting.
- `internal/proxy/admin.go`: local admin/status API for TUI and diagnostics.
- `internal/proxy/planner_bridge.go`: planner facts for route/workload decisions.
- `internal/proxy/savings_probe.go`: product-savings state projection.

### Observability and UI

- `internal/debug/decisions.go`: request decision ring and optional JSONL.
- `internal/debug/session.go`: session file stats and replay.
- `internal/analytics/collector.go`: metrics collection.
- `internal/tui/`: BubbleTea product UI, adapters, presenters, keybindings.
- `docs/tui-keybindings.md`: generated keybinding reference.

### CLI

- `cmd/slimference/main.go`: command dispatch and TUI entry.
- `cmd/slimference/codex_cmd.go`: scoped Codex CLI/Desktop paths.
- `cmd/slimference/debug_cmd.go`: debug views.
- `cmd/slimference/savings_cmd.go`: unified savings reporting.
- `cmd/slimference/output_reduce_cmd.go`: output-reduce control.
- `cmd/slimference/tool_prune_cmd.go`: tool-prune control.
- `cmd/slimference/preview_cmd.go`: compression preview.
- `cmd/slimference/config_update_helpers.go`: config mutation helpers.

## State and Storage

- Config: `$XDG_CONFIG_HOME/slimference/config.toml`, falling back to
  `~/.slimference/config.toml`.
- Analytics: `~/.slimference/analytics/YYYY-MM-DD.jsonl`.
- Read cache: `~/.slimference/read-cache/`.
- Content archive: `~/.slimference/content-archive/`.
- Tool archive: `~/.slimference/tool-archive/`.
- Checkpoints: `~/.slimference/checkpoints/`.
- Debug decisions: configured through `SLIMFERENCE_DEBUG_DECISIONS_LOG`.

## Interface Boundaries

- `tui.ProxyInterface` is defined in `internal/tui` and implemented by proxy or
  remote adapters. It exposes provider/layer toggles, analytics, recent
  requests, read-cache/checkpoint/tool-archive status, provider health, config,
  session logger, cache flush, and shutdown.
- `tui.SessionLoggerInterface` is implemented by `internal/sessions`.
- CLI and TUI interact with the daemon through the local admin API when running
  in remote/attach mode.

## Release Evidence

- `go run ./scripts/ci` is the final local truth gate.
- `scripts/benchmarks/benchmark_corpus.go` gates live-corpus evidence.
- `scripts/utils/release-proof-report` summarizes clean proof matrices and
  resource bundles.
- `docs/live-corpus-policy.md` defines the accepted corpus metadata and
  promotion rules.
