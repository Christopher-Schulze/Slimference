# TASK 92: Per-bullet lineage markers

Status: completed (T76 WP3 re-injection consumer deferred)
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

## Closure Notes (2026-04-30)

Landed:

- System prompt instructs the model to end every bullet with
  `[msg:N]` or `[msg:N,M,...]`. The example block now demonstrates the
  marker format on every bullet so the few-shot signal is unambiguous.
- `hasLineageMarker(line)` and `StripLineageMarker(line)` helpers cover
  detection and human-display stripping.
- `RecordLineageStats(summary)` is called at the end of
  `cleanSummaryOutput` so every successful summary feeds the
  `lineage_marker_rate` telemetry. `LineageMarkerCounts()` and
  `LineageMarkerRate()` expose the values; `ResetLineageMarkerStats()`
  is the test helper.
- Validator already tolerates trailing content (it only checks for the
  `- ` prefix), so no validator change was required.

Deferred:

- T76 WP3 (opportunistic re-injection) is the consumer of these markers.
  Until WP3 lands, markers are recorded in the summary but nothing reads
  them at the proxy level. The marker stays in the on-the-wire summary
  so the model itself can use it ("see msg #3 above") without proxy
  involvement.
- `/admin/status.summarization.lineage` endpoint surface (counters
  exist, just not yet exposed).
