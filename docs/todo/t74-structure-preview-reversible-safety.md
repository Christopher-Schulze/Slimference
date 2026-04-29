# T74 - Structure-Preview Reversible Safety

Status: todo
Priority: P1
Scope: `internal/compression/preview*.go`, `internal/toolarchive/`, `cmd/slimference/checkpoint_cmd.go`, `internal/proxy/admin.go`, `docs/todo/t55-structure-preview-default-on.md`, `docs/documentation.md`
Driver: Structure preview is default-on but its original T55 reversibility criteria remain open.

---

## Problem

`StructurePreview` is enabled by default. The pass replaces large tool-result
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
   - Preview text includes `slim://archive/<id>` and `slimference expand <id>`.
   - Expand returns the exact original bytes.
   - Admin/TUI expose preview count and expand count.
2. **Fallback:** preview default is off until reversible.
   - `structure_preview = false` by default.
   - Docs explain how to opt in.

No default-on lossy preview without recovery.

## Implementation Plan

### WP1 - Choose default policy
- If T74 can be implemented quickly, keep default-on and add archive recovery.
- If not, flip default off first and re-open reversible preview as follow-up.

### WP2 - Archive integration
- Extend preview pass with an archive writer abstraction.
- Archive before replacement.
- Include enough metadata: provider, request ID when available, tool name,
  tool result ID, message index, source `structure_preview`.
- Handle archive failure by skipping preview, not by dropping content.

### WP3 - Preview text format
- Keep preview useful even without expansion.
- Add explicit recovery line:
  `Full output archived: slim://archive/<id> (run slimference expand <id>)`
- Preserve existing shape summary below it.

### WP4 - Telemetry
- Count preview applied/skipped/archive-failed.
- Count expand invocations already tracked by toolarchive.
- Add admin status fields and docs.

### WP5 - Tests
- End-to-end: long tool result -> preview contains archive ID -> expand returns
  exact original.
- Archive failure -> original text remains unchanged.
- Default config test matches chosen policy.

## Acceptance Criteria

- [ ] Default-on preview has an archive ID and `slimference expand` recovery.
- [ ] Archive failure never causes lossy preview.
- [ ] `slimference expand <id>` returns exact original output.
- [ ] Admin status exposes preview/expand counters or docs explain why not.
- [ ] If reversibility is not implemented, default is off and docs are updated.
- [ ] `go test -race ./internal/compression/... ./internal/toolarchive/... ./cmd/slimference/...` green.

## Out of Scope

- Remote archive sync.
- Automatic model-triggered expansion.
- Per-provider preview learning.

## Validation

```
go test -race ./internal/compression/... ./internal/toolarchive/... ./cmd/slimference/...
slimference expand <id>
curl -s 127.0.0.1:8990/_slimference/admin/status | jq .tool_archive
```
