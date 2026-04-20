# T42 - Analytics-Queue Overflow Visibility

Status: todo
Priority: P0
Scope: `internal/proxy/proxy.go`, `internal/analytics/collector.go`, `internal/tui/`, `internal/admin/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`internal/proxy/proxy.go` sends analytics events through a buffered channel
(`analyticsQueue`, buffer=256) using non-blocking semantics:

```go
select {
case p.analyticsQueue <- ev:
default: // DROPPED silently
}
```

If the queue fills (e.g. high-throughput burst, slow collector, or collector
goroutine stuck in fsync), events are dropped **without any log, counter, or
user-visible signal**. Consequences:

1. `docs/benchmarks.md` numbers become unreliable under load.
2. Savings-percentage in the TUI undercounts, leading to "why does
   Slimference show less savings than I expected?" support tickets.
3. Regression detection in CI becomes noisy because the baseline depends on
   whether the machine happened to drop events.

This violates `~/.claude/CLAUDE.md` "no silent failures" and
`AGENTS.md` "no schummeln with measurements".

## Current State

- Buffer=256, non-blocking send, no counter.
- Collector persists to JSONL with fsync every 64 events or every 500 ms.
- No visibility in TUI or `/admin/status`.

## Target State

- Every dropped event increments `slim_analytics_dropped_total`.
- First drop per minute emits `slog.Warn` with fields: `queue_depth=256`,
  `current_depth`, `reason=queue_full`. Subsequent drops within 60 s are
  counted but not re-logged (rate-limited).
- TUI Stats view shows `Analytics Drops: <n>` (red if non-zero) and
  `Queue: <depth>/256`.
- `/admin/status` JSON gains `analytics_queue: { capacity, depth, dropped }`.
- On graceful shutdown, collector flushes remaining queue and logs
  `event=analytics_flush_complete queue_final_depth=<n>`.

## Design

### Counter type

`atomic.Int64` attached to `ProxyServer`:
- `analyticsDropped`
- `analyticsEnqueued`
- `analyticsProcessed`

### Queue depth sampling

Collector goroutine writes current depth to another `atomic.Int64` on every
tick (cheap, `len(chan)` is O(1)).

### Rate-limited logging

Re-use `internal/slogutil.NewRateLimitedLogger(1*time.Minute)`; factor out
from existing usage in `hooks` package if not already public.

### TUI surface

`internal/tui/views.go` `renderStats`:

```
Analytics:
  Enqueued:  12,458
  Processed: 12,450
  Dropped:      8   (!!)   <- red when > 0
  Queue:      12/256
```

### Admin surface

`/admin/status` additional JSON:

```json
"analytics_queue": {
  "capacity": 256,
  "depth": 12,
  "enqueued_total": 12458,
  "processed_total": 12450,
  "dropped_total": 8
}
```

## Implementation Plan

### WP1 - Counters
- Add atomic counters to `ProxyServer`.
- Increment on send / drop / dequeue.

### WP2 - Rate-limited warning
- Integrate `slogutil.NewRateLimitedLogger` for `analytics_drop_warn`.

### WP3 - TUI + Admin
- Extend analytics snapshot with queue stats.
- Render in TUI Stats view.
- Serialize in `/admin/status`.

### WP4 - Graceful shutdown flush
- In `Shutdown`, drain `analyticsQueue` with timeout (5 s), log final counts.

### WP5 - Consider capacity auto-tune
- Out of scope for this TASK, but record finding in `docs/tuning-inventory.md`:
  if `dropped_total > 0` persists in prod, raise buffer to 1024.

---

## Subtasks

- [ ] Add three atomic counters + queue-depth sampler.
- [ ] Rate-limited slog.Warn on first drop per 60 s.
- [ ] Extend analytics snapshot struct.
- [ ] TUI Stats rendering with colour flag.
- [ ] `/admin/status` JSON extension.
- [ ] Graceful shutdown flush with timeout.
- [ ] Unit test: simulate drops (fill buffer, block collector), assert counter.
- [ ] Integration test: burst 500 events, verify no silent loss beyond counter.
- [ ] Update `docs/documentation.md` §7 Analytics.

## Risks

- Slog rate-limiter misbehaves under clock skew: unit-test with injected
  `clock.Clock` (already used elsewhere in `internal/util`).
- TUI redraw cost if counters change every tick: throttle render to 500 ms.

## Acceptance Criteria

- [ ] `go test -race ./...` green.
- [ ] Crafted burst test produces non-zero `analyticsDropped` and exactly one
      warn log per 60 s.
- [ ] TUI Stats shows red indicator when drops > 0.
- [ ] `/admin/status` exposes all four counters.
- [ ] Graceful shutdown flushes ≤ 5 s with final-count log.

## Out of Scope

- Dynamic queue resizing at runtime.
- Disk-backed queue (not worth the complexity at current scale).

---

## Validation

```
go test -race ./internal/proxy/... ./internal/analytics/...
./scripts/benchmarks/run-session-report.sh
curl -s 127.0.0.1:8990/admin/status | jq .analytics_queue
```
