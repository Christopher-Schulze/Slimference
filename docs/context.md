# TokenProxy - Context & Worklog

## Status: L1.8-L1.13 + L2.8-L2.9 Implemented (v1.2.0)

All tests pass: `go test ./... -count=1` green. Total coverage: 97.4%.

New in v1.2.0:
- L1.8: Tool-Result Classifier (classifyToolResult) - classifies tool output by type
- L1.9: Tool-Output Compressor (compressToolOutput) - RTK-style per-type filtering with aggressive/moderate modes
- L1.11: Image Base64 Replacement (replaceImageBase64) - strips old screenshots/images
- L1.12: Repeated Tool Collapse (ToolCallIndex.CollapseRepeated) - deduplicates identical tool calls
- L1.13: Conversation Graph Pruning (FileOpGraph.PruneRedundant) - removes stale file reads
- L2.8: Adaptive Sliding Window (AdaptiveWindowSize) - complexity-driven window sizing
- L2.9: Tool Result Priority Classification (ClassifyPriority, SummarizationHint)
- types.ToolResultType + types.ToolResultPriority enums added

---

## Current State

### What is done
- All 13 internal packages implemented (types, config, tokens, security, compression,
  summarization, caching, analytics, resilience, sessions, proxy, tui, util)
- cmd/tokenproxy/main.go: entry point, all CLI subcommands, proxy adapter wiring
- go.mod: correct module path and all direct/indirect deps declared (incl. charmbracelet)
- go.sum: NOT yet populated (must run `go mod tidy` before first build)
- 13 test files written covering all core packages (table-driven, t.Parallel)

### What is NOT done yet
- go.sum is empty - run `go mod tidy` to populate it
- No binary built
- No integration tests run against real APIs
- MiniMax API key not configured (Layer 2 will be skipped until set)

---

## Architecture Summary

TokenProxy is a transparent HTTP reverse proxy. Requests from LLM CLIs arrive on
localhost:8990, pass through a 4-layer compression pipeline, and are forwarded to
the real upstream API (Anthropic or OpenAI). Responses stream back unmodified.

### The 4 Layers

| Layer | Name | Latency | Dependency |
|-------|------|---------|------------|
| 1 | Deterministic Compression | <1ms sync | None (pure Go) |
| 2 | MiniMax Summarization | async pre-computed | MINIMAX_API_KEY |
| 3 | Response Caching | <0.1ms | fsnotify |
| 4 | Go Concurrency Pipeline | N/A (goroutine model) | None |

### Request Flow (simplified)
1. CLI sends POST /v1/messages or /v1/chat/completions to localhost:8990
2. proxy.ServeHTTP detects provider (Anthropic or OpenAI) from path and body
3. Layer 1 runs synchronously: JSON compact, comment strip, dedup, tree-sitter, delta
4. Layer 2 check: if pre-compressed summary cached, use it; else skip and enqueue async job
5. Layer 3 check: response cache lookup
6. Reconstruct request with compressed messages, forward to upstream
7. Stream SSE response back to CLI
8. After stream complete: cache response, enqueue Layer 2 pre-compression for next request, update analytics

### Goroutine Model
- Main goroutine: BubbleTea TUI event loop
- Proxy goroutine: net/http.Server.Serve (spawns a goroutine per request)
- compressionWorker goroutine: reads from compressQueue (cap 4), runs Layer 2
- analyticsWorker goroutine: reads from analyticsQueue (cap 256), updates metrics
- cacheJanitor goroutine: periodically evicts expired response cache entries
- analyticsPeriodicFlush goroutine: writes JSONL analytics to disk every 30 min

---

## Key Design Decisions

### No CGO
Tree-sitter was replaced with regex-based code structure extraction.
Reason: CGO creates significant build complexity, cross-compilation issues, and
binary size overhead. The regex approach achieves 80%+ of the token savings with
zero dependency risk.
File: internal/compression/treesitter.go

### Interface-based TUI/proxy decoupling (tui.ProxyInterface)
The tui package defines ProxyInterface, SessionLoggerInterface, and ProxyConfigInterface.
The proxy.Proxy struct implements all three, but the TUI never imports the proxy package.
The cmd/main.go wires them together via a proxyAdapter struct.
Reason: prevents import cycle (proxy imports tui for event delivery; tui would then
import proxy for type access, creating a cycle).
Files: internal/tui/model.go, cmd/tokenproxy/main.go

### sessions.LogEntry used directly in tui.SessionLoggerInterface
tui.SessionLoggerInterface.Recent() returns []sessions.LogEntry directly.
No copy/wrapper type was created.
Reason: the tui package already imports sessions for types; creating a duplicate
type would be redundant and would require conversion on every call.
File: internal/tui/model.go (SessionLoggerInterface)

### AnalyticsSnapshot computed fields
AvgTokensPerRequest and CompressionRatio are computed inside Analytics.Snapshot()
and stored as plain fields on AnalyticsSnapshot.
Reason: the TUI renders these on every tick; computing them in the snapshot (under
lock, once) is cleaner than computing them in the view layer (no lock, potentially
inconsistent intermediate values).
File: internal/analytics/collector.go

### Sliding window = last 5 exchanges always uncompressed
Messages within the last 5 exchanges (configurable: compression.sliding_window)
are never touched by Layer 1 or Layer 2.
Reason: the model's active working context must not be degraded. Only old history
is safe to compress. 5 is the default; increase it for more conservative behavior.
File: internal/config/defaults.go

### Atomic toggle switches
provider and layer enabled/disabled state is stored in [2]atomic.Bool and
[3]atomic.Bool on the Proxy struct. No mutex needed for reads in the hot path.
File: internal/proxy/proxy.go

---

## Open TODOs / Follow-ups

1. Run `go mod tidy` to populate go.sum
   - Required before any `go build` or `go test`
   - Command: `cd /Users/christopher/CODE/TokenProxy && go mod tidy`

2. Test with real Claude Code
   - Set: `ANTHROPIC_BASE_URL=http://127.0.0.1:8990`
   - Run: `tokenproxy` (starts TUI + proxy)
   - Then use Claude Code normally; watch TUI for compression activity

3. MiniMax API key required for Layer 2
   - Without it: Layer 2 is silently skipped (graceful degradation)
   - Set: `export MINIMAX_API_KEY=<your-key>`
   - Verify: `tokenproxy test minimax`

---

## Session Log

2026-04-09 - Initial implementation complete. All packages written from spec.md v1.0.0-final.
             docs/ directory created. Documentation files written.
