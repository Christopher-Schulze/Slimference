# Slimference - Architecture Map

## Module

`github.com/slimference/slimference`

## Entry Point

`cmd/slimference/main.go` -> `proxy.New(cfg)` + `tui.NewModel(adapter)` + `tea.NewProgram()`

## Dependency Graph (simplified)

```
types        <- (all packages)
config       <- (all packages except types/util)
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
- `internal/hooks/hooks.go`: Install/Remove/Verify for 10 LLM agent targets
- `internal/hooks/claude.go`: Claude Code hook script generation + settings.json patch
- `internal/hooks/codex.go`: Codex AGENTS.md marker injection
- `internal/hooks/verify.go`: InstalledStatus(home) - check Claude/Codex hook presence

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

- `internal/summarization/layer2.go`: Layer2 coordinator
- `internal/summarization/minimax.go`: MiniMax M2.7 API client
- `internal/summarization/anchor.go`: Anchor point detection (5 types)
- `internal/summarization/validator.go`: Quality validation (5 checks)
- `internal/summarization/cache.go`: SummaryCache with atomic Compressing flag
- `internal/summarization/progressive.go`: Multi-tier compression
- `internal/summarization/adaptive_window.go`: L2.8 complexity-driven window sizing
- `internal/summarization/priority.go`: L2.9 HIGH/MEDIUM/LOW priority classification

### Layer 3 - Response Caching

- `internal/caching/response_cache.go`: LRU response cache with TTL and SHA256 key
- `internal/caching/file_watcher.go`: fsnotify watcher for cache invalidation

### Core Proxy (HTTP Handler)

- `internal/proxy/proxy.go`: Proxy struct, New(), Start(), Shutdown(), toggle atomics
- `internal/proxy/handler.go`: handleCompressibleRequest() hot path, buildLayer1Breakdown()
- `internal/proxy/provider.go`: Provider detection, message extraction/reconstruction
- `internal/proxy/streaming.go`: SSE relay, token counting from stream events

### Analytics and Debug

- `internal/analytics/collector.go`: Analytics struct, Record(), Snapshot()
- `internal/analytics/persistence.go`: JSONL logging to ~/.slimference/analytics/
- `internal/analytics/gain.go`: slimference gain - filter savings by period/command
- `internal/debug/session.go`: SessionFileStats() for JSONL preview
- `internal/debug/decisions.go`: Recorder ring buffer, DecisionEntry, RequestSummary

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
