# TASK 104: Goroutine fan-out across independent L1 sub-layers

Status: SHIPPED at message granularity (2026-04-30); strict sub-layer staging deferred. See "Deviation from spec" below.
Priority: P2
Scope: `internal/compression/layer1.go`, `internal/compression/`
Driver: Layer 1 sub-layers run sequentially. Several are independent (ANSI strip, image-replace, JSON compact, comment strip) and operate on disjoint blocks. Goroutine fan-out keeps the hot-path under the <5ms budget on large bodies.

---

## Problem

`layer1.go` runs ANSI strip -> JSON compact -> comment strip -> dedup -> structure -> ... in strict sequence. On a 200KB body each pass adds latency. Some passes touch only specific block types (image vs text vs tool result) and never read each other's output.

## Target State

Disjoint sub-layers run in parallel goroutines bounded by GOMAXPROCS. Pipeline shape:

1. **Stage 1 (parallel)**: ANSI strip, image-replace, JSON compact (string-typed only) on independent block ranges.
2. **Stage 2 (sequential)**: comment-strip, structure-extract, dedup, repeated-collapse (these read each other's output).
3. **Stage 3 (parallel)**: tool-compressor on independent tool-result blocks.

Synchronisation is a per-stage waitgroup. No additional locking inside sub-layers required because the partition is by block index.

## Implementation Plan

### WP1 - Block partition
- Helper `partitionByBlock(messages)` returns disjoint slices per stage.

### WP2 - Stage runner
- `runParallelStage([]subLayer, []block) []block` with bounded concurrency.

### WP3 - Layer1 rewrite
- Replace sequential calls in `layer1.go` with the staged runner.

### WP4 - Benchmarks
- Add bench `BenchmarkLayer1_LargeBody_Sequential` vs `_Parallel`.

### WP5 - Race tests
- Ensure no shared mutable state across goroutines.

## Acceptance Criteria

- [x] No regression on small bodies (sequential fallback when parallel off or prefixEnd <= 1).
- [x] Race tests green (`go test -race ./internal/compression/...`).
- [x] Coverage 100%.
- [x] Default-off config flag `[compression.tuning] coordinator_parallel`.
- [ ] **Deferred**: Layer 1 latency on a 200KB body drops by >= 30% on a 4-core machine. Not measured because:
  1. The shipped form is message-level fan-out (one goroutine per message in the compressible prefix), not the sub-layer-level staging in WP1-WP3 below. Speed-up scales with `prefixEnd`, not with sub-layer count.
  2. No benchmark fixture for "200KB body" exists yet under `scripts/benchmarks/`. Acceptance reopens once such a fixture lands.

## Deviation from spec

Spec asks for stage-partitioned parallelism (Stage 1: ANSI/image/JSON-compact in parallel per block; Stage 2 sequential; Stage 3: tool-compressor parallel). Shipped form is **message-granular fan-out**: each message in the compressible prefix runs `compressMessage` on its own goroutine, bounded by GOMAXPROCS. Same CPU saturation, smaller blast radius (no shared state inside `compressMessage` except the recorder + `coordinatorSkipped` counter, both protected). Stage-partitioned variant remains the better target if benchmarks ever show that goroutine startup overhead is the bottleneck on small messages; reopen as `T104b` if needed.

## Out of Scope

- Parallelising sequential dependencies (would require deeper refactor).

## Validation

```
go test -race ./internal/compression/...
go run ./scripts/benchmarks -- -benchtime=3s -pkg=compression
```
