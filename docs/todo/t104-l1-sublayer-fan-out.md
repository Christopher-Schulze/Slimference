# TASK 104: Goroutine fan-out across independent L1 sub-layers

Status: deferred - see docs/todo.md for closure rationale
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

- [ ] Layer 1 latency on a 200KB body drops by >= 30% on a 4-core machine.
- [ ] No regression on small bodies.
- [ ] Race tests green.
- [ ] Coverage 100%.

## Out of Scope

- Parallelising sequential dependencies (would require deeper refactor).

## Validation

```
go test -race ./internal/compression/...
go run ./scripts/benchmarks -- -benchtime=3s -pkg=compression
```
