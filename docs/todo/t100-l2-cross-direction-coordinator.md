# TASK 100: Cross-direction L1/L2 coordinator

Status: DONE (2026-04-30) — coordinator decision-rule wired in handler.go, L1 honours plan via SetCoordinatorSubsume, cheap passes preserved, telemetry exposed. Default off via coordinator_enabled.
Priority: P2
Scope: `internal/compression/layer1.go`, `internal/summarization/layer2.go`, `internal/proxy/handler.go`
Driver: Layer 1 does aggressive work that Layer 2 then subsumes when it summarises the same exchanges. The two layers do not coordinate. A coordinator that knows L2 is about to summarise an exchange can skip the L1 spend on that exchange.

---

## Problem

Pipeline runs L1 over every message, then L2 over the older window. For exchanges that L2 will collapse anyway, L1's per-block dedup, structure-extract, and tool-compressor cost is wasted. T76 makes the lever real because skipping L1 is no longer destructive when L2 archives the original.

## Target State

A coordinator decides per-exchange whether to:

- Run L1 fully (default for the live window).
- Skip aggressive L1 sub-layers when L2 will summarise the exchange.
- Always run cheap, idempotent L1 sub-layers (ANSI strip, JSON compact) regardless.

The decision uses the L2 window plan as input: if the exchange is in the to-be-summarised window, L1 only runs cheap passes.

## Implementation Plan

### WP1 - Plan struct
- L2 produces a `WindowPlan{exchanges_to_summarise []int}` before L1 runs.

### WP2 - L1 selector
- L1 reads the plan; for each exchange, picks the sub-layer set.

### WP3 - Always-on cheap passes
- Defined list: ANSI strip, JSON compact (and any others that have no archive cost).

### WP4 - Telemetry
- `coord_l1_skipped_total`, `coord_l1_full_total`.

### WP5 - Tests
- Long-session fixture: assert L1 work is skipped on the window L2 will summarise, not on the live window.

## Acceptance Criteria

- [x] L2 produces a plan before L1 runs (handler.go:154-157 checks conditions).
- [x] L1 honours the plan and skips heavy sub-layers on summarised exchanges.
- [x] Cheap passes always run (ANSI strip, JSON compact).
- [x] Coverage 100%; race tests green.
- [ ] **Tracked as T100b** (separate task): No quality regression measured by T77 signals. Requires a soak window with the flag on against real traffic; not measurable in unit tests.

## Out of Scope

- Reordering layers entirely.
- Predicting future windows.

## Validation

```
go test ./internal/compression/... ./internal/summarization/... ./internal/proxy/...
```
