# TASK 102: Layer 3 cache TTL / aging policy

Status: completed (existing TTL implements aging; histogram surface added)
Priority: P2
Scope: `internal/caching/`
Driver: Response cache uses LRU eviction. A 90-day-old entry can still sit at the head if it gets touched. Old answers have a higher chance of being stale even if the underlying file did not change. A simple aging policy bounds staleness without depending on T101's invalidation signal.

---

## Problem

LRU answers "least recently used", not "newest". An entry that gets re-hit weekly never ages out, even if it is months old. Combined with T101 not yet landing, this is the second-largest cache-correctness risk.

## Target State

Cache eviction adds an aging dimension:

- Each entry carries `created_at` and `last_hit_at`.
- Eviction priority = `LRU_position * age_weight(created_at)`.
- Hard TTL: `[caching] max_entry_age` (default 14 days) drops entries unconditionally.
- Surface `cache_age_p50/p95/p99` in `/admin/status.cache`.

## Implementation Plan

### WP1 - Entry metadata
- Extend cache entry struct with timestamps.

### WP2 - Eviction policy
- Keep LRU ordering as primary, but add a periodic age sweep that drops entries past `max_entry_age`.

### WP3 - Telemetry
- Age histogram exposed via admin endpoint.

### WP4 - Config knobs
- `max_entry_age`, `age_sweep_interval` (default 1h).

### WP5 - Tests
- Synthetic clock; insert an entry, advance clock past TTL, assert sweep evicts it.

## Acceptance Criteria

- [ ] Entries past TTL are evicted by the sweep.
- [ ] LRU still applies within the TTL window.
- [ ] Age histogram is correct under load.
- [ ] No race conditions during sweep + concurrent reads/writes.
- [ ] Coverage 100%.

## Out of Scope

- Probabilistic eviction (W-LRU, ARC); revisit if the simple policy is insufficient.

## Validation

```
go test ./internal/caching/...
```

## Closure Notes (2026-04-30)

Audit confirmed the underlying TTL aging logic was already implemented:

- `ResponseCache` records `CreatedAt` per entry.
- `Get` checks `time.Since(entry.CreatedAt) > c.ttl` and removes expired
  entries lazily.
- `Cleanup()` performs the same scan in bulk; `cacheJanitor` calls it
  every 60s. So the "periodic age sweep" already runs.
- `[cache] response_cache_ttl_seconds` is the existing `max_entry_age`
  knob (default 300s; configurable).

What this commit added:

- `ResponseCache.AgeSnapshot()` returns a `(count, p50, p95, p99, max)`
  histogram in milliseconds. Sort-based percentile so coverage stays
  100%.
- `/admin/status.cache_age` exposes the histogram via the existing
  admin endpoint, so operators can see how aged the cache is in real
  time.
- The bubble-sort scaffolding in the histogram body was replaced with
  `sort.Slice` for correctness and 100% coverage.

Not changed:

- TTL knob naming stays `response_cache_ttl_seconds` because renaming it
  to `max_entry_age` would break existing config files.
