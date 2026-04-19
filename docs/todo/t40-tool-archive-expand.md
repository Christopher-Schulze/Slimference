# T40 - Large Tool-Result Archive + Explicit `expand`

Status: done
Priority: medium
Scope: `internal/toolarchive`, `internal/filter`, `internal/hooks`, `internal/analytics`, `internal/tui`, `cmd/slimference`
Source repo: `repos/token-optimizer`

---

## Problem

Slimference already compacts and truncates tool output well.

But there is still no general path for this workflow:

- a tool returns a very large result
- Slimference reduces what enters context
- later the user or model needs the full original result again
- the only option is to rerun the command or tool

That is wasteful for:

- large logs
- test and build output
- long MCP or CLI results

The foreign repo `repos/token-optimizer` has a useful answer here:

- persist the full large result locally
- expose only a compact preview in-context
- allow later recovery through an explicit retrieve path

---

## Reality-Checked Extraction from the Foreign Repo

Relevant upstream references:

- `repos/token-optimizer/skills/token-optimizer/scripts/archive_result.py`
- `repos/token-optimizer/skills/token-optimizer/scripts/measure.py`

Ideas worth importing:

1. archive only large results
2. keep a short preview in context
3. make the full result retrievable later by stable ID
4. keep the store local, deterministic, and bounded

Ideas explicitly not imported:

1. Python control-plane glue
2. automatic model-side `expand` assumptions tied to Claude-specific behavior
3. broad savings-event ecosystem around the archive

---

## Why This Could Matter for Slimference

This is a real but secondary feature.

It matters most when:

- a huge result has already been compacted away
- rerunning the tool would be slow, expensive, or nondeterministic
- the exact original bytes still matter

Compared with T39:

- lower leverage
- smaller architectural footprint
- best value after a continuity layer exists

So the right priority is:

- worth doing
- but behind Smart Compaction / checkpoints

---

## Scope Boundaries

In scope:

- local archival of oversized tool results
- explicit retrieval by ID
- integration with the existing `slimference posttool` path
- visibility in CLI/admin/TUI

Out of scope:

- full automatic memory system
- silent replay into proxied requests by default
- indefinite retention with no limits
- archiving every single tool result

---

## Best Integration Shape for Slimference

This must fit the repo we actually have.

The best insertion point is not a separate sidecar or daemon.
It is the already-existing:

- `slimference posttool`

Why:

- the current Codex PostToolUse flow already pipes hook JSON into `posttool`
- that command already extracts command + tool output and emits bounded
  `additionalContext`
- the archive feature can extend this existing path instead of inventing a
  parallel transport layer

That means the correct architecture is:

1. extend the hook payload parser to capture optional metadata:
   - `tool_name`
   - `tool_use_id`
   - `session_id`
   - existing `command`
   - existing `tool_response`
2. archive eligible large outputs before final context emission
3. emit a compact preview plus archive reference
4. retrieve later through a dedicated CLI command

---

## Desired End State

### Phase 1 - Deterministic archive store

- large tool results can be stored locally under Slimference-owned paths
- every archived result has stable metadata and a retrievable ID
- retention and size bounds exist from day one

### Phase 2 - PostTool integration

- `slimference posttool` can archive oversized outputs when eligible
- `additionalContext` includes only a short preview plus archive reference
- existing posttool compaction behavior remains the default fallback

### Phase 3 - Explicit retrieval

- `slimference expand <id>` returns the full archived body
- list/stats/gc surfaces exist for operator use

### Phase 4 - Visibility

- admin/TUI show archive counts, bytes, and recent archival activity
- analytics track archive hits and expansions

---

## Proposed Slimference Shape

### New package family

- `internal/toolarchive`
- `internal/toolarchive/store.go`
- `internal/toolarchive/types.go`
- `internal/toolarchive/eligibility.go`
- `internal/toolarchive/format.go`

Optional later:

- `internal/toolarchive/gc.go`
- `internal/toolarchive/stats.go`

### Storage path

- `~/.slimference/tool-archive/`

Per archived item:

- metadata
- full response body
- optional manifest / index row

### Stable identifier

Preferred rule:

1. use upstream `tool_use_id` when present
2. otherwise generate a Slimference archive ID from deterministic metadata

