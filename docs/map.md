# Slimference - Architecture Map

## Module

`github.com/slimference/slimference`

## Entry Point

`cmd/slimference/main.go` -> `newRemoteProxyAdapter(cfg)` + `tui.NewModel(adapter)` + `tea.NewProgram()`

## Dependency Graph (simplified)

```
types        <- (all packages)
config       <- (all packages except types/util)
buildinfo    <- cmd, proxy, tui
tokens       <- types
security     <- types
compression  <- types, config, tokens
summarization <- types, config, compression (via token counting)
caching      <- types, config
analytics    <- types
resilience   <- (stdlib only)
sessions     <- types
debug        <- (stdlib only)
planner      <- types
wscompact    <- (stdlib only)
contentarchive <- (stdlib only)
filter       <- types, compression (StripANSICodes, StripComments, LanguageFromPath)
hooks        <- (stdlib only)
readcache    <- contentarchive
checkpoints  <- analytics, debug, sessions, types
toolarchive  <- (stdlib only)
proxy        <- types, config, compression, summarization, caching, analytics, security, sessions, resilience, debug, planner, checkpoints, readcache, contentarchive, wscompact
tui          <- types, analytics, sessions (via interface)
cmd          <- proxy, tui, config, analytics, filter, hooks, debug, checkpoints, toolarchive
```

## Key Files

### Layer 0 - Pre-Entry Filter (CLI)

- `internal/filter/pipeline.go`: RunPipeline() - ANSI strip + filter dispatch + truncation
- `internal/filter/engine.go`: RunCommand(), EstimateTokensFromBytes()
- `internal/filter/dispatch.go`: ClassifyCommand()
- `internal/filter/passthrough.go`: TruncateStdoutWithHint()
- `internal/filter/rewrite.go`: Command rewriting + JSON stdin hook extraction
- `internal/filter/permissions.go`: DeniedShellCommand(), AskRequired()
- `internal/filter/tee.go`: WriteTeeRecovery() - raw output preservation
- `internal/filter/tracking.go`: SQLite filter_runs recording + querying
- `internal/filter/filters_toml.go`: TOML DSL 8-stage transform pipeline
- `internal/filter/paths.go`: Path resolution for filter.db, tee dir
- `internal/filter/npx_argv.go`: npx/pnpm exec/yarn argv normalization
- `internal/filter/builtin_git.go`: F01-F05 git filters
- `internal/filter/builtin_build.go`: F07 30+ build tools
- `internal/filter/builtin_testrun.go`: F08 40+ test runners
- `internal/filter/builtin_lint.go`: F09 50+ linters
- `internal/filter/builtin_search.go`: F10 search result compaction
- `internal/filter/builtin_fs.go`: F11 ls/tree listing
- `internal/filter/builtin_pkg.go`: F12 package manager output
- `internal/filter/builtin_container.go`: F13 docker/kubectl/helm
- `internal/filter/builtin_json.go`: F14 JSON stdout minification
- `internal/filter/builtin_log.go`: F15 log deduplication
- `internal/filter/builtin_aws.go`: F16 AWS CLI metadata stripping
- `internal/filter/builtin_gh.go` + `builtin_glab.go`: F18 GitHub/GitLab CLI
- `internal/filter/builtin_psql.go`: F19 psql compact
- `internal/filter/builtin_dotnet.go`: F20 dotnet compact
- `internal/filter/builtin_ruby.go`: F21 Ruby output compact
- `internal/filter/builtin_format.go`: F24 formatter ok-detection
- `internal/filter/builtin_read.go`: F06 file read (cat/head/tail + comment strip)
- `internal/filter/builtin_compact_helpers.go`: shared label/empty-detection helpers
- `internal/filter/project_filters.go`: LoadMergedDenyPatterns() - project + user filter merge
- `internal/filter/posttool_details.go`: PostToolUse payload extraction with optional `tool_name`, `tool_use_id`, and `session_id`
- `internal/hooks/claude.go`: Claude Code PreToolUse structured contract + non-destructive settings.json merge/remove
- `internal/hooks/codex.go`: Codex hooks.json PreToolUse/PostToolUse install, fail-fast preflight for malformed hooks/config conflicts, conflict-safe config.toml patch/remove; never writes global `~/.codex/AGENTS.md`
- `internal/hooks/verify.go`: authoritative Claude/Codex install verification against coherent scripts + config state

