# TASK 258: Codex savings policy engine v2

Status: [~] IN PROGRESS - central auto policy foundation active for route, risk,
recovery, recency, workload, proof, and closed-candidate blocking
Priority: P0 - turns aggressive mechanisms into safe automatic behavior
Scope: Extend `internal/savingspolicy` from mechanism toggles into a full Codex
autopilot. Applies to WSS first; HTTP archive refs are permanently blocked by T259.

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
- Policy output includes: allow, shadow, full-pass, required recovery note,
  telemetry reason, and promotion-block reason.
- Every default-auto reducer flows through this policy: read-delta, repeated-output,
  ranged-read, search-delta, chunk-dedup, and closed-candidate block telemetry.
- No reducer uses ad-hoc direct config checks for product default behavior when a policy
  decision should own it.
- Policy records content-free counters for allow/shadow/full-pass decisions by
  mechanism, route, and reason.
- `auto` means evidence-backed activation, not "all knobs on"; `max` still requires
  recovery and fail-open constraints.

## Gates

- Unit gate: table-driven tests cover every mechanism x mode x route x risk decision.
- Integration gate: WSS Phase-F tests prove policy reasons for auto chunk-dedup,
  conservative opt-in, post-collapse full-pass, recent-edit full-pass, and blocked or
  telemetry-only high-risk candidates.
- Proof gate: T257 evidence promotes only mechanisms whose capture class passed.
- Drawdown gate: policy never enables a first-read lossy/reconstructive reducer unless
  the route has recovery and the workload class has A/B proof.
- UX gate: TUI/admin status can explain "active", "shadow", "full-pass", and "blocked"
  without implying fake savings.
- Regression gate: full build/vet/test/CI green.

## Sub-Tasks

- [x] Define policy enums and decision structs for risk, recovery, recency, workload,
      and proof level.
- [~] Add policy evidence inputs from T257 reports and current session telemetry.
- [~] Move remaining route/mechanism ad-hoc gates into `internal/savingspolicy`.
- [x] Add decision support for closed T253/T254 candidates: no default mutation, with
      server-state mirror retained as telemetry only.
- [x] Surface policy reason counters in admin state, `wss-audit`, and workday reports
      (`/admin/state`, `aggregate-savings`, `workday-savings`, and optional
      `wss-audit --admin-state-file` join done).
- [x] Document the auto/max/conservative/off semantics with exact safety guarantees.

## Notes

- This is the architecture answer to "why have a feature if it is off". Features are
  either auto-active when proven safe, telemetry-only when they do not touch model
  context, or removed/closed if they cannot become safe.
- T258 should stay small and explicit. No clever hidden scoring until evidence proves
  thresholds.
- 2026-05-31: `internal/savingspolicy` now has typed route/client/workload/risk/
  recovery/proof/mechanism/action enums and `DecideCodexMechanism`. The WSS/HTTP
  Layer-0 reducer passes route and workload into policy. Proven lossless mechanisms are
  allowed; WSS recoverable chunk-dedup is allowed only with archive recovery; HTTP
  archive refs are blocked with reason `http_archive_recovery_unproven`; recent edits,
  post-collapse re-reads, and session-integrity budget hits full-pass. First-read
  elision, predictive post-edit, apply_patch context dedup, reasoning compaction, and
  generalized server-state-mirror mutation are closed as product-default candidates.
  The server-state mirror remains telemetry/policy infrastructure only.
- 2026-05-31: `/admin/state`, `aggregate-savings`, `workday-savings`, and optional
  `wss-audit --admin-state-file` savings telemetry now include content-free
  `proxy_layer0_policy` counters keyed by route, mechanism, action, reason, block reason,
  and count.

## Deviations

(none)
