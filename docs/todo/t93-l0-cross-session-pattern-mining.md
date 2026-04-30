# TASK 93: Layer 0 cross-session pattern mining

Status: deferred - see docs/todo.md for closure rationale
Priority: P2
Scope: `internal/filter/`, `internal/sessions/`, `cmd/slimference/`
Driver: Repeated identical commands (`git status`, `npm test`, `pytest`) produce the same output across runs in the same session and across sessions. Today every run is filtered fresh. From run #3 of an unchanged tool with unchanged output, a pointer marker (`see msg #N`) replaces the body and saves another 30-50% on tool-heavy workflows.

---

## Problem

Layer 0 filters reduce the size of *individual* tool outputs but do nothing about repetition. A typical debug loop runs `git status` ten times. Layer 0 compacts each run; nothing dedupes the run-vs-run identity.

## Target State

A new sub-stage of Layer 0 (after the existing built-ins, before the TOML pipeline) checks the compacted output against a per-session cache:

- Key: `(tool, args, project_path, compacted_output_sha)`.
- On a hit (counter >= `[filter.repetition] threshold`, default 3), replace the output with `[git status] same as msg #N`.
- TTL: 5 minutes idle or 50 messages since last hit.

Persisted across daemon restarts via `filter.db`.

## Implementation Plan

### WP1 - Repetition store
- New `internal/filter/repetition.go` with the cache and a thin SQLite layer.

### WP2 - Filter pipeline integration
- After existing built-ins, the repetition stage decides whether to replace.

### WP3 - Reverse path
- T76 archive layer records the original output once so the model can ask for it; the marker contains the archive id.

### WP4 - Telemetry
- Counter `filter_repetition_hits_total`, savings attribution under `Layer 0` in `RequestSummary`.

### WP5 - Tests + docs
- Unit tests with synthetic repeated outputs.
- Doc snippet in `docs/integration.md`.

## Acceptance Criteria

- [ ] After 3 identical runs of a tool, run 4 emits the marker.
- [ ] Marker carries an archive id so reverse path works.
- [ ] Counter is surfaced and savings are attributed correctly.
- [ ] No regression on first-run outputs (which must always pass through filters as today).
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Approximate-match repetition (only exact post-compact hash for v1).
- Cross-machine sharing.

## Validation

```
go test ./internal/filter/...
slimference savings today --by-layer
```
