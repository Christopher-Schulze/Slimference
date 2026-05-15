# TASK 153: Hierarchical context capsules

Status: DONE
Priority: P0
Parent: T144/T76
Scope: `internal/summarization/`, `internal/contentarchive/`, `internal/compression/`, `internal/debug/`, `cmd/slimference/expand*`

## Why

A single flat summary is too blunt. Large sessions contain repeated tool results, task phases, and old decisions with different risk profiles. Hierarchical capsules let Slimference shrink each level independently while preserving critical anchors and raw archive expansion.

## Target State

Context is represented as reversible capsules:

1. Micro-capsules for large tool results.
2. Phase capsules for coherent task slices.
3. Session capsules for old, low-risk history.
4. Anchors preserved verbatim: file edits, exact commands, failures, user decisions, blockers, and protected boundaries.
5. Each capsule carries archive IDs for expansion.
6. T149 chooses which capsule tier enters the request.

## Implementation Plan

### WP1 - Capsule schema

- [x] Define capsule type, source range, token accounting, anchor list, archive IDs, and validation state.
- [x] Store schema in Go structs with stable JSON fields for debug/export output.

### WP2 - Micro-capsules

- [x] Convert archive-backed large tool results to short structured summaries plus expansion references.
- [x] Use deterministic compaction only; defer L2 capsule rewriting to T149/T144 policy.

### WP3 - Phase capsules

- [x] Detect phase boundaries from user task/next/fix/plan markers and role clusters.
- [x] Collapse old phases only after they are outside the active tail.

### WP4 - Session capsules

- [x] Summarize very old prefixes from validated phase capsules.
- [x] Keep anchor-containing ranges uncapsuled so open blockers/edits/errors remain verbatim.

### WP5 - Expansion tooling

- [x] Reuse `slimference expand` through content-archive URIs carried by every capsule.
- [x] Never create a capsule that cannot be expanded or traced to source.

## Acceptance

- [x] Capsule schema is explicit and archive-backed.
- [x] Critical anchors remain verbatim.
- [x] Micro/phase/session tiers are independently selectable.
- [x] Capsule expansion retrieves original source content.
- [x] `go test ./...` passes.

## Implementation Notes

- `internal/summarization/capsules.go` adds `ContextCapsule`, `CapsuleTier`, `CapsuleBuildOptions`, `BuildContextCapsules`, and `CapsulesByTier`.
- Micro capsules archive large non-anchor tool results through `internal/contentarchive` and carry a deterministic structured summary plus expansion URI.
- Phase capsules split inactive ranges on task/next/fix/plan/user-boundary markers and skip any range containing anchor edits, errors, decisions, or config touches.
- Session capsules compose validated phase capsules only when enough old low-risk history exists; they carry both phase archive URIs and a session-prefix archive URI.

## Verification

- `go test ./internal/summarization -count=1`
- `go test ./...`
- `git diff --check`

## Non-Goals

- No repo-onboarding capsule.
- No lossy capsule without raw archive.
- No active edit-context capsule unless T149 allows it.
