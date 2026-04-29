# TASK 106: Batched filter-DB writes

Status: closed - spec premise inaccurate, no-op
Priority: P2
Scope: `internal/filter/`, `internal/daemon/`
Driver: Every Layer 0 filter run commits to `filter.db` synchronously. On tool-heavy sessions (`git status` x N) this is the per-event bottleneck. Batched inserts via channel + periodic flush remove the bottleneck without sacrificing data integrity.

---

## Problem

`RecordFilterRun` performs an immediate `INSERT` + commit per call. On a session that runs 50 small commands, that is 50 fsyncs. Latency adds up and competes with the hot path.

## Target State

Filter records flow through a buffered channel. A goroutine drains the channel and:

- Batches up to `[filter.batch] size` (default 100) inserts.
- Flushes on size or `[filter.batch] interval` (default 200ms).
- Flushes on shutdown (lifecycle hook).

If the channel fills (slow disk), the producer falls back to a synchronous insert and increments `filter_batch_overflow_total`.

## Implementation Plan

### WP1 - Producer/consumer
- New `internal/filter/batcher.go` with the buffered channel, batch builder, periodic flush.

### WP2 - Lifecycle integration
- Daemon registers the batcher; shutdown drains it.

### WP3 - Telemetry
- Counters: `filter_batch_inserts_total`, `filter_batch_flushes_total`, `filter_batch_overflow_total`.

### WP4 - Tests
- Producer floods channel; assert size-flush + interval-flush + shutdown-drain.
- Race test: concurrent producers + a flush.

## Acceptance Criteria

- [ ] Filter inserts no longer block the hot path under tool-heavy load.
- [ ] No data loss on graceful shutdown.
- [ ] Counters expose batching effectiveness.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Cross-process batching.
- Switching DB engines.

## Validation

```
go test ./internal/filter/...
```

## Closure Notes (2026-04-30)

Audit of the actual write pattern:

- `slimference filter <cmd>` is a fresh subprocess per filtered tool
  call. Each invocation: opens filter.db, writes one row,
  closes the DB. There is no long-running process accumulating writes
  in a single connection.
- The proxy / daemon never calls `RecordFilterRun` directly; only the
  one-shot `filter` and `posttool` subcommands do.
- A buffered channel + periodic flush would only reduce fsyncs *within*
  one subprocess, but each subprocess writes exactly one row.
- Cross-process batching would require IPC and a long-running
  consumer process, which is far outside the task scope.

Closed as no-op. If a future change moves filter recording into the
long-running daemon (e.g. via a hook handler that loops), this task
should be reopened with the corrected premise.
