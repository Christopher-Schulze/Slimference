# T57 - Read-Cache + Tool-Archive TUI-Live-Metriken

Status: closed (largely already implemented; remaining items rolled into stretch)
Priority: n/a

## 2026-04-20 Closure Note

Code verification revealed that the bulk of this task is already live:

- `internal/readcache/stats.go::Snapshot()` returns the full stats struct.
- `internal/toolarchive/toolarchive.go::Snapshot()` returns counts + bytes.
- `/admin/status` already exposes `read_cache` and `tool_archive` blocks
  (`internal/proxy/admin.go` `AdminReadCacheStatus` + `AdminToolArchiveStatus`).
- `internal/tui/model.go` has `GetReadCacheStatus()` and
  `GetToolArchiveStatus()`; `views.go` renders both in the dashboard view.

What's left unimplemented (carried forward as stretch, not blocking):

1. Explicit `hit_rate` field derived from Blocks/(Blocks+Allows) - currently
   TUI computes it inline; a computed field on the admin surface would make
   monitoring tools cheaper.
2. `bytes_cap` awareness and colour thresholds (amber at 80%, red at 95%).
   Requires wiring a cap source into the stats; non-trivial because the
   cap lives on the ToolArchive config side.
3. `evictions_total` counter - the readcache currently does not evict;
   sessions accumulate. If/when eviction is added this counter moves too.

Closed as largely-done. Reopen with a smaller spec if any of the three
stretch items gains concrete motivation from operator feedback.

---

# Original specification below (kept for historical reference)

Status: todo
Priority: P2
Scope: `internal/readcache/`, `internal/toolarchive/`, `internal/tui/`, `internal/admin/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

T37 (Read-hook cache + delta) and T40 (Tool-result archive) are both
active in production but their runtime state is not exposed to the
user. Operators cannot tell:

- is the Read-cache actually hitting?
- how many bytes are stored?
- how often does eviction run?
- has the tool archive hit its disk cap?

Without that visibility, the user cannot verify claimed savings or
diagnose "my cache does not seem to help" reports.

## Current State

- `internal/readcache/store.go` tracks hits / misses / entries in
  private counters.
- `internal/toolarchive` tracks item count, total bytes, recent
  additions.
- TUI Stats view does not render these.
- `/admin/status` exposes only a subset.

## Target State

TUI Stats view gains two dedicated rows:

```
Read-Cache:    hit 342 / miss 57   (85.7 %)   size  14.3 MiB / 64 MiB
Tool-Archive:  items 48            bytes 112 MiB / 256 MiB   last: 3 s ago
```

`/admin/status` JSON gains two structured blocks:

```json
"read_cache": {
  "enabled": true,
  "hits_total": 342,
  "misses_total": 57,
  "hit_rate": 0.857,
  "entries": 48,
  "bytes": 15000000,
  "bytes_cap": 67108864,
  "evictions_total": 3,
  "last_evict_ts": "2026-04-20T14:32:11Z"
},
"tool_archive": {
  "enabled": true,
  "items_total": 48,
  "bytes_total": 117440512,
  "bytes_cap": 268435456,
  "last_add_ts": "2026-04-20T14:31:08Z",
  "gc_runs_total": 2,
  "expand_invocations_total": 7
}
```

## Design

### Snapshot structs

`internal/readcache/store.go`:

```go
type Snapshot struct {
    Enabled        bool
    Hits           int64
    Misses         int64
    Entries        int
    Bytes          int64
    BytesCap       int64
    Evictions      int64
    LastEvictTime  time.Time
}

func (s *Store) Snapshot() Snapshot
```

`internal/toolarchive`:

```go
type Snapshot struct {
    Enabled       bool
    Items         int
    Bytes         int64
    BytesCap      int64
    LastAdd       time.Time
    GCRuns        int64
    ExpandCalls   int64
}
```

### Admin surface

`/admin/status` already assembles a root JSON from sub-snapshots. Add
two new keys.

### TUI rendering

`internal/tui/views.go` `renderStats` adds two new rows. Colour hints:

- Hit-rate < 40 % → amber.
- Bytes > 80 % of cap → amber; > 95 % → red.
- Last add > 10 min ago (and items > 0) → grey (quiet).

### Refresh cadence

Snapshots are cheap (atomic loads). Pull on TUI tick (500 ms).

## Implementation Plan

### WP1 - Snapshot APIs in readcache + toolarchive.
### WP2 - `/admin/status` extension.
### WP3 - TUI rendering rows.
### WP4 - Colour thresholds.
### WP5 - Tests
- Unit: snapshot reflects stored state.
- Integration: hit/miss flow updates counters.

---

## Subtasks

- [ ] `readcache.Store.Snapshot()` + fields.
- [ ] `toolarchive.Snapshot()` + fields.
- [ ] Wire into `/admin/status`.
- [ ] TUI Stats rows with colour rules.
- [ ] Unit tests on snapshot fields.
- [ ] Docs: `docs/documentation.md` §8 Analytics + §12 Operability.

## Risks

- Snapshot under lock vs atomic loads: keep atomic so TUI tick is
  cheap. Where lock is unavoidable (entry count), use RLock.
- Counters double-count if eviction path has two entry points.
  Verify with deliberate eviction test.

## Acceptance Criteria

- [ ] TUI Stats renders both rows.
- [ ] `/admin/status` JSON includes both blocks.
- [ ] Colour thresholds trigger on crafted scenarios.
- [ ] `go test -race ./...` green.

## Out of Scope

- Historical time-series (Prometheus scraping is a separate TASK).
- Per-tool hit breakdown (just totals for now).

---

## Validation

```
./slimference --no-tui &
curl -s 127.0.0.1:8990/admin/status | jq '.read_cache, .tool_archive'
# load a Read-heavy workload, watch TUI
```