### Layer 1 - Deterministic Compression

- `internal/compression/layer1.go`: DeterministicCompressor orchestrator, Compress(), Reset()
- `internal/compression/json_compact.go`: L1.1 JSON minification
- `internal/compression/comment_strip.go`: L1.2 comment/whitespace removal (14 languages)
- `internal/compression/dedup.go`: L1.3 SHA256 exact dedup
- `internal/compression/dedup_minhash.go`: L1.3 MinHash near-duplicate detection
- `internal/compression/structure.go`: L1.4 regex-based code structure extraction
- `internal/compression/delta.go`: L1.5 file revision delta encoding
- `internal/compression/prompt_cache.go`: L1.6 Anthropic cache_control breakpoints with stable-prefix token gate and high-value tool-result scoring
- `internal/compression/ansi_strip.go`: L1.7 ANSI/progress bar removal
- `internal/compression/tool_classifier.go`: L1.8 tool result type classification
- `internal/compression/tool_compressor.go`: L1.9 per-type RTK-style compression
- `internal/compression/stacktrace_compact.go`: T143d semantic test-failure / stacktrace compaction behind L1.9
- `internal/compression/success_shortcircuit.go`: L1.10 success pattern detection
- `internal/compression/image_replace.go`: L1.11 base64 image replacement
- `internal/compression/repeated_collapse.go`: L1.12 identical tool call deduplication
- `internal/compression/graph_pruning.go`: L1.13 file operation graph pruning
- `internal/compression/prefilter_tag.go`: L1.14 Layer 0 marker detection
- `internal/compression/lang.go`: Language detection from file extension

### Layer 2 - OCRL Context Ledger and Legacy Background Summarization

- `docs/ocrl.md`: OCRL product spec for deterministic old-context replacement, modes, route gates, archive recovery, and zero-drawdown promotion rules
- `internal/contextledger/ledger.go`: deterministic command/file/search/failure/decision/recovery capsule builders
- `internal/contextledger/selection.go`: fail-closed capsule selection, active-path/quality-pressure gates, archive expansion and archive recoverability verification
- `internal/contextledger/ocrl.go`: pure OCRL route/recovery/token gate engine plus deterministic capsule renderer
- `internal/proxy/ocrl_shadow.go`: Codex WSS OCRL shadow summary wiring with archive-token would-save telemetry and no model-facing mutation
- `internal/summarization/layer2.go`: Layer2 coordinator, strict summary formatting, ROI candidate scoring, prefix-hash apply validation, context-aware compression jobs, timeout-wrapped parent contexts, no post-cancel cache writes
- `internal/summarization/minimax.go`: MiniMax M2.7 API client with request-bound HTTP contexts and cancelable retry backoff
- `internal/summarization/prompt_contract.go`: T144a task-shaped summary contract selector for coding/debug/review/planning/docs/live-E2E prompts
- `internal/summarization/anchor.go`: Anchor point detection (5 types)
- `internal/summarization/capsules.go`: archive-backed micro/phase/session context capsule schema, builders, and tier selectors
- `internal/summarization/validator.go`: strict quality validation over structured content blocks
- `internal/summarization/cache.go`: session-keyed SummaryCache with atomic Compressing flag, candidate hashes, hash-mismatch and stale-job telemetry
- `internal/summarization/progressive.go`: Multi-tier compression
- `internal/summarization/adaptive_window.go`: L2.8 complexity-driven window sizing
- `internal/summarization/priority.go`: L2.9 HIGH/MEDIUM/LOW priority classification

### Layer 3 - Response Caching

