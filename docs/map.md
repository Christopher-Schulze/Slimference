# Slimference - Architecture Map

## Module

`github.com/slimference/slimference`

## Entry Point

`cmd/slimference/main.go` -> `proxy.New(cfg)` + `tui.NewModel(adapter)` + `tea.NewProgram()`

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
filter       <- types, compression (StripANSICodes, StripComments, LanguageFromPath)
hooks        <- (stdlib only)
proxy        <- types, config, compression, summarization, caching, analytics, security, sessions, resilience, debug
tui          <- types, analytics, sessions (via interface)
cmd          <- proxy, tui, config, analytics, filter, hooks, debug
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
- `internal/hooks/claude.go`: Claude Code PreToolUse structured contract + non-destructive settings.json merge/remove
- `internal/hooks/codex.go`: Codex hooks.json PreToolUse/PostToolUse install, conflict-safe config.toml patch/remove, legacy AGENTS.md fallback
- `internal/hooks/verify.go`: authoritative Claude/Codex install verification against coherent scripts + config state

### Layer 1 - Deterministic Compression

- `internal/compression/layer1.go`: DeterministicCompressor orchestrator, Compress(), Reset()
- `internal/compression/json_compact.go`: L1.1 JSON minification
- `internal/compression/comment_strip.go`: L1.2 comment/whitespace removal (14 languages)
- `internal/compression/dedup.go`: L1.3 SHA256 exact dedup
- `internal/compression/dedup_minhash.go`: L1.3 MinHash near-duplicate detection
- `internal/compression/structure.go`: L1.4 regex-based code structure extraction
- `internal/compression/delta.go`: L1.5 file revision delta encoding
- `internal/compression/prompt_cache.go`: L1.6 Anthropic cache_control breakpoints
- `internal/compression/ansi_strip.go`: L1.7 ANSI/progress bar removal
- `internal/compression/tool_classifier.go`: L1.8 tool result type classification
- `internal/compression/tool_compressor.go`: L1.9 per-type RTK-style compression
- `internal/compression/success_shortcircuit.go`: L1.10 success pattern detection
- `internal/compression/image_replace.go`: L1.11 base64 image replacement
- `internal/compression/repeated_collapse.go`: L1.12 identical tool call deduplication
- `internal/compression/graph_pruning.go`: L1.13 file operation graph pruning
- `internal/compression/prefilter_tag.go`: L1.14 Layer 0 marker detection
- `internal/compression/lang.go`: Language detection from file extension

### Layer 2 - MiniMax Summarization

- `internal/summarization/layer2.go`: Layer2 coordinator, strict summary formatting, context-aware compression jobs
- `internal/summarization/minimax.go`: MiniMax M2.7 API client
- `internal/summarization/anchor.go`: Anchor point detection (5 types)
- `internal/summarization/validator.go`: strict quality validation over structured content blocks
- `internal/summarization/cache.go`: SummaryCache with atomic Compressing flag
- `internal/summarization/progressive.go`: Multi-tier compression
- `internal/summarization/adaptive_window.go`: L2.8 complexity-driven window sizing
- `internal/summarization/priority.go`: L2.9 HIGH/MEDIUM/LOW priority classification

### Layer 3 - Response Caching

- `internal/caching/response_cache.go`: LRU response cache with canonical forwarded-request SHA256 keys, normalized cache-relevant headers, stochastic-request bypass, and dependency-path invalidation
- `internal/caching/file_watcher.go`: fsnotify watcher for dependency-path cache invalidation, armed-watch verification, idempotent close

### Core Proxy (HTTP Handler)

- `internal/proxy/proxy.go`: Proxy struct, New(), Start(), Shutdown(), toggle atomics, listener readiness state, 32 MiB request-body hard fail
- `internal/proxy/handler.go`: handleCompressibleRequest() hot path, zero-downside guard, dependency-safe Layer 3 admission, analytics shutdown drain
- `internal/proxy/provider.go`: Provider detection, message extraction/reconstruction
- `internal/proxy/streaming.go`: SSE relay, token counting from stream events, bounded non-streaming passthrough with safe local-502 behavior

### Analytics and Debug

- `internal/analytics/collector.go`: Analytics struct, Record(), Snapshot()
- `internal/analytics/persistence.go`: JSONL logging to ~/.slimference/analytics/
- `internal/analytics/gain.go`: slimference gain - filter savings by period/command
- `internal/debug/session.go`: SessionFileStats() for JSONL preview, ReplaySession() with non-summary skip
- `internal/debug/decisions.go`: Recorder ring buffer, DecisionEntry, RequestSummary, guarded JSONL flush on marshal/write failure
- `internal/buildinfo/version.go`: single source of truth for CLI/TUI/health version strings

### TUI (BubbleTea + Lipgloss)

- `internal/tui/model.go`: Model, Update(), Init(), ProxyInterface, SessionLoggerInterface, HookStatus, SetHookStatus()
- `internal/tui/views.go`: renderMainView(), renderStatsView(), renderDebugView(), renderHookStatus()
- `internal/tui/styles.go`: Lipgloss color palette
- `internal/tui/components.go`: Progress bar, badges, table renderer, log line renderer
- `internal/tui/keys.go`: KeyMap, DefaultKeyMap(), footerHelp()

### Supporting Packages

- `internal/types/types.go`: Message, ContentBlock, RingBuffer, AnalyticsEvent,
  ToolResultType (11 values), ToolResultPriority (3 values), etc.
- `internal/config/config.go` + `defaults.go`: TOML config with env overrides
- `internal/tokens/counter.go` + `usage.go`: Token counting + UsageTracker
- `internal/security/patterns.go` + `secrets.go`: Secret detection and redaction
- `internal/resilience/retry.go` + `health.go` + `latency.go`: HTTP resilience
- `internal/sessions/logger.go` + `export.go`: Session log ring buffer
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
| compressQueue | types.CompressJob | 4 | proxy handler | compressionWorker |
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
- `scripts/utils/main.go`: offline session/decision/filter/combined reporting with text/JSON/CSV output
