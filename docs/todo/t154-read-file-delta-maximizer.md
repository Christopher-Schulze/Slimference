# TASK 154: Read/File delta maximizer

Status: DONE
Priority: P0
Parent: T37/T125/T143
Scope: `internal/readcache/`, `internal/compression/`, `internal/proxy/layer0_proxy.go`, `internal/proxy/handler.go`, `internal/contentarchive/`, `cmd/slimference/expand-body`

## Why

Repeated file reads are one of the most common coding-agent costs. T37 handles hook-time read-cache/delta, and T125 handles safe Go AST compaction for large reads. The remaining win is to make file-read history inside proxied model requests delta-aware and archive-expandable.

## Target State

File-read content is sent once, then updated by compact deltas:

1. First full read archives and optionally compacts by safe structure.
2. Unchanged reread becomes a stable reference.
3. Changed reread becomes a concise textual delta.
4. Recently edited or likely-to-edit files bypass aggressive delta unless T149 approves.
5. Full content remains expandable by archive ID.
6. Savings are counted only when delta is shorter than full content and archive is valid.

## Implementation Plan

### WP1 - Proxy-visible read detection

- [x] Detect file-read tool results inside proxied request bodies before the
  deterministic Layer 0 proxy pass mutates content.
- [x] Extract path, session, observed content hash, and archive ID from
  command-shaped `cat` / `sed -n` / `head` / `tail` file reads.

### WP2 - Delta store

- [x] Extend `internal/readcache` entries with content hash and archive URI.
- [x] Keep the prior full observed content only for session-scoped delta
  rendering; unknown sessions remain fail-open.

### WP3 - Delta renderer

- [x] Emit stable unchanged-file references with an expandable
  `local-archive://` URI.
- [x] Emit changed-file textual deltas only when shorter than the current full
  content, and include the new full-content archive URI for recovery.

### WP4 - Safety gates

- [x] Bypass read-delta compression when the same-session hook state marks the
  file as recently edited.
- [x] Keep unknown paths, unknown sessions, missing archives, and non-shorter
  deltas as fail-open passthrough.
- [x] Leave T149 as the future controller for more aggressive task-shape
  selection.

### WP5 - Tests

- [x] Full read -> unchanged reread reference.
- [x] Full read -> changed reread delta.
- [x] Recent edit bypasses read-delta compression.
- [x] Archive expansion returns original content.

## Acceptance

- [x] Proxy-visible file reads can be delta-encoded.
- [x] Unchanged rereads collapse to a stable reference.
- [x] Changed rereads use shorter textual deltas only.
- [x] Recent edit safety bypass works.
- [x] Archive expansion works.
- [x] `go test ./...` passes.

## Implementation Notes

- `readcache.EvaluateObserved` archives the full observed read through
  `internal/contentarchive` before returning any compact reference.
- Unchanged rereads return a stable reference to the archived full content.
- Changed rereads use the existing text delta renderer and append the archive
  URI for the new full content.
- `proxy.applyProxyLayer0WithSession` applies this path only when a session id
  and a concrete read command path are available.
- `sessions.RecentlyEditedHookFile` disables the aggressive read-delta path for
  recent same-session edits, while the older deterministic Layer 0 compaction
  may still apply if it is safe and shorter.
- The final hard-verification pass also tightened Layer 2 huge-input handling:
  formatted summariser input is capped before preprocessing/density scoring, and
  adaptive target sizing is based on the actually submitted text.

## Verification

- `go test ./internal/readcache ./internal/proxy -count=1`
- `go test ./...`
- `go run ./scripts/ci`
- `go test -race ./cmd/... ./internal/...`

## Non-Goals

- No binary file delta.
- No blind path inference from truncated tool output.
- No delta-of-delta chains.