Recommended URI namespace:

- `slim://tool/{id}`

### Retrieval CLI

Primary user-facing command:

- `slimference expand <id>`

Possible supporting commands:

- `slimference archive list`
- `slimference archive stats`
- `slimference archive gc`

`expand` should stay first-class because it is the shortest path for the user
and for later agent/tool integration.

### Eligibility policy

Do not archive everything.

First-pass archive only when all are true:

- output exceeds a hard size threshold
- output is from a tool/result type where later recall is plausible
- storing it is more useful than the compacted fallback alone

Good first-pass targets:

- build output
- test output
- long logs
- long MCP/CLI outputs

Poor first-pass targets:

- tiny outputs
- obviously redundant or already-minimal results

### PostTool output contract

For an archived result, the emitted `additionalContext` should contain:

- a short, deterministic preview
- archive ID / URI
- a concise hint that full output can be retrieved later

It must stay shorter than the original output, otherwise Slimference should
fall back to current compaction logic.

---

## Implementation Plan

### WP1 - Archive data model and local store

- define archive item and metadata schema
- add bounded local storage
- add retention and disk limits

### WP2 - Hook payload enrichment

- extend posttool payload parsing to extract optional metadata fields
- preserve backwards compatibility with today's payload shape

### WP3 - Archive eligibility and formatting

- add deterministic archive gating
- define preview format and archive hint format
- prefer existing command-aware compaction when archiving is not eligible

### WP4 - `posttool` integration

Completed implementation:

- `internal/toolarchive` now persists large tool results under
  `~/.slimference/tool-archive/`
- archive IDs use upstream `tool_use_id` when present and otherwise fall back
  to deterministic Slimference IDs
- `slimference posttool` now archives eligible large outputs when real hook
  metadata is present, while preserving the old compaction path as the fallback
- `slimference expand <id>` is implemented and restores archived output locally
- admin/TUI now expose archive counts, bytes, and expansion activity

Scope reality:

- this currently lands on the existing Codex/CLI `posttool` surface
- no speculative sidecar or full automatic replay layer was added

Verification:

- `go test ./internal/toolarchive ./internal/filter ./cmd/slimference ./internal/proxy`
- `go test ./...`

- wire archive creation into `slimference posttool`
- keep current compaction as fallback
- do not break existing Codex PostTool behavior

### WP5 - Retrieval commands

- add `slimference expand <id>`
- add list/stats/gc commands as needed for operability

### WP6 - Visibility and analytics

- archive count
- total bytes
- archived items created
- expansion count

### WP7 - Tests

- archive store round-trips
- payload parsing with and without optional metadata
- posttool archive path
- posttool fallback path
- expand retrieval
- retention / gc

---

## Subtasks

- [x] Define archive item schema and ID rules.
- [x] Add `internal/toolarchive` package with deterministic local storage.
- [x] Extend posttool payload parsing for optional tool metadata.
- [x] Implement archive eligibility logic and compact preview formatting.
- [x] Integrate archive creation into `slimference posttool`.
- [x] Add `slimference expand <id>`.
- [x] Add archive list/stats/gc support.
- [x] Expose archive metrics in admin/TUI surfaces.
- [x] Add targeted tests and keep `go test ./...` green.

---

## Risks

- unbounded disk growth if retention is weak
- storing sensitive output too eagerly
- archive references that are longer than the benefit they provide
- breaking existing posttool compaction behavior

Mitigations:

- hard size caps
- retention / gc from first pass
- eligibility gating
- fallback to current posttool compaction when archiving is not net-positive

---

## Acceptance Criteria

- [x] Slimference can persist oversized tool results locally with stable IDs.
- [x] `slimference posttool` can emit a compact preview plus archive reference.
- [x] `slimference expand <id>` retrieves the archived full result without rerunning the tool.
- [x] Archive storage is bounded and inspectable.
- [x] Existing posttool compaction behavior still works when archiving is skipped.
- [x] Admin/TUI surfaces expose archive activity and size.

---

## Current Recommendation

This is a real, useful import from `repos/token-optimizer`, but it is not the
highest-value remaining one.

Recommended order:

1. T39 Smart Compaction / checkpoints
2. T40 tool-result archive + `expand`
