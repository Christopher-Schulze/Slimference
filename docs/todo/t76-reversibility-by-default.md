# TASK 76: Reversibility-by-default for lossy Layer 1 operations

Status: partial - WP1 / WP2(coarse + per-sub-layer attribution) / WP4 / WP5 done; WP3 (opportunistic re-injection) tracked as T76c
Priority: P0
Scope: `internal/compression/`, `internal/toolarchive/`, `internal/proxy/handler.go`, `internal/types/types.go`, `cmd/slimference/checkpoint_cmd.go`
Driver: T74 had to flip `structure_preview` back to default-off because preview-time content cannot be recovered. The same risk applies to dedup, comment-strip, JSON-compact, repeated-collapse, image-replace, and structure-extract. Without an archive layer behind every lossy operation, no aggressive mode can be safely default-on, no quality calibration loop is possible, and tool-definition pruning (T103) cannot be built.

---

## Problem

`internal/toolarchive` archives large tool-result payloads with a stable `local-archive://<id>` URI and exposes `slimference expand`. That mechanism is currently scoped to *tool-result* content. Layer 1 operations on regular message content are still write-once: once a comment is stripped or a duplicate block replaced with a reference, the original is gone. Recovery only works if the operator notices and replays.

This blocks several follow-on improvements (T74 default-on, T98 SAFETY-comment whitelist, T103 tool-definition pruning, T77 quality calibration loop) because each of them assumes "model can ask for the original".

## Target State

Every lossy Layer 1 operation that mutates message content writes a content-archive entry before the mutation, keyed by a stable hash over `(session_id, message_index, block_index, sub_layer, original_bytes_sha256)`. The mutated block carries an `archive_id` reference. Two recovery paths:

1. **Server-side opportunistic re-injection.** When the upstream response references a removed block (heuristic match on archive id markers, or repeated re-read of the same path/block), the next request re-injects the archived block before sending.
2. **Explicit retrieval** via the existing `slimference expand <archive-id>` command for human inspection.

Storage reuses `internal/toolarchive` SQLite under a separate table so eviction policies stay independent. Defaults stay safe: no behaviour changes until a sub-layer opts in to "archived-mutate" instead of "destructive-mutate".

## Implementation Plan

### WP1 - Archive contract and storage
- New package `internal/contentarchive` (or extension of `toolarchive`) with `Put(ContentRef, payload []byte) (id string, err error)` and `Get(id string) ([]byte, error)`.
- SQLite layout: separate table `content_archive` with same DB file as tool archive.
- Sizing knobs: `[contentarchive] max_bytes` (default 64 MiB), `max_entries` (default 5000).

### WP2 - Sub-layer integration
- Define `MutationRecorder` interface that lossy sub-layers consume.
- Migrate `comment_strip`, `json_compact` (string fields only), block-level `dedup`, `repeated_collapse`, `image_replace`, and `structure` (preview branch) to call the recorder.
- Each mutation result carries `archive_id` in a `types.ContentBlock` field; existing fields untouched.

### WP3 - Opportunistic re-injection
- Detect `local-archive://<id>` references in upstream response body / SSE chunks (lightweight pattern match, no LLM call).
- Re-inject the archived bytes into the next request's matching slot.
- Counter `re_inject_count`, surfaced under `/admin/status.contentarchive`.

### WP4 - Explicit expand path
- Extend `slimference expand` (and `cmd/slimference/checkpoint_cmd.go` shared helpers) to accept content archive ids as well as tool-result ids.
- Document the dual scope in `docs/integration.md`.

### WP5 - Default flips and docs
- Once WP1-WP4 land, flip `structure_preview` to default-on and re-open T74 closure note.
- Update `docs/documentation.md` with the new safety guarantee.
- Update `docs/savings-assessment.md` so aggressive-mode claims point at the safety net.

## Acceptance Criteria

- [ ] Every lossy L1 sub-layer that mutates message content uses the archive recorder.
- [ ] `/admin/status.contentarchive` exposes `entries`, `bytes_used`, `bytes_cap`, `re_inject_count`, `evictions`.
- [ ] `slimference expand <id>` retrieves both tool-result and content archives.
- [ ] An integration test that triggers an archive-id reference in the upstream response causes opportunistic re-injection on the next request.
- [ ] `structure_preview` ships default-on with a passing safety regression test.
- [ ] `go test ./...`, `-race`, integration, and `bun test tests/ts` green; `scripts/ci` PASS at coverage 100%.

## Out of Scope

- Cross-machine archive sync.
- Encrypted archive at rest (separate hardening track).
- Embedding-based re-injection trigger; heuristic match is sufficient for v1.

## Validation

```
go test ./internal/contentarchive/... ./internal/compression/... ./internal/proxy/...
go run ./scripts/ci
slimference expand local-archive://<id>
```

## Closure Notes (2026-04-30)

Landed in this pass:

- New `internal/contentarchive` package mirrors the `toolarchive` shape but
  archives lossy *content* mutations rather than tool outputs. Storage at
  `~/.slimference/content-archive/`, gzip + JSON metadata, eviction by
  `MaxEntries` / `MaxBytes` (defaults 5000 / 64 MiB), 100% covered.
- `MutationRecorder` interface, `NoopRecorder`, `DiskRecorder`, and an
  `archiveOriginal` helper on the deterministic compressor. Recorder is
  wired in `proxy.New` so production traffic archives by default.
- `types.ContentBlock.ArchiveID` field plus `CompressWithSession` to scope
  archive entries to the per-request session id.
- `compressMessage` archives once per block at the end, when any non-ANSI
  mutation occurred; `preview_pass` archives per block when the preview
  fires.
- `slimference expand <id>` now retrieves both tool-archive and
  content-archive entries, with toolarchive tried first for back-compat.
- `/admin/status.content_archive` exposes
  `count / archived / expanded / re_inject_count / evictions /
   bytes_raw / bytes_stored / last_*`.
- `structure_preview` ships **default on** (T74 default-off contract was
  scoped to "until preview is reversible"; T76 satisfies that condition).
  T76 regression tests pin the new default in both the Go struct and the
  generated TOML template.
- Full proof stack green: `go run ./scripts/ci` PASS at 100.0% coverage,
  `go test -race ./...` green, `bun test tests/ts` green,
  integration tests green.

Deferred to follow-up tasks (kept out of scope here so the foundation can
land cleanly):

- **WP2 per-sub-layer attribution.** The current end-of-block archive
  records a single entry per mutated block tagged `sub_layer="layer1"`.
  For full audit visibility, `comment_strip`, `dedup`, `structure_extract`,
  `delta`, `tool_compressor`, `success_short_circuit`, `image_replace`,
  `repeated_collapse`, and `graph_pruning` should each archive
  individually with their own subLayer tag. Tracked as T76-followup-A.
- **WP3 opportunistic re-injection.** The `contentarchive.RecordReInject`
  helper and the `re_inject_count` counter exist, but the proxy does not
  yet detect archive-id references in upstream responses and re-inject
  the archived bytes in the next request. Tracked as T76-followup-B.
- **Integration test for end-to-end reversibility flow.** Today the
  contract is unit-tested per layer; an integration test that simulates
  "model asks about archived block -> re-injection happens" needs WP3
  first.
