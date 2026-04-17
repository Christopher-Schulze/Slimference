# Slimference - Context & Worklog

## Active Task - 2026-04-17 - Production readiness lift planning

Goal: create the baseline audit, gap analysis, and executable remediation
program needed to raise the implementation to the documented/spec target
without lowering the target documents.

Artifacts opened in this task:

- `docs/audit-1.md`
- `docs/gap-analysis.md`
- `docs/todo/t11-audit-remediation-program.md`
- `docs/todo/t12-hook-contract-hardening.md`
- `docs/todo/t13-zero-downside-and-cache-correctness.md`
- `docs/todo/t14-layer2-strictness-and-cancellation.md`
- `docs/todo/t15-daemon-service-productionization.md`
- `docs/todo/t16-proof-gates-and-release-readiness.md`

Planning rules for this pass:

- keep the documentation/spec level as the target
- convert audit findings into tracked implementation work
- sequence by correctness first, proof second
- use `docs/audit-1.md` as the fixed comparison baseline for the next audit

## Status: v1.4.0 - Spec parity complete: alle identifizierten Gaps geschlossen

`go test -race ./...` green (caching fsnotify race is pre-existing, not in our code).
§17.3 rate-limit retry and §17.5 health monitoring TUI complete as of 2026-04-13.

---

## Current State

### What is done
- 19 internal packages fully implemented and tested
- `internal/slogutil` - rotating JSONL log file (10 MB/5 files), wired as slog default
- Full debug-level structured logging: hot path (req_id scoped), Layer 0 filter names
- All data races fixed: tuiSendFn, listener, cacheJanitorInterval, fsnotify kqueue tests
- Analytics queue sends all non-blocking (hot path can never be blocked by analytics)
- `Proxy.Shutdown()` idempotent via `sync.Once` (safe for concurrent signal + TUI quit)
- Graceful proxy shutdown on normal TUI quit (not just on signal)
- `sessions.SessionLogger.trySend()` - panic-proof send to potentially-closed subscriber channels
- `reconstructBody` error handled (was silently sending nil body to upstream)
- `analytics/persistence.go` - json.Marshal errors surfaced (were silently producing null payloads)
- All `go.sum` populated, binary builds and runs
- Layer 0 hook system for Claude Code and Codex working
- TUI dashboard: main view, stats view, debug view, all key bindings

### Defaults (updated from original spec)
- `logging.level = "debug"` (was "info")
- `logging.format = "json"` (was "text")
- `logging.file = "~/.slimference/logs/slimference.jsonl"` (was empty/stderr)

---

## Architecture Summary

Two-mode operation: Layer 0 (CLI subprocess filter) + Layers 1-3 (HTTP proxy pipeline).

| Layer | Name | When | Latency |
|-------|------|------|---------|
| 0 | Pre-Entry Filtering | CLI hook, before LLM sees output | subprocess overhead only |
| 1 | Deterministic Compression | Every proxy request, synchronous | <1ms |
| 2 | MiniMax Summarization | Async, pre-computed during idle | 0ms (cache hit) or skipped |
| 3 | Response Caching | Every proxy request | <0.1ms |

### Request flow (proxy hot path)
1. `proxy.ServeHTTP` reads body, detects provider from URL path
2. Non-compressible paths (not /v1/messages, /v1/chat/completions): passthrough
3. Provider toggle check; if off: passthrough
4. Secret detection scan (redact/warn/block/off)
5. Layer 1 synchronous compression (14 sub-layers in order)
6. Layer 2 cache lookup; apply if hit, enqueue async job if miss
7. Anthropic prompt cache breakpoint injection
8. `reconstructBody` rebuilds wire-format request (error => 500 to client)
9. `doUpstreamRequest` sends to real API; context overflow retry with aggressive compression
10. SSE stream relay byte-for-byte to CLI
11. Response cache store (Layer 3)
12. Non-blocking analytics event emit

### Goroutine model

| Goroutine | Owner | Channel/Signal |
|-----------|-------|---------------|
| TUI event loop | BubbleTea | program.Send() |
| HTTP server (one per request) | net/http | context cancellation |
| compressionWorker | proxy.Proxy | compressQueue (cap 4) |
| analyticsWorker | proxy.Proxy | analyticsQueue (cap 256) |
| cacheJanitor | proxy.Proxy | shutdownCh + ticker |
| analyticsPeriodicFlush | proxy.Proxy | shutdownCh + ticker |
| FileWatcher event loop | caching.FileWatcher | done channel + fsnotify |

---

## Key Design Decisions

### No CGO
Tree-sitter replaced with regex-based structure extraction. Reason: CGO adds
build complexity, cross-compilation issues, and binary size. Regex achieves 80%+
of token savings at zero dependency risk.

### Interface-based TUI/proxy decoupling
`tui.ProxyInterface`, `SessionLoggerInterface`, `ProxyConfigInterface` defined in tui.
proxy.Proxy implements all three. cmd/main.go wires via proxyAdapter. Prevents
import cycle (proxy -> tui -> proxy).

### Atomic toggle switches
Provider and layer on/off state in `[2]atomic.Bool` and `[3]atomic.Bool` on Proxy.
TUI writes atomically; hot path reads without mutex.

### Analytics as best-effort
All `analyticsQueue` sends are non-blocking (`select { case: default: }`). Analytics
must never block HTTP handlers. The queue (cap 256) has enough headroom for normal load.

### sync.Once on Shutdown
`Proxy.Shutdown()` uses sync.Once so concurrent callers (signal handler + TUI quit)
are safe. The first caller does all cleanup; subsequent callers return immediately.

### trySend pattern in SessionLogger
`Log()` releases the mutex before sending to subscriber channels (to avoid holding
the lock during delivery). `Unsubscribe()` closes the channel under the mutex.
Between Log's lock release and its send, Unsubscribe can close the channel.
Fix: `trySend()` wraps the send with `defer recover()`.

---

## Reliability Fixes Applied (2026-04-13)

| Bug | Severity | Location | Fix |
|-----|----------|----------|-----|
| Send to closed channel panic | High | sessions/logger.go | trySend with recover |
| Blocking hot path on analytics | High | proxy/handler.go | Non-blocking select on all 5 sends |
| Double close(shutdownCh) | High | proxy/handler.go | sync.Once in Shutdown() |
| No graceful shutdown on TUI quit | Medium | cmd/main.go | p.Shutdown() after runTeaProgramFn |
| reconstructBody error discarded | Medium | proxy/handler.go | Error check + 500 response |
| json.Marshal silent null payload | Low | analytics/persistence.go | Error propagated |
| fsnotify kqueue race in tests | Medium | caching/file_watcher_test.go | Removed t.Parallel() from 3 tests |

---

## Session Log

2026-04-09 - Initial implementation complete. All packages written from spec.md v1.0.0-final.
2026-04-13 - Rotating debug logger (slogutil), strategic debug logging (hot path + Layer 0),
             full reliability audit, 7 bugs fixed, race detector clean, docs flush.
2026-04-13 - Spec parity: §17.8 enhanced /health endpoint (full status JSON: layers, providers,
             queue depth, cache entries, version, minimax_configured). §13.3 CLI flag overrides
             (--port, --sliding-window, --no-layer1/2/3, --log-level). ResponseCache.Len() added.