- `internal/caching/response_cache.go`: LRU response cache with canonical forwarded-request SHA256 keys, normalized cache-relevant headers, stochastic-request bypass, and dependency-path invalidation
- `internal/caching/file_watcher.go`: fsnotify watcher for dependency-path cache invalidation, armed-watch verification, idempotent close

### Core Proxy (HTTP Handler)

- `internal/proxy/proxy.go`: Proxy struct, New(), Start(), Shutdown(), worker-owned cancellation context, toggle atomics, listener readiness state, 32 MiB request-body hard fail
- `internal/proxy/handler.go`: handleCompressibleRequest() hot path, session-aware Layer 0 proxy pass, zero-downside guard, context-aware overflow retry fallback, dependency-safe Layer 3 admission, analytics shutdown drain, shutdown-aware compression worker
- `internal/proxy/provider.go`: Provider detection, message extraction/reconstruction, safe OpenAI structured-content roundtrip without stringifying multimodal arrays, Codex `/v1/responses` and `/backend-api/codex/*` request-shape normalization
- `internal/proxy/streaming.go`: SSE relay, token counting from stream events, 8 MiB per-line SSE cap, bounded non-streaming passthrough with safe local-502 behavior
- `internal/proxy/admin.go`: daemon-admin HTTP surface for TUI attach mode, live status snapshot, provider/layer toggles, cache flush endpoint, read-cache, checkpoint, tool-archive, and Layer 2 status export
- `internal/proxy/checkpoints.go`: async checkpoint capture bridge from analytics events into `internal/checkpoints`
- `internal/proxy/planner_bridge.go`: proxy-to-planner fact bridge, recent-edit hook-state lookup, live-corpus confidence derivation, WebSocket shape fact lookup
- `internal/planner/planner.go`: deterministic cross-layer compression plan, safety gates, and layer decisions
- `internal/wscompact/`: inspect-only RFC 6455 frame summarizer, non-mutating JSON shadow estimator, and shape registry for WebSocket planner facts
- `internal/checkpoints/checkpoints.go`: deterministic checkpoint store, trigger policy, ranked restore, persisted stats
- `internal/toolarchive/toolarchive.go`: local archive store, `local-archive://*` references, bounded retrieval, persisted stats

### Analytics and Debug

- `internal/analytics/collector.go`: Analytics struct, Record(), Snapshot()
- `internal/analytics/prompt_cache.go`: persisted prompt-cache report reader and CSV/JSON export helpers for `stats prompt-cache`
- `internal/analytics/persistence.go`: JSONL logging to ~/.slimference/analytics/
- `internal/analytics/gain.go`: slimference gain - filter savings by period/command
- `internal/analytics/output_reduce.go`: `slimference gain --output` observed-output report with provider/model/profile/task-shape rows and no fake baseline savings
- `internal/analytics/proxy_gain.go`: `slimference gain --proxy` decision-log flight accounting, provider-cache credits, tool-prune totals, output-reduce overhead, and prompt-cache heat rows for real proxied LLM requests
- `internal/outputreduce/`: output-discipline injection, task-shape detection,
  repair-followup detection, and provider/model/task-shape auto-downgrade
- `internal/proxy/output_reduce_repair.go`: per-session one-shot repair signal
  bridge from follow-up request text to the previous output-reduce bucket
- `scripts/benchmarks/benchmark_corpus.go`: live-corpus category gate,
  planner replay, layer-combination matrix, and failable scenario validators
- `internal/debug/session.go`: SessionFileStats() for JSONL preview, ReplaySession() with non-summary skip
- `internal/debug/decisions.go`: Recorder ring buffer, DecisionEntry, RequestSummary, guarded JSONL flush on marshal/write failure
- `internal/tui/model.go`: BubbleTea model, arrow-first operator-console navigation, selectable dashboard/debug/setup actions, bounded shutdown, private debug-log export (`~/.slimference/exports`, 0700/0600)
- `internal/buildinfo/version.go`: single source of truth for CLI/TUI/health version strings

