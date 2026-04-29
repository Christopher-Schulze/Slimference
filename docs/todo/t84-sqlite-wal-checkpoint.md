# TASK 84: SQLite WAL periodic checkpoint

Status: todo
Priority: P1
Scope: `internal/filter/`, `internal/analytics/`, `internal/readcache/`, `internal/toolarchive/`, `internal/daemon/`
Driver: All four SQLite stores (filter.db, analytics.db, readcache, toolarchive) run in WAL mode. Without periodic `wal_checkpoint(TRUNCATE)`, the WAL file grows unbounded under long-running daemon uptime, eventually exhausting disk and degrading write performance.

---

## Problem

WAL mode gives correctness and concurrency, but Go's stdlib + `modernc.org/sqlite` does not auto-checkpoint aggressively when the daemon is the only writer. Long uptimes (which are explicitly the supported launchd KeepAlive case) accumulate WAL bytes. There is no safety valve in the codebase today.

## Target State

A single `internal/sqliteops` helper exposes `Checkpoint(db, mode)` and a per-store ticker that runs it on a schedule. Each store registers itself with the daemon's lifecycle so the ticker is cancellable on shutdown. Defaults are conservative:

- Idle threshold: trigger only when no writes happened in the last N seconds (default 30s) so we never block a hot path.
- Periodic floor: at most every 5 minutes regardless of idle (covers chatty workloads).
- TRUNCATE mode by default (returns disk).

A counter exposes `wal_checkpoint_runs` and `wal_checkpoint_pages_reclaimed` per DB at `/admin/status.sqlite`.

## Implementation Plan

### WP1 - Checkpoint helper
- `internal/sqliteops/checkpoint.go` wrapping `PRAGMA wal_checkpoint(TRUNCATE)`.
- Returns checkpoint result and rough pages-reclaimed count.

### WP2 - Lifecycle integration
- Daemon registers each store via `RegisterCheckpoint(name, db, opts)`.
- Internal scheduler ticks per registered store with backoff.

### WP3 - Telemetry
- `/admin/status.sqlite` exposes per-store last checkpoint timestamp, runs, and pages reclaimed.

### WP4 - Tests
- Unit test: open a temp DB, write, force-tick, assert WAL file shrinks.
- Race test: concurrent writes during a checkpoint do not deadlock.

## Acceptance Criteria

- [ ] All four stores register with the checkpoint scheduler.
- [ ] WAL file size stays bounded under a synthetic 24h-equivalent write load.
- [ ] `/admin/status.sqlite` exposes the counters.
- [ ] Shutdown cancels in-flight checkpoints cleanly (no orphan goroutines).
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Switching off WAL.
- Online VACUUM (separate concern, far heavier).

## Validation

```
go test ./internal/sqliteops/... ./internal/daemon/...
curl localhost:8990/admin/status | jq .sqlite
```
