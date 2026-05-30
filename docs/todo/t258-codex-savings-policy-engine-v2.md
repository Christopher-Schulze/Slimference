# TASK 258: Codex savings policy engine v2

Status: [ ] QUEUED - central auto policy for risk, recovery, recency, and workload class
Priority: P0 - turns aggressive mechanisms into safe automatic behavior
Scope: Extend `internal/savingspolicy` from mechanism toggles into a full Codex
autopilot. Applies to WSS first; HTTP promotion depends on T259.

## Why

T256 created the first correct shape: default `auto`, recoverability before chunk
dedup, and automatic loosening on recent edits or post-collapse re-reads. The final
target is stronger: every reducer must declare risk, recovery, recency sensitivity,
workload class, proof level, and fallback. Then the product can use aggressive savings
without manual toggles and without silently making the model worse.

## Acceptance

- Policy input includes: route, client type when known, workload class, mechanism risk
  level, recovery level, recency signal, recent-edit state, post-collapse re-read state,
  proof level, and session integrity budget.
- Policy output includes: allow, shadow-only, full-pass, required recovery note,
  telemetry reason, and promotion-block reason.
- Every default-auto reducer flows through this policy: read-delta, repeated-output,
  ranged-read, search-delta, chunk-dedup, and future T253/T254 candidates.
- No reducer uses ad-hoc direct config checks for product default behavior when a policy
  decision should own it.
- Policy records content-free counters for allow/shadow/full-pass decisions by
  mechanism, route, and reason.
- `auto` means evidence-backed activation, not "all knobs on"; `max` still requires
  recovery and fail-open constraints.

## Gates

- Unit gate: table-driven tests cover every mechanism x mode x route x risk decision.
- Integration gate: WSS Phase-F tests prove policy reasons for auto chunk-dedup,
  conservative opt-in, post-collapse full-pass, recent-edit full-pass, and shadow-only
  high-risk candidates.
- Proof gate: T257 evidence promotes only mechanisms whose capture class passed.
- Drawdown gate: policy never enables a first-read lossy/reconstructive reducer unless
  the route has recovery and the workload class has A/B proof.
- UX gate: TUI/admin status can explain "active", "shadow", "full-pass", and "blocked"
  without implying fake savings.
- Regression gate: full build/vet/test/CI green.

## Sub-Tasks

- [ ] Define policy enums and decision structs for risk, recovery, recency, workload,
      and proof level.
- [ ] Add policy evidence inputs from T257 reports and current session telemetry.
- [ ] Move remaining route/mechanism ad-hoc gates into `internal/savingspolicy`.
- [ ] Add shadow-only decision support for T253/T254 candidates.
- [ ] Surface policy reason counters in admin state, `wss-audit`, and workday reports.
- [ ] Document the auto/max/conservative/off semantics with exact safety guarantees.

## Notes

- This is the architecture answer to "why have a feature if it is off". Features are
  either auto-active when proven safe, shadow-only while proving, or removed if they
  cannot become safe.
- T258 should stay small and explicit. No clever hidden scoring until evidence proves
  thresholds.

## Deviations

(none)