### TUI (BubbleTea + Lipgloss)

- `internal/tui/model.go`: Model, Update(), Init(), ProxyInterface, SessionLoggerInterface, HookStatus, SetHookStatus()
- `internal/tui/views.go`: renderMainView(), renderStatsView(), renderDebugView(), renderSetupView(), renderHookStatus(), operator-console dashboard layout
- `internal/tui/styles.go`: Lipgloss operator-console palette (dark surfaces, cyan focus, green savings)
- `internal/tui/components.go`: Progress bar, badges, table renderer, log line renderer, selectable menu rows, compact KPI helpers
- `internal/tui/keys.go`: KeyMap, DefaultKeyMap(), footerHelp() with arrow-key-first navigation hints

### Supporting Packages

- `internal/types/types.go`: Message, ContentBlock, RingBuffer, AnalyticsEvent,
  ToolResultType (11 values), ToolResultPriority (3 values), etc.
- `internal/config/config.go` + `defaults.go`: TOML config with env overrides
- `internal/tokens/counter.go` + `usage.go`: Token counting + UsageTracker
- `internal/security/patterns.go` + `secrets.go`: Secret detection and redaction
- `internal/resilience/retry.go` + `health.go` + `latency.go`: HTTP resilience
- `internal/sessions/logger.go` + `export.go`: Session log ring buffer
- `internal/readcache/*.go`: session-scoped read-cache state, proxy-visible read deltas, contentarchive-backed reread references, decision accounting, persisted stats under `~/.slimference/read-cache/`
- `internal/contentarchive/`: local reversible content archive used by Layer 1 lossy transforms, capsules, proxy-visible read deltas, and read-only `Peek` verification for shadow/proof paths
- `internal/checkpoints/checkpoints.go`: deterministic checkpoint state and ranked restore under `~/.slimference/checkpoints/`
- `internal/toolarchive/toolarchive.go`: bounded large-result archive under `~/.slimference/tool-archive/`
- `internal/util/safego.go`: Safe goroutine launch with panic recovery

## Interface Boundaries

### tui.ProxyInterface (defined in tui, implemented in proxy, wired in cmd)

```
SetProviderEnabled(types.Provider, bool)
SetLayerEnabled(int, bool)
IsProviderEnabled(types.Provider) bool
IsLayerEnabled(int) bool
FlushCaches()
GetAnalytics() analytics.AnalyticsSnapshot
GetRecentRequests(int) []types.RequestMetrics
GetLayer2Status() tui.Layer2Status
GetReadCacheStatus() tui.ReadCacheStatus
GetCheckpointStatus() tui.CheckpointStatus
GetToolArchiveStatus() tui.ToolArchiveStatus
GetProviderHealth(types.Provider) types.ProviderHealthInfo
SessionLogger() tui.SessionLoggerInterface
Shutdown(context.Context) error
Config() tui.ProxyConfigInterface
```

### tui.SessionLoggerInterface (defined in tui, implemented by sessions.SessionLogger)

```
Recent(int) []sessions.LogEntry
Format(sessions.LogEntry) string
```

### tui.ProxyConfigInterface (defined in tui, implemented by configAdapter in cmd)

```
GetListenPort() int
GetPrefillSpeed() int
```

## Channel Architecture

| Channel | Element type | Buffer | Writer | Reader |
|---------|-------------|--------|--------|--------|
| compressQueue | types.CompressJob with session/input hash | 4 | proxy handler | compressionWorker |
| analyticsQueue | types.AnalyticsEvent | 256 | proxy handler | analyticsWorker |
| shutdownCh | struct{} | 0 | Proxy.Shutdown | all workers |
| tuiSendFn (func) | types.RequestMetrics | N/A | proxy handler | tea.Program.Send |

## Atomic State

