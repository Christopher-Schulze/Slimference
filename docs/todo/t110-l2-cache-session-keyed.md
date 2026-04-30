# TASK 110: Layer 2 cache - session-keyed multi-slot replacement

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P0
Scope: `internal/summarization/cache.go`, `internal/summarization/layer2.go`, `internal/proxy/handler.go`
Driver: Today `SummaryCache` is a 2-slot singleton (current + previous) shared across **every** request the proxy handles. A second concurrent CLI session that triggers Layer 2 will overwrite the first session's cached summary. When the first session's next request arrives, `ApplyToMessages` will splice in **the wrong session's summary** - synthetically prepended to a different conversation. This is a correctness bug, not a tuning issue, and it blocks Layer 2 default-on for any deployment that serves more than one CLI tool simultaneously.

---

## Problem

`internal/summarization/cache.go`:
- One global `Layer2.cache *SummaryCache` constructed once in `NewLayer2`.
- `SummaryCache` holds `current *CachedSummary` + `previous *CachedSummary` as struct fields.
- `Store(s)` demotes `current` to `previous` regardless of which conversation `s` belongs to.
- `GetCurrent()` returns whatever was stored last.
- `Hash` field on `CachedSummary` exists but is never used for lookup.

The result: if you run Claude Code and Codex simultaneously through the same proxy daemon, or two Claude Code instances on different projects, Layer 2 will splice cross-session summaries.

Today this is masked because Layer 2 is gated behind heavy thresholds (`MinTokensForLayer2 = 15000`) and the typical operator runs a single CLI. But T121 will move L2 toward default-on, and the proxy is already meant to support multi-tool concurrent use.

## Target State

`SessionScopedCache` keyed on a stable session identifier:

- Map `sessionID -> CachedSummary` (current + previous tuple per session).
- LRU eviction when number of distinct sessions exceeds `[compression.summary] cache_max_sessions` (default 64).
- TTL (existing 30-min staleness) applies per entry.
- Hash-based invalidation: when the upstream content hash diverges from the cached hash beyond a tunable similarity threshold, drop the entry rather than serving a stale-summary-for-different-conversation.

Session ID source (existing infrastructure):

- Anthropic: `anthropic-organization-id` + extracted-from-body `metadata.user_id` if present, else `r.Header.Get("anthropic-trace-id")`, else hash of the first user message.
- OpenAI / Codex: `r.Header.Get("openai-conversation-id")` if present, else `previous_response_id`, else hash of the first user message.
- Local fallback: SHA-256 of `(provider, first 200 chars of first user message)` so two distinct conversations never collide accidentally.

Disk persistence (graceful shutdown -> daemon restart):

- On `Proxy.Stop()` snapshot the cache to `~/.slimference/layer2_cache/<session_id>.json`.
- On `NewLayer2` rehydrate from disk; entries past TTL are pruned during load.
- Disk cap: `[compression.summary] cache_disk_max_mb` (default 64MB), oldest evicted first.

## Implementation Plan

### WP1 - SessionScopedCache type
- New `internal/summarization/cache.go` SessionScopedCache wrapping `map[string]*sessionEntry` with `sync.RWMutex`.
- `sessionEntry{current, previous *CachedSummary, lastTouch time.Time}`.
- LRU via `container/list` for eviction order; max entries from cfg.

### WP2 - Session ID extraction
- New `internal/summarization/session_id.go` with `ExtractSessionID(provider types.Provider, body []byte, headers http.Header) string` plus per-provider helpers.
- Tested per-provider with header / body / fallback fixtures.

### WP3 - Layer2 wire-in
- `Layer2.ApplyToMessages` accepts `sessionID string` parameter.
- `Layer2.RunCompressionJobContext` accepts `sessionID string`; stores via `cache.Store(sessionID, ...)`.
- Handler passes session ID through (already extracted for telemetry).

### WP4 - Hash-based invalidation
- `CachedSummary.Hash` becomes the SHA-256 of the canonical input messages.
- `cache.Get(sessionID, currentInputHash)` returns nil when hash differs and similarity score (`differentiateSimilarity`) below threshold.

### WP5 - Disk persistence
- `Layer2.SnapshotToDisk(dir)` and `Layer2.LoadFromDisk(dir)`. Best-effort; corruption tolerated by skipping bad files.
- Wired into `Proxy.Stop` and `NewLayer2`.
- Disk cap enforcement on every snapshot write.

### WP6 - Telemetry
- `/admin/status.layer2.cache.{sessions, hits, misses, evictions, stale_hits, hash_mismatches, disk_loaded, disk_saved}`.
- Per-session counts visible via `slimference debug cache`.

### WP7 - Migration / safety
- Existing tests using `Layer2.cache.Store` directly: migrate to `cache.Store(sessionID="", ...)` (legacy global slot for back-compat in test fixtures).
- Production path always passes a real session id; the empty-string slot is reserved for tests + the deterministic stub paths.

### WP8 - Tests
- Concurrent two-session test: drive two distinct sessions through `ApplyToMessages` interleaved; assert each one only sees its own summary.
- Hash-mismatch test: store summary, mutate input, assert cache miss.
- Disk roundtrip test.
- LRU eviction test (cache_max_sessions=2, store 3 sessions, oldest evicted).

## Acceptance Criteria

- [ ] No code path can read a Layer 2 summary for session A and apply it to session B.
- [ ] `cache_max_sessions` enforced; eviction order is LRU.
- [ ] Disk snapshot/load roundtrip preserves entries within TTL.
- [ ] Telemetry counters cover the 8 events above.
- [ ] Coverage 100%; race tests green with t.Parallel concurrent sessions.

## Out of Scope

- Cross-process cache sharing (would require IPC; tracked separately if ever needed).
- True content-addressable cache where two sessions with identical prefixes share one entry (T96 already covers content dedup at L1; doing it at L2 is risky given anchor differences).

## Validation

```
go test -race ./internal/summarization/... ./internal/proxy/...
go run ./scripts/verify
```
