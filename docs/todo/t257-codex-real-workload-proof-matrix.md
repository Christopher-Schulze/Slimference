# TASK 257: Codex real-workload proof matrix

Status: [~] ACTIVE - proof tooling built; live CLI/Desktop captures remain
Priority: P0 - required before claiming max savings with no model/workflow drawdown
Scope: Codex CLI and Codex Desktop through scoped WSS Phase-F. Build a repeatable
capture, replay, and workday-proof matrix for all default-auto savings mechanisms.

## Why

Single-session proofs are necessary but not enough for the user's bar. T249 proves the
A/B harness, T255/T256 prove recoverable chunk dedup on one real capture, and the
daemon/route gates are healthy. The remaining gap is breadth: multiple real workloads,
both clients, and long-session behavior. This task turns "savings-proven in a case" into
"default-auto is safe across representative Codex work".

## Acceptance

- At least 10 real scoped WSS captures are collected locally: 5 CLI, 5 Desktop.
- Captures cover: repeated full-file reads, similar files, changed files, ranged reads,
  search/rg loops, git status/diff loops, build/test/lint failures, apply_patch then
  read, long mixed workday, and one no-savings small-chat/control workload.
- Every capture has a metadata row: client, Codex version, Slimference commit, repo,
  workload class, expected reducer candidates, start/end timestamps, route mode,
  model, and whether the session was CLI or Desktop.
- Every capture runs through `wss-ab-replay --fail-on-lost --json` and records
  `gate_passed`, `bytes_saved`, expected extras, unexpected losses, mutation count,
  and route counters.
- Workday ceremony runs at least twice: one CLI-heavy window and one Desktop-heavy
  window using `workday-savings start|finish` after sessions are closed.
- PASS requires: `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  no unexpected A/B losses, no unexplained re-read canary spike, and positive net
  billable input savings on workloads with expected redundancy.

## Gates

- Capture hygiene: no auth headers, no WebSocket upgrade metadata, no committed private
  raw payloads. Captures stay local unless scrubbed and explicitly moved into fixtures.
- Replay gate: all non-control captures pass `wss-ab-replay --fail-on-lost --json`.
- Route gate: `phasef_bridged>0` for each live capture, no byte-bridge fallback unless
  that workload is intentionally a fallback control.
- Savings gate: at least 7 of 10 captures show positive billable input savings or are
  documented as expected-zero controls.
- Drawdown gate: no capture shows unexpected elision, recovery-loop churn, repeated
  post-collapse re-read after policy loosening, or workflow-visible Codex failure.
- Regression gate: `go build ./... && go vet ./... && go test ./... && go run
  ./scripts/ci` green after any tool/reporting changes.

## Sub-Tasks

- [x] Add/extend a capture metadata schema and local report command if current
      `wss-audit`/`wss-ab-replay` output cannot summarize all required gates.
- [ ] Run 5 CLI captures and replay them.
- [ ] Run 5 Desktop captures and replay them.
- [ ] Run CLI and Desktop workday windows.
- [ ] Produce a content-free proof table in `docs/operation-log.md` and summarize
      default-auto readiness in `docs/documentation.md`.

## Notes

- This task does not invent new reducers. It decides which existing and next reducers
  are safe to promote through T258.
- Captures are local evidence. Only scrubbed metadata and aggregate proof numbers go
  into the repository.
- This is the next blocker for any stronger "comprehension-preserved" claim.
- 2026-05-30: Added `go run ./scripts/utils wss-proof-matrix <captures.jsonl>
  [--json]`. The metadata schema is one JSONL row per local capture with:
  `id`, `client`, `workload_class`, `frames_path`, optional `decisions_path`,
  `codex_version`, `slimference_commit`, `repo`, `model`, timestamps,
  `expected_reducers`, and `expected_zero_savings`. The command runs every frame
  file through the existing A/B replay gate, optionally audits the matching
  decisions log, verifies 5 CLI + 5 Desktop coverage, verifies all required
  workload classes, enforces per-capture gates, and checks the 7/10
  positive-or-expected-zero savings gate.

## Deviations

(none)
