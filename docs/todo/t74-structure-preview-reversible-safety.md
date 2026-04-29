# T74 - Structure-Preview Reversible Safety

Status: done
Priority: P1
Scope: `internal/compression/preview*.go`, `internal/toolarchive/`, `cmd/slimference/checkpoint_cmd.go`, `internal/proxy/admin.go`, `docs/todo/t55-structure-preview-default-on.md`, `docs/documentation.md`
Driver: Structure preview had become default-on while its original T55
reversibility criteria were still open.

---

## Problem

Before T74, `StructurePreview` was enabled by default. The pass replaces large tool-result
text with a shorter shape-aware preview. That saves tokens, but the current
compression pass does not archive the original body or embed an expandable
archive reference. The T55 task explicitly lists reversibility and expand-rate
telemetry as open acceptance criteria.

This is acceptable as an experimental opt-in. It is risky as a default-on
production behavior because a rare critical line can be removed from old
context without a local recovery path.

## Target State

Pick one safe default:

1. **Preferred:** preview is reversible.
   - Archive original output via `internal/toolarchive`.
   - Preview text includes `local-archive://<id>` plus the archive ID.
   - Expand returns the exact original bytes.
   - Admin/TUI expose preview count and expand count.
2. **Fallback:** preview default is off until reversible.
   - `structure_preview = false` by default.
   - Docs explain how to opt in.

No default-on lossy preview without recovery.

T74 chose option 2: preview is opt-in until a future task wires archive-backed
recovery directly into the preview pass.

## Implementation Plan

### WP1 - Choose default policy
- [ ] If T74 can be implemented quickly, keep default-on and add archive recovery.
- [x] If not, flip default off first and re-open reversible preview as follow-up.

### WP2 - Archive integration
- [ ] Extend preview pass with an archive writer abstraction.
- [ ] Archive before replacement.
- [ ] Include enough metadata: provider, request ID when available, tool name,
  tool result ID, message index, source `structure_preview`.
- [ ] Handle archive failure by skipping preview, not by dropping content.

### WP3 - Preview text format
- [ ] Keep preview useful even without expansion.
- [ ] Add explicit recovery line:
  `Full output archived: local-archive://<id> (archive ID: <id>)`
- [ ] Preserve existing shape summary below it.

### WP4 - Telemetry
- [ ] Count preview applied/skipped/archive-failed.
- [x] Count expand invocations already tracked by toolarchive.
- [x] Add admin status fields and docs for existing tool archive counters.

### WP5 - Tests
- [ ] End-to-end: long tool result -> preview contains archive ID -> expand returns
  exact original.
- [ ] Archive failure -> original text remains unchanged.
- [x] Default config test matches chosen policy.

## Acceptance Criteria

- [x] Default-on preview has an archive ID and local expand recovery, or is not default-on.
- [x] Archive failure never causes lossy default preview because preview is opt-in by default.
- [x] `slimference expand <id>` returns exact original output for tool-archive entries.
- [x] Admin status exposes existing tool archive archive/expand counters.
- [x] If reversibility is not implemented, default is off and docs are updated.
- [x] `go test -race ./internal/compression/... ./internal/toolarchive/... ./cmd/slimference/...` green.

## Out of Scope

- Remote archive sync.
- Automatic model-triggered expansion.
- Per-provider preview learning.

## Validation

```
go test -race ./internal/compression/... ./internal/toolarchive/... ./cmd/slimference/...
slimference expand <id>
curl -s 127.0.0.1:8990/_slimference/admin/status | jq .tool_archive
go run ./scripts/ci
go test -race ./...
```

## Closure Notes

- `internal/config/defaults.go` now sets
  `Compression.Tuning.StructurePreview=false`.
- `DefaultTOML()` emits `structure_preview = false`.
- `internal/config/t55_defaults_test.go` now pins the T74 safety default.
- Full archive-backed preview remains useful follow-up work, but it is no
  longer a production blocker because the lossy path is not default-on.
- Final verification on 2026-04-29: `go run ./scripts/ci`,
  `go test -race ./...`, `bun test tests/ts`,
  `go test -tags=integration ./tests/integration`, and
  `go run ./scripts/benchmarks` passed.
