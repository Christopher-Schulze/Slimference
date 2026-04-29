# TASK 92: Per-bullet lineage markers

Status: todo
Priority: P2
Scope: `internal/summarization/minimax.go`, `internal/summarization/validator.go`
Driver: After Layer 2 summarisation, bullets lose their connection to the original messages. If the model later asks "what did the user say in message 7?", Slimference cannot re-inject the original. T76 archive layer needs a back-reference path; lineage markers provide it.

---

## Problem

The current bullet format is `- <fact>`. There is no way to know which input message a given bullet came from. When the model references a missing block ("the file you mentioned earlier") the proxy cannot map it back to a message index without re-running the summary in reverse, which is not feasible.

## Target State

Bullets gain an optional trailing `[msg:N,M]` marker that lists the original message indices the bullet was extracted from. The marker is stripped by the validator on the operator-facing TUI/CLI views but kept in the on-the-wire body so the model + the proxy itself can use it for lineage.

Format example:
```
- src/auth/handler.go contains HandleLogin() - needs token validation [msg:1,2]
- go test ./src/auth/... passed (0.012s) [msg:8]
```

## Implementation Plan

### WP1 - Prompt update (T86 prompt template)
- Add a rule to the active prompt: "End every bullet with `[msg:N]` or `[msg:N,M,...]` where N/M are the indices that produced the fact. Do not omit the marker."

### WP2 - Validator
- Accept bullets with or without markers (gradual rollout).
- Counter `bullet_marker_present_rate` so the operator can see prompt compliance.

### WP3 - Proxy use of markers
- T76 re-injection consumes the markers to know which original message to pull from the archive.

### WP4 - Display layer
- `slimference debug last`, TUI views, and `slimference savings` strip markers when rendering for a human.

## Acceptance Criteria

- [ ] Active prompt requests markers.
- [ ] Validator tolerates absence (no regression) and counts presence rate.
- [ ] T76 re-injection uses markers when present.
- [ ] Human-facing views render bullets without markers.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Forcing markers via post-hoc rewriting; rely on prompt compliance and gradual roll-up of the metric.
- Cross-session lineage (markers are per summary).

## Validation

```
go test ./internal/summarization/...
```
