# TASK 120: Filter dispatch panic recovery + per-filter observability

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P1
Scope: `internal/filter/pipeline.go`, `internal/filter/dispatch.go`, `internal/filter/observability.go` (new), `internal/filter/tracking.go`
Driver: `applyLayer0Filters` (`pipeline.go:84-162`) calls 25+ `TryCompact*` functions in sequence. None are wrapped in `defer/recover`. A panic in any single filter (regex bug, nil deref on edge-case input, malformed UTF-8) crashes the entire `slimference filter` subprocess - the LLM agent gets no output, the user-visible session sees a tool failure, the build/test fails. Plus: there is zero per-filter observability. We can't tell which filter is slow, which one fired, or which one short-circuited. Both gaps fixed in one task.

---

## Problem

```go
// pipeline.go (current)
func applyLayer0Filters(workDir string, argv []string, stdout []byte) ([]byte, string) {
    if out, ok := TryCompactGitStatus(argv, stdout); ok { return out, "git_status" }
    if out, ok := TryCompactGitDiff(argv, stdout); ok { return out, "git_diff" }
    ...
}
```

If `TryCompactGitStatus` panics on a malformed input (unicode edge case, regex panic on huge input, nil map access), the whole subprocess dies. The hook system sees an exit code of 1 or 2 (depending on whether bash trapped the signal); the agent sees no stdout. Layer 0 just disappears.

No per-filter timing either. Even when filters run successfully, we can't answer:
- Which filter matched this argv?
- How long did it take?
- Did the filter actually save bytes (input vs output sizes)?
- Are any filters approaching pathological latency on certain inputs?

## Target State

1. **Panic recovery**: every `TryCompact*` call wrapped via a single `runFilter(name, fn)` helper that defers `recover()`, logs the panic with stack + filter name + argv0 + stdout-size, increments a panic counter, and returns `(stdout, false)` so dispatch falls through to the next filter or passthrough.
2. **Per-filter observability**: each call records `(filter_name, elapsed_ns, in_bytes, out_bytes, matched bool, panicked bool)` in a local ring buffer. Aggregated counters surfaced via `/admin/status.layer0.filters[]` showing per-filter call_count, p50/p95/p99 latency, avg-savings, panic_count.
3. **Slow-filter alerting**: when any filter exceeds `[filter.tuning] slow_filter_threshold_ms` (default 50ms) for a single call, log a `WARN` with the input shape (length, argv0). Repeated slow events from the same filter raise a one-time deprecation hint.

## Implementation Plan

### WP1 - Wrapped dispatch helper
- New `internal/filter/dispatch.go::runFilter(name string, fn func() ([]byte, bool)) ([]byte, bool, FilterStats)`.
- Returns stats: `{Name, Elapsed time.Duration, Panicked bool, MatchedFilter bool, InBytes, OutBytes int}`.
- `defer recover()` -> log + counter + stats.Panicked=true.

### WP2 - applyLayer0Filters refactor
- Replace each direct `TryCompactX(...)` call with `runFilter("X", func() (...) { return TryCompactX(argv, stdout) })`.
- Append every `FilterStats` to a per-call slice that gets logged + counted.

### WP3 - Per-filter counters
- New `internal/filter/observability.go` with `FilterCounters` struct:
  - `callsByName map[string]int64` (atomic via sync.Map of *atomic.Int64)
  - `panicsByName map[string]int64`
  - `latencyHistByName map[string]*analytics.Histogram`
  - `bytesSavedByName map[string]int64`
- Hot path uses sync.Map with per-filter struct + atomic fields; lock only on map insert.

### WP4 - Slow-filter detector
- After each `runFilter`, check elapsed against threshold; on overage, log `WARN filter_slow` with name, elapsed, in_bytes.
- 1/min rate-limit per filter name (similar to existing analytics-queue warn pattern).

### WP5 - Admin surface
- `/admin/status.layer0.filters` returns the JSON snapshot.
- TUI Stats view gains a "FILTER PERFORMANCE" panel showing top 10 slowest + top 5 highest-saving + any panic-counted entries.
- `slimference gain --by-filter` prints the per-filter savings table.

### WP6 - Tests
- Synthetic panicking filter via test-only registration; assert pipeline survives + counter increments.
- Slow-filter threshold trip; assert warn fires once + counter.
- Per-filter histogram values against fixed input.
- Concurrent dispatch (race detector).

### WP7 - Existing filter audit
- One-time pass to identify any current filter with regex that could blow up (catastrophic backtracking). Document in `docs/layer0-filter-safety.md`.
- Replace any genuinely vulnerable regex with `regexp/syntax`-safe alternatives or input-size guards.

## Acceptance Criteria

- [ ] No filter panic can crash the `slimference filter` subprocess.
- [ ] Each filter has a counter for calls, panics, and latency p50/p95/p99.
- [ ] Slow-filter warn fires with rate-limit; observable via log inspection.
- [ ] `/admin/status.layer0.filters` returns the snapshot.
- [ ] `slimference gain --by-filter` prints per-filter savings.
- [ ] Coverage 100%; race tests green.
- [ ] Audit document committed.

## Out of Scope

- Auto-disabling a filter that exceeds its panic budget (could be added later; first we want visibility).
- Per-input-shape tuning (some filters are slow on certain shapes - tracked as a follow-up if observability shows it).

## Validation

```
go test -race ./internal/filter/...
go run ./scripts/ci
```
