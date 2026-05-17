# TASK 186: Quality A/B harness for gated output-reduce profiles

Status: TODO (planning 2026-05-16; prerequisite for T169)
Priority: P1 (unblocks T169 and any future gated lever)
Scope: new `internal/qualityab/`, hooks in `internal/proxy/handler.go`

## Why

T169 (Be-Terse system-prompt hint) is the largest single-knob output-token saving available (15-30%) but carries real quality risk: a too-aggressive brevity instruction can amputate genuinely useful content. Shipping it default-on without a harness is irresponsible. We need:

1. **Cohort routing**: deterministically assign every session to treatment or control based on a stable hash.
2. **Outcome tracking**: count upstream failures (HTTP 4xx/5xx), retries (T148 repair signals), output tokens per cohort.
3. **Auto-rollback**: when treatment cohort's failure rate exceeds control's by more than a configurable delta (default 5 pp), turn the lever off for the rest of the process lifetime.
4. **Admin surface**: per-cohort metrics + rollback state in /admin/status.

The harness must be a reusable substrate so future risky levers (per-tool output budgets, aggressive structure extraction) can plug in without re-inventing cohort routing.

**Why:** Quality-A/B is the load-bearing infrastructure for any "ship default-on but reversible" feature. Without it, T169 stays off forever.
**How to apply:** A small package that owns cohort assignment + outcome counters + rollback decision. Handler.go consults it at one site (cohort decision) and reports at another (outcome).

## Target State

1. New `internal/qualityab/`:
   - `Harness` struct: cohort hash function, atomic counters per cohort, rollback flag.
   - `Cohort(sessionID string) Cohort` → returns "control" or "treatment"; "control" when harness disabled.
   - `RecordOutcome(cohort Cohort, outcome Outcome)`: outcome is one of Success / UpstreamError / RetryRequested.
   - `ShouldRollback() bool`: snapshot-based decision; recomputed each call.
   - `Snapshot() QualityABTelemetry`: per-cohort counters + rollback state.
2. Cohort assignment uses FNV-64 of sessionID mod 2 so the same session always lands in the same cohort.
3. Rollback fires when:
   - Treatment count ≥ MinSamples (default 50)
   - treatment.failureRate - control.failureRate > FailureDelta (default 0.05)
   - One-way latch: once rolled back, stays rolled back for the process lifetime.
4. Telemetry under `/admin/status.quality_ab` (per-cohort counts + rollback bool).
5. Tests: cohort stability under multiple session IDs, rollback fires at threshold, race-detector clean.

## Acceptance

- A session ID routes to the same cohort across N requests.
- 50 treatment requests, all failing; control ≥ 50 with low failure → rollback fires.
- 50 treatment requests, all succeeding → no rollback.
- Disabled harness puts everyone in control.
- Counters JSON shape stable in admin response.
- 100% coverage on `internal/qualityab/`.

## Sub-Tasks

- [ ] Harness struct + Cohort enum.
- [ ] Hash-based cohort assignment (FNV-64 mod 2).
- [ ] Atomic outcome counters per cohort.
- [ ] Rollback decision (atomic latch).
- [ ] Snapshot accessor for admin.
- [ ] Tests: stability, rollback, race-clean.

## Notes

- v1 is a process-lifetime harness. Persistent rollback (carried across restarts) is a follow-up.
- Per-model or per-task-shape cohort buckets are out of scope; v1 is whole-session.

## Deviations

(none yet)
