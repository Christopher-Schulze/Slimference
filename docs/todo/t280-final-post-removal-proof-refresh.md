# TASK 280: Final post-removal proof refresh

## Why

After the semantic Layer 2 path is removed, the proof stack must be refreshed
against the active layers only. Release evidence should not accidentally count
removed summary/OCRL fixtures, metadata, validators, or savings fields.

## Acceptance

- `go test ./...` passes after semantic Layer 2 removal.
- `go run ./scripts/ci` passes after semantic Layer 2 removal.
- Benchmark and live-corpus reports list only active layers and active workload
  classes.
- `release-proof-report` fails closed unless fresh CLI and Desktop resource
  bundles are supplied; absence of those bundles is tracked as live-only proof
  work, not an implementation gap.
- Any remaining live-only evidence gaps are explicitly tracked as proof gaps,
  not implementation gaps.

## Sub-Tasks

- [x] Re-run checked-in benchmark-corpus gates.
- [x] Re-run release-proof report preflight and confirm it fails closed without
  required CLI/Desktop resource bundles.
- [x] Update docs for active-layer-only proof/report wording.

## Notes

- Completed after T279/T281/T282.
- `go test ./...` passed after T282.
- `go run ./scripts/ci` passed after T282: all 8 steps, coverage 96.6%, live
  corpus PASS with 55 requests and 42.07% known-denominator ratio.
- `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
  --promotion-check` passed: 54 real sessions, codex_cli=37,
  codex_desktop=17.
- `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
  --maxx-check` passed with the same 54 real sessions and the maxx workload
  matrix present.
- `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus
  --maxx-check --json | rg ...` found no removed Layer 2/OCRL/MiniMax/context
  ledger report fields.
- `go run ./scripts/utils release-proof-report ... --json` fails closed without
  `--resource-profile-proof` bundles and reports the missing CLI/Desktop bundle
  requirement.
- Superseded by T296 on 2026-06-06: the current final release-proof refresh
  passed with a fresh clean matrix and both CLI/Desktop resource bundles. T280
  remains historical evidence for the fail-closed preflight, not the current
  proof state.

## Deviations

- None.
