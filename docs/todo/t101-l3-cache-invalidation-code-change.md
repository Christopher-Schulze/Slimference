# TASK 101: Layer 2 cache invalidation on code change

Status: closed - already implemented via ExtractDependencyPaths + fileWatcher
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

## Closure Notes (2026-04-30)

Audit confirmed the contract is already in production:

- `caching.ExtractDependencyPaths(body)` extracts file-shaped paths from
  request bodies. Used at request time by `proxy/handler.go` (line 337).
- `caching.FileWatcher` monitors each extracted path; on filesystem
  change events the watcher calls `ResponseCache.Invalidate(path)`,
  which prunes every entry whose `DependencyPaths` mention that path.
- `CacheEntry.DependencyPaths` carries the path list per entry; entries
  set this in `proxy/handler.go::Set` (line 369).

The task was originally framed in terms of mtime+size cache key
fingerprinting. The existing dependency-watcher approach is strictly
better: it invalidates on real filesystem events and does not break the
cache for benign no-op rewrites that preserve content.

Closed as already implemented. If a future bug shows file watcher
events are unreliable on a particular platform, this task can be
reopened with that concrete failure mode as the driver.
