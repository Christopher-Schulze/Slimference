# TASK T296: Release proof refresh and docs alignment

## Why

The implementation is route-ready, but release-grade claims must stay tied to
fresh proof output. We need a current autonomous proof pass over the checked-in
corpus, local capture inventory, resource bundles, install/status surfaces, and
final CI, then the docs must reflect exactly what is proven versus what still
needs an interactive Codex Desktop/App run.

## Acceptance

- Checked-in corpus passes the normal, promotion, and maxx gates.
- Local capture inventory and release-proof report are run against content-free
  matrix rows and available resource bundles.
- Install/status/savings surfaces are sampled without enabling persistent Codex
  routing.
- `documentation.md` and task docs describe only measured, current claims.
- Any remaining live-only proof is listed as a precise follow-up prompt or
  blocked condition, not as vague future work.
- Final tests and `go run ./scripts/ci` pass.

## Sub-Tasks

- [x] Run autonomous corpus, inventory, status, savings, and resource proof
  commands.
- [x] Fill any missing proof bundle or live-only prompt gaps.
- [x] Align docs with current proof state.
- [x] Run final verification gates.
- [x] Commit the completed task.

## Notes

- Scope is proof, release-claim hygiene, and docs alignment. It must not enable
  persistent shared Codex routing.
- Corpus gates:
  - `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
    --check` passed.
  - `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
    --promotion-check` passed with 54 real sessions, `codex_cli=37`,
    `codex_desktop=17`.
  - `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
    --maxx-check` passed with the same 54 real sessions and maxx workload
    breadth present.
- Inventory:
  - `go run ./scripts/utils wss-proof-inventory ~/.slimference/captures --json`
    reported 89 rows across 24 matrix files, CLI=70, Desktop=19, all maxx
    workload classes complete, `positive_token_rows=60`,
    `host_budget_ok_rows=27`, and `safety_issue_rows=0`.
- Clean release proof:
  - `go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures
    /tmp/slimference-release-proof-t296-clean-matrix.jsonl --json` wrote 70
    clean release rows from 89 local proof rows.
  - `go run ./scripts/utils release-proof-report
    /tmp/slimference-release-proof-t296-clean-matrix.jsonl
    --resource-profile-proof
    ~/.slimference/captures/host-resource-codex_cli-auto-20260604T212018Z
    --resource-profile-proof
    ~/.slimference/captures/host-resource-codex_desktop-20260604T212111Z
    --json` passed with `gate_passed=true`,
    `resource_profile_proof_ok=true`,
    `local_billable_input_tokens_saved=330518`,
    `provider_cache_read_tokens=430720`, `tool_prune_tokens_saved=26`,
    `output_reduce_injected_turns=2`, `host_budget_issue_rows=0`,
    `proof_event_loss_rows=0`, and `safety_issue_rows=0`.
- Status/savings sampling:
  - `slimference status --preflight` showed normal Codex direct,
    advanced shared route off, no global listeners, daemon healthy, WSS
    certified.
  - `slimference codex status` showed `enabled=false`, daemon reachable, and
    WSS recert status passed.
  - `slimference savings today` reported 267.4K decision-log net saved tokens
    for the current local day.
- No missing proof bundle remains for the current release-corpus claim. Broader
  market-average or new workload claims still need matching future proof rows.
- Updated `docs/documentation.md`, `docs/benchmarks.md`,
  `docs/savings-assessment.md`, `docs/operation-log.md`, and the historical
  T280 note.
- Verification:
  - `go test ./docs`
  - `go test ./scripts/benchmarks ./scripts/utils`
  - `go run ./scripts/ci` passed all 8 steps with total coverage 96.5%.

## Deviations

- None.
