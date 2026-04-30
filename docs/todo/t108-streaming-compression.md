# TASK 108: Streaming compression for long tool outputs

Status: deferred - see docs/todo.md for closure rationale
Priority: P2
Scope: `internal/compression/`, `internal/proxy/handler.go`, `internal/filter/streamfilter.go` (T94)
Driver: Today compression runs on whole bodies. Long tool outputs (100MB log dumps, multi-minute test runs) blow the working-set budget and produce a latency cliff. Chunked compression during the tool run keeps memory and latency bounded.

---

## Problem

`compress(body)` is whole-body. For a 200MB log file, the proxy buffers the whole thing before compressing. Memory peaks, latency spikes, and the operator sees a frozen session for several seconds.

## Target State

Layer 1 sub-layers that are streaming-safe (ANSI strip, line-based dedup, repeated-line collapse) run on a chunked pipe:

- Buffered window of last `[compression.streaming] window_lines` lines (default 500).
- Output emitted as compacted chunks while the tool is still running.
- Non-streaming-safe sub-layers (structure extract, JSON compact across boundaries) still run at end-of-stream on the compacted result.

T94 (streaming Layer 0) is the producer side; T108 is the proxy side that also handles streaming bodies arriving via the request pipeline.

## Implementation Plan

### WP1 - Chunk pipeline
- New `internal/compression/streaming.go` with a Reader-Writer wrapper.

### WP2 - Streaming-safe sub-layers
- Mark each sub-layer with `streaming_safe bool`. Pipeline picks safe ones for chunked path.

### WP3 - Boundary handling
- End-of-stream finalisation runs unsafe sub-layers on the compacted body.

### WP4 - Tests
- Synthetic 100k-line stream; assert memory ceiling and end-to-end equivalence with whole-body output.

## Acceptance Criteria

- [ ] Streaming bodies do not buffer fully in memory before compression.
- [ ] End-result token count is within X% of whole-body baseline.
- [ ] No race conditions on the chunk pipeline.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- True online (1-pass) compression for all sub-layers; some inherently need full body.

## Validation

```
go test -race ./internal/compression/...
```
