# TASK 101: Layer 3 cache invalidation on code change

Status: todo
Priority: P2
Scope: `internal/caching/`, `internal/proxy/handler.go`
Driver: Response cache is keyed on request body. When the underlying repository state changes (`git pull`, file edits) but the request looks identical, the cache returns a stale answer about code that no longer exists. Mtime / git-state-aware cache key fixes it.

---

## Problem

A request like "what does `auth.go` do?" can hit a cache entry from before a refactor that renamed `auth.go`. The proxy returns the old answer with no signal that it is stale. Today the cache invalidates only on LRU eviction or daemon restart.

## Target State

Cache key includes a lightweight code-state fingerprint:

- For each file path mentioned in the request body, compute `mtime + size` and feed it into the key hash.
- Optional `[caching] git_head_in_key = true` adds the active `git rev-parse HEAD` (cached per project_path with short TTL) so commits invalidate cache.
- Defaults: mtime hashing on, git head off (avoid surprising users with cross-branch invalidations).

## Implementation Plan

### WP1 - Path extraction
- A small parser pulls candidate file paths from request bodies (existing OpenAI/Anthropic content blocks and Codex Responses input).

### WP2 - Fingerprint helper
- `internal/caching/fingerprint.go` returns `(path, mtime, size)` tuples. Bounded: max N paths checked per request.

### WP3 - Key builder integration
- Existing cache-key path consumes the fingerprint as additional input.

### WP4 - Telemetry
- `cache_invalidations_due_to_fingerprint_change_total`.

### WP5 - Tests
- Touch-and-go test: same request, modify a file mtime, assert cache miss.

## Acceptance Criteria

- [ ] File mtime change causes a cache miss for the next request that references the file.
- [ ] Bounded path-check budget keeps hot-path latency unchanged.
- [ ] Counter exposed in `/admin/status.cache`.
- [ ] No regression on requests that do not mention any file path.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Reading file content into the key (mtime + size is enough).
- Cross-machine invalidation.

## Validation

```
go test ./internal/caching/...
```