| Field | Type | Index mapping |
|-------|------|--------------|
| proxy.providerEnabled | [2]atomic.Bool | 0=Anthropic, 1=OpenAI |
| proxy.layerEnabled | [3]atomic.Bool | 0=Layer1, 1=Layer2, 2=Layer3 |
| summarization.SummaryCache.Compressing | atomic.Bool | N/A |

## Config File Location

`~/.slimference/config.toml` (default)
Override: `SLIMFERENCE_CONFIG` env var

## Analytics Log Location

`~/.slimference/analytics/` (default)
JSONL files, one per day: `YYYY-MM-DD.jsonl`

## Audit and Planning Artifacts

- `docs/audit-1.md`: fixed production-readiness baseline for comparison against later audits
- `docs/audit-2.md`: fresh-eyes follow-up audit after remediation closure
- `docs/gap-analysis.md`: target-vs-reality matrix and closure conditions
- `docs/todo/t11-audit-remediation-program.md`: program driver and sequencing
- `docs/todo/t12-hook-contract-hardening.md`: Claude Code and Codex hook remediation plan
- `docs/todo/t13-zero-downside-and-cache-correctness.md`: hot-path and Layer 3 correctness plan
- `docs/todo/t14-layer2-strictness-and-cancellation.md`: MiniMax policy, validation, cancellation plan
- `docs/todo/t15-daemon-service-productionization.md`: daemon/launchd hardening plan
- `docs/todo/t16-proof-gates-and-release-readiness.md`: CI, coverage, and release-proof plan
- `cmd/slimference/remote_proxy.go`: TUI attach adapter backed by the daemon admin API and file-backed session-log view
- `cmd/slimference/checkpoint_cmd.go`: `checkpoint` and `expand` CLI commands
- `cmd/slimference/prompt_cache_stats.go`: `stats prompt-cache` CLI
- `scripts/utils/main.go`: offline session/decision/filter/combined reporting with text/JSON/CSV output

## Post-2.0 Additions (T41-T64)

### New or significantly extended packages

| Area                                 | Path / symbol                                         | Task |
|--------------------------------------|-------------------------------------------------------|------|
| Top-level help + version dispatch    | `cmd/slimference/help.go`                             | T43  |
| Headless foreground runner           | `cmd/slimference/headless.go`                         | T44  |
| `--config` flag parser               | `cmd/slimference/main.go::extractConfigFlag`          | T46  |
| LoadWithOptions / LoadInfo           | `internal/config/config.go::LoadWithOptions`          | T46  |
| XDG config path resolver             | `internal/config/config.go::XDGConfigPath`            | T46  |
| Analytics-queue counters             | `internal/proxy/proxy.go::trySendAnalytics`           | T42  |
| Shutdown-timeout guard + pprof dump  | `internal/proxy/handler.go::Shutdown`                 | T60  |
| Pipeline phase histograms            | `internal/analytics/phase_hist.go`                    | T58  |
| Anthropic-version negotiation        | `internal/proxy/version_negotiation.go`               | T62  |
| Multi-breakpoint prompt cache        | `internal/compression/prompt_cache.go`                | T45  |
| Dedup-similarity staircase           | `internal/compression/layer1.go::resolveDedupThreshold` | T53 |
| Tool-compressor tuning knobs         | `internal/compression/tool_compressor.go::SetToolCompressorTuning` | T61 |
| L2 latency estimator + decision rule | `internal/summarization/latency_estimator.go`         | T54  |
| Layer-0 exit-code matrix doc         | `docs/layer0-exit-codes.md`                           | T63  |
| Release pipeline                     | `scripts/release/main.go`                             | T47  |
| Linux systemd service                | `scripts/service/linux/slimference.service`           | T48  |
| Distroless Dockerfile                | `scripts/service/docker/Dockerfile`                   | T48  |
| TUI keybindings generator            | `internal/tui/keys.go::RenderKeybindingsMarkdown`     | T64  |
| Codex corpus metadata + smoke gate   | `scripts/benchmarks/corpus_metadata.go`               | T75  |
| Codex smoke gate fixture + schema    | `tests/fixtures/codex/codex-metadata.json`            | T75  |
| Codex smoke gate ci step             | `scripts/ci/main.go::defaultSteps[4]`                 | T75  |

