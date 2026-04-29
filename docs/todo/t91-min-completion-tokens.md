# TASK 91: Min-completion-tokens to reduce false validator rejects

Status: todo
Priority: P2
Scope: `internal/summarization/minimax.go`, `internal/types/`, `internal/config/`
Driver: `MaxTokens=targetTokens` is a cap. The model can stop early mid-bullet when budget is tight, producing a truncated output that the validator rejects. Several providers expose a min-completion-tokens or stop-condition parameter that prevents premature stops.

---

## Problem

When `targetTokens` is small (e.g. 200 because of T54 latency budget) the model may stop after 90 tokens with the last bullet incomplete. The validator rightly rejects it and the summary is lost. There is no signal in the request that says "do not stop until you reach at least N tokens or hit a clean line break".

## Target State

When the active provider's capability map (T88) reports `supports_min_completion_tokens` (or an equivalent stop-condition field), the request builder sets:

- `min_completion_tokens = max(0.7 * target, 32)`
- `stop_on_clean_line_break = true` where supported

Otherwise the builder leaves the request unchanged.

The repair path (T90) and the validator path keep working as today; this just reduces how often they have to fire.

## Implementation Plan

### WP1 - Capability extension
- Extend the capability struct from T88 with `supports_min_completion_tokens`, `supports_stop_conditions`.

### WP2 - Request builder
- Add the fields to `mmRequest` when supported.
- Document defaults in `internal/config/defaults.go`.

### WP3 - Tests
- Snapshot: payload contains the new fields when capability is on.
- Snapshot: payload omits them when capability is off.

### WP4 - Counter
- `summary_premature_stop_total` increments when validator detects truncation; the repair pipeline (T90) drops this counter as it improves.

## Acceptance Criteria

- [ ] When supported, the request body carries the min-completion-tokens parameter.
- [ ] No-regression on providers that do not support it.
- [ ] Counter `summary_premature_stop_total` is exposed.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Picking a different `target` budget; that stays driven by adaptive window logic (T54, O2).

## Validation

```
go test ./internal/summarization/...
```
