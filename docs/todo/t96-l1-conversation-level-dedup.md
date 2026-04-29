# TASK 96: Layer 1 conversation-level dedup

Status: todo
Priority: P1
Scope: `internal/compression/dedup.go`, `internal/compression/dedup_minhash.go`, `internal/compression/layer1.go`, `internal/sessions/`
Driver: Today dedup operates intra-message. Two identical `git diff` outputs in message 5 and message 17 do not find each other. Conversation-level dedup with stable hash references across messages is one of the largest unrealised L1 levers.

---

## Problem

`internal/compression/dedup.go` and `dedup_minhash.go` find duplicate or near-duplicate blocks within a message. Cross-message dedup is implicit (the second-occurrence message gets compressed independently) and produces no shared reference. The agent reads "the same diff" twice.

## Target State

Layer 1 keeps a per-conversation rolling shingle index. On a new block:

1. Hash the block.
2. Lookup against the index. If a previous identical or > 0.85-Jaccard match exists, replace the new block with `[content same as msg #N, see local-archive://<id>]`.
3. The original is archived (T76) and reachable via `slimference expand`.

Storage of the index uses an in-memory map per session, persisted to `~/.slimference/state/<session_id>/dedup_index.json` so daemon restarts do not lose context.

## Implementation Plan

### WP1 - Conversation index
- New `internal/compression/conv_dedup.go` with shingle index + LRU eviction (default 5000 entries per session).
- Persistence: JSON snapshot per session.

### WP2 - Layer 1 hook
- After existing dedup, run conversation-level dedup as a new sub-step.
- Replaces matched blocks with the marker; emits archive id.

### WP3 - T76 wiring
- Marker emission goes through the content archive recorder.

### WP4 - Counters + telemetry
- `conv_dedup_replacements_total`, `conv_dedup_bytes_saved_total`.

### WP5 - Tests
- Long-session fixture with cross-message duplicates; assert later messages emit markers.

## Acceptance Criteria

- [ ] Cross-message identical blocks are replaced with markers.
- [ ] Markers link to T76 archive for reverse path.
- [ ] Restart preserves the index for the session.
- [ ] Counter exposed in `/admin/status.compression`.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Cross-session dedup (would weaken privacy boundaries).
- Embedding-based similarity.

## Validation

```
go test ./internal/compression/...
go test -tags=integration ./tests/integration
```
