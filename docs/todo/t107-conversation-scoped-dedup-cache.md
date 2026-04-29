# TASK 107: Conversation-scoped dedup hash cache

Status: todo
Priority: P2
Scope: `internal/compression/dedup.go`, `internal/compression/dedup_minhash.go`, `internal/sessions/`
Driver: Dedup recomputes shingle hashes for the entire body on every request. Long sessions repeat much of the same content. A per-conversation hash cache reduces compute proportionally to history length.

---

## Problem

Each request runs the full shingle/MinHash pipeline against the entire compressible body. In a 200-message session most of that history is identical to the previous request. The work is duplicated every turn.

## Target State

A per-conversation cache keyed on message id (or content hash) stores the precomputed shingle set. Layer 1 dedup checks the cache before computing. New messages produce new entries; the cache evicts on session close or LRU.

This task pairs with T96 (conversation-level dedup): T96 introduces the index, T107 makes the index cheap to maintain.

## Implementation Plan

### WP1 - Cache layout
- `internal/compression/conv_dedup_cache.go` with per-session map, LRU eviction.

### WP2 - Hot-path integration
- Dedup pass checks cache; cache miss runs computation, stores result.

### WP3 - Persistence
- Snapshot to disk on graceful shutdown so daemon restart reuses work.

### WP4 - Tests
- Long-session benchmark: assert speedup.

## Acceptance Criteria

- [ ] Repeated messages do not re-shingle.
- [ ] Cache hits and misses are counted.
- [ ] No correctness regression on existing dedup tests.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Cross-session shingle sharing.

## Validation

```
go test ./internal/compression/...
go run ./scripts/benchmarks -- -pkg=compression
```