## Strategic Improvement Program Additions (T76-T108)

### New or extended packages

| Area                                  | Path / symbol                                          | Task |
|---------------------------------------|--------------------------------------------------------|------|
| Reversibility content archive         | `internal/contentarchive/`                             | T76  |
| MutationRecorder + DiskRecorder       | `internal/compression/recorder.go`                     | T76  |
| Opportunistic re-injection            | `internal/proxy/reinject.go`                           | T76 WP3 |
| Quality calibration signals           | `internal/quality/`                                    | T77  |
| Provider response-state store         | `internal/sessions/response_state.go`                  | T78  |
| `slimference watch` live ticker       | `cmd/slimference/watch_cmd.go`                         | T79  |
| Unified `slimference savings`         | `cmd/slimference/savings_cmd.go` + `internal/analytics/proxy_gain.go` | T80/T140 |
| Duration + next-request bypass        | `internal/proxy/proxy.go::SetBypassFor*`               | T81  |
| `slimference compress-preview`        | `cmd/slimference/preview_cmd.go` + `internal/proxy/preview.go` | T82  |
| Provider degradation composite flag   | `internal/proxy/health_monitor.go::anyDegraded`        | T83  |
| Drain-timeout knob                    | `internal/proxy/handler.go::applyDrainTimeout`         | T85  |
| Configurable system prompt            | `internal/summarization/minimax.go::LoadPromptOverrideFromPath` | T86 |
| Multi-stack few-shot picker           | `internal/summarization/minimax.go::pickExampleLang`   | T87  |
| Provider capability registry          | `internal/types/provider_caps.go`                      | T88  |
| 12-family CoT stripper                | `internal/summarization/minimax.go::StripCoTTags`      | T89  |
| Deterministic summary repair          | `internal/summarization/repair.go`                     | T90  |
| Lineage markers + telemetry           | `internal/summarization/minimax.go::RecordLineageStats`| T92  |
| Posttool repetition store             | `internal/repetition/`                                 | T93  |
| Streaming Layer-0 filter pump         | `internal/filter/stream.go`                            | T94  |
| Per-session ContentIndex namespace    | `internal/compression/dedup.go::CheckAndRecordForSession` | T96/T107 |
| Comment-strip whitelist               | `internal/compression/comment_strip.go::isWhitelistedComment` | T98 |
| L1/L2 cross-direction coordinator     | `internal/compression/layer1.go::SetCoordinatorSubsume`| T100 |
| Cache age histogram                   | `internal/caching/response_cache.go::AgeSnapshot`      | T102 |
| Tool-definition usage tracker + safety retry | `internal/toolprune/`, `internal/proxy/tool_prune_retry.go` | T103/T151 |

### Admin surface additions (`/admin/status`)

| JSON key                  | Shape                                          | Task |
|---------------------------|------------------------------------------------|------|
| `analytics_queue`         | capacity / depth / totals                      | T42  |
| `prompt_cache`            | breakpoints injected total                     | T45  |
| `pipeline`                | phase snapshots (p50/p95/max)                  | T58  |
| `anthropic_version`       | supported / behavior / unknown                 | T62  |
| `content_archive`         | entries / bytes / re_inject_count / evictions  | T76  |
| `quality`                 | reread / cache_miss_spike / net_savings        | T77  |
| `cache_age`               | count / p50 / p95 / p99 / max ms               | T102 |
| `layer2.cache_stats`      | sessions / hash_mismatches / candidate_sets / stale_job_skips | T152 |
| `tool_prune`              | sessions / pruned / reattach / miss / retry / disabled | T103/T151 |
| `summarization`           | prompt version / examples / CoT / lineage / repair | T86/T87/T89/T90/T92 |
| `coordinator`             | enabled flag + skipped_total                   | T100 |
| `any_provider_degraded`   | composite degradation flag                     | T83  |
