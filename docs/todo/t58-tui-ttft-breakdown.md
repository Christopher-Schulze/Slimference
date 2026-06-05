# T58 - TUI TTFT-Breakdown pro Layer

Status: todo
Priority: P2
Scope: `internal/analytics/`, `internal/proxy/handler.go`, `internal/tui/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

TUI currently shows aggregate savings. When a user asks "why is
Slimference slow?" or "which layer saves me the most?", there is no
way to answer without reading raw JSONL. A per-layer TTFT
(time-to-first-token) + per-layer byte-savings breakdown is the single
most requested diagnostic, per `docs/context.md` notes.

## Current State

- Analytics snapshot has total TTFT and total byte savings.
- Handler instruments phase timings sparsely; no per-layer total.

## Target State

TUI Stats view shows:

```
Request pipeline (rolling p50 / p95 ms):
  L0 pre-entry      0.3 / 0.8
  L1 deterministic  2.1 / 5.4    -42.1 % tokens
  L1 structure+del  0.9 / 2.2    -11.7 % tokens
  L2 MiniMax       340   / 780    -22.4 % tokens   (ran:  78 / 93)
  L2 cache-hit      0.1 / 0.3    -28.0 % tokens   (hits: 31 / 93)
  Upstream         620   / 1950
  Total (proxy-only)   3.3 / 8.7
  Total (incl. upstream) 623 / 1958
```

`/admin/status` exposes the same as JSON.

## Design

### Phase instrumentation

`internal/proxy/handler.go` wraps phases:

```go
phase := timings.Start("l1_deterministic")
compressed, l1Metrics := pipeline.Run(body)
phase.Stop()
```

`timings` is a per-request recorder; emits to analytics on response
finalisation.

### Analytics aggregation

`internal/analytics/phase_histogram.go`:

```go
type PhaseHist struct {
    name    string
    buckets []int64  // fixed bucket edges
    p50, p95 atomic.Uint64
}

func (h *PhaseHist) Observe(d time.Duration)
func (h *PhaseHist) Snapshot() PhaseSnap
```

Buckets at [1 ms, 2, 5, 10, 25, 50, 100, 250, 500, 1 s, 2.5 s, 5 s, 10 s,
∞]. Rolling window 200 samples via ring buffer.

Per-layer byte savings already available; expose per-request and
aggregate.

### TUI rendering

`renderPipeline()` in `internal/tui/views.go` reads snapshot and
renders the table above.

### JSON surface

```json
"pipeline": {
  "phases": [
    {"name": "l0",            "p50_ms": 0.3, "p95_ms": 0.8},
    {"name": "l1_deterministic","p50_ms": 2.1,"p95_ms": 5.4, "token_saving_pct": 42.1},
    {"name": "l2_minimax",    "p50_ms": 340, "p95_ms": 780,
     "token_saving_pct": 22.4, "ran": 78, "total": 93},
    {"name": "l3_cache",      "p50_ms": 0.1, "p95_ms": 0.3,
     "token_saving_pct": 28.0, "hits": 31, "total": 93},
    {"name": "upstream",      "p50_ms": 620, "p95_ms": 1950}
  ],
  "total_proxy_ms": {"p50": 3.3, "p95": 8.7},
  "total_full_ms":  {"p50": 623, "p95": 1958}
}
```

### Performance budget

Phase instrumentation must cost < 0.5 µs per phase on hot path. Use
`time.Now` (~20 ns) and atomic adds.

## Implementation Plan

### WP1 - Phase timings type
- Allocator-light recorder, single goroutine per request.

### WP2 - Rolling histogram with p50/p95
- Ring buffer with HDR-style approximation or simple sorted sample.

### WP3 - Handler instrumentation
- Wrap each layer; ensure no double-counting on early return.

### WP4 - Snapshot + admin JSON.

### WP5 - TUI render.

### WP6 - Benchmark to confirm instrumentation cost.

---

## Subtasks

- [ ] Phase recorder type.
- [ ] Rolling histogram with p50/p95.
- [ ] Handler phase wraps (L0/L1-det/L1-struct/L2-cache/upstream).
- [ ] Byte-savings aggregation per layer.
- [ ] Admin JSON extension.
- [ ] TUI table render.
- [ ] Benchmark: instrumentation cost < 0.5 µs per phase.
- [ ] Docs: `docs/documentation.md` §8 Analytics + §12 Operability.

## Risks

- Double-counting when overflow-recompress kicks in (runs L1 twice).
  Mitigation: tag phase with `attempt=1|2` and expose separately.
- TUI flicker at 500 ms tick. Render only if snapshot delta > epsilon.

## Acceptance Criteria

- [ ] TUI shows pipeline breakdown with p50/p95 and saving %.
- [ ] Admin JSON surface mirrors it.
- [ ] Benchmark: hot-path overhead ≤ 1 %.
- [ ] `go test -race ./...` green.

## Out of Scope

- Prometheus exposition (separate TASK).
- Per-session breakdown (aggregate only for now).

---

## Validation

```
go test -race ./internal/proxy/... ./internal/analytics/...
curl -s 127.0.0.1:8990/admin/status | jq .pipeline
go test -bench=BenchmarkPhaseRecorder ./internal/analytics/...
```
