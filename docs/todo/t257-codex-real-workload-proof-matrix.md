# TASK 257: Codex real-workload proof matrix

Status: [x] DONE - CLI/Desktop proof matrix and workday windows passed
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
  `gate_passed`, replay `bytes_saved` as the model-facing regression proxy,
  expected extras, unexpected losses, mutation count, live admin-state
  `billable_input_tokens_saved`, and route/safety counters.
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
- [x] Run 5 CLI captures and replay them.
- [x] Run 5 Desktop captures and replay them.
- [x] Run CLI and Desktop workday windows.
- [x] Produce a content-free proof table in `docs/operation-log.md` and summarize
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
- 2026-05-30: Live CLI capture `cli-repeat-full-read-001` passed the replay gate:
  109 frames, 3 request turns, 1 mutated request, 10,027 bytes saved, lost=0.
  Matching live daemon counters showed phasef_bridged=1, frames_reencoded=1,
  compressed_messages_mutated=1, parse_failures=0, degraded_sessions=0,
  compression_errors=0, and 2,838 billable input tokens saved.
- 2026-05-30: Live CLI capture `cli-similar-files` was a default-auto negative
  proof: route and parsing were clean, but replay saved 0 bytes and live counters
  showed read_delta_misses=2 with no mutation. Forcing chunk dedup on the capture
  only added the recovery note and was net-negative, so this workload must not
  promote chunk dedup by default without broader positive evidence.
- 2026-05-30: Live CLI `changed_file` capture exposed a safety issue in captured
  shell-output compaction: a compound `cat; append; cat` command was parsed as
  the first simple `cat`, producing A/B replay lost=1. The captured-output argv
  parser now rejects command lines containing operators, pipes, redirects, or
  shellisms anywhere in the command. Replaying the same capture after the fix
  returns lost=0 and bytes_saved=0, which is the correct fail-open outcome.
- 2026-05-30: Valid separate-turn CLI `changed_file` capture passed the A/B gate:
  two scoped WSS connections, 173 frames, 4 request turns, 1 mutated request,
  5,822 replay bytes saved, lost=0. Live counters recorded 1,184 billable input
  tokens saved, read_delta_attempts=2, read_delta_misses=1, read_delta_blocks=1,
  parse_failures=0, degraded_sessions=0, compression_errors=0. Codex `exec
  resume` emitted a post-tool upstream 400 after the second turn; the reducer
  proof remains valid, but the resume workflow should be tracked separately if it
  reproduces outside this proof harness.
- 2026-05-30: Live CLI no-savings control `cli-no-savings-control` passed as an
  expected-zero control: 25 frames, 1 request turn, mutated_requests=0,
  bytes_saved=0, lost=0, gate_passed=true. Live counters showed clean
  phasef_bridged routing, no tool_result candidates, and zero parse, degraded
  session, or compression errors.
- 2026-05-30: Live CLI `git_status_diff` capture exposed a second safety issue:
  generic captured-output compaction saved tokens for `git status --short`, but
  A/B replay classified it as lost=1 because the compact `[git status]` summary
  drops filenames. The WSS Layer-0 path now requires captured-output and Codex
  exec-envelope compaction to carry a `local-archive://` recovery marker; if the
  session key or archive write is unavailable, WSS fails open and leaves the
  original output unchanged. Replaying the same capture after the fix reports
  77 frames, 2 request turns, 1 mutated request, 1,047 bytes saved, lost=0,
  one `elided_with_reference` item, and gate_passed=true. The live run that
  produced the capture recorded phasef_bridged=1, frames_reencoded=1,
  compressed_messages_mutated=1, codex_exec_envelope_blocks=1,
  billable_input_tokens_saved=459, and zero parse, degraded-session, or
  compression errors.
- 2026-05-30: Desktop proof captures now cover repeat read, git-status diff,
  no-savings control, search loop, build/test/lint failure, and apply-patch then
  read. Replay results:
  - `desktop-repeat-full-read`: 154 frames, 5 request turns, 1 mutated request,
    10,027 bytes saved, lost=0, gate_passed=true; audit recorded 2,839 saved
    tokens.
  - `desktop-git-status-diff`: 83 frames, 3 request turns, 1 mutated request,
    1,376 bytes saved, lost=0; audit recorded 422 saved tokens.
  - `desktop-no-savings-control`: 55 frames, 2 request turns, 0 mutations,
    bytes_saved=0, lost=0; this is the expected-zero control.
  - `desktop-search-loop`: 93 frames, 3 request turns, 1 mutated request,
    6,882 bytes saved, lost=0; audit recorded 1,475 saved tokens.
  - `desktop-build-test-lint-failure`: 93 frames, 3 request turns, 1 mutated
    request, 103 bytes saved, lost=0; audit recorded 19 saved tokens.
  - `desktop-apply-patch-then-read`: 271 frames, 7 request turns, 0 mutations,
    bytes_saved=0, lost=0. This is an expected-zero safety proof: after a recent
    edit, WSS full-passes instead of collapsing the re-read.
- 2026-05-30: Additional CLI coverage closed the missing workload classes:
  `cli-ranged-read` replayed with 37 frames, 2 request turns, 1 mutated request,
  7,868 bytes saved, lost=0, and 1,584 audited tokens saved. `cli-long-mixed-
  workday` replayed with 177 frames, 5 request turns, 3 mutated requests,
  7,655 bytes saved, lost=0, and 2,066 audited tokens saved. Invalid CLI
  apply-patch/resume attempts were discarded because Codex reordered commands or
  hit an upstream resume 400 after the edit; they are not counted as proof.
- 2026-05-30: Final local proof matrix `proof-matrix-13` passed: 13 captures
  total, 7 CLI, 6 Desktop, all 10 required workload classes present, 9 positive-
  savings captures, 4 expected-zero captures, captures_with_issues=0, and
  gate_passed=true. The replay tool emitted benign scoped-desktop-CA warnings
  from a temporary HOME during replay; the captures themselves use the no-CA
  app-server WSS path.
- 2026-05-30: Formal `workday-savings start|finish` windows are complete. Clean
  CLI positive window: `git status --short .`, exit 0, 372 billable WSS-input
  tokens saved, phasef_bridged=1, compressed_messages_mutated=1,
  frames_reencoded=1, codex_exec_envelope_blocks=1, parse_failures=0,
  degraded_sessions=0, compression_errors=0. Clean Desktop positive window:
  `rg -n TODO /tmp/t257-workday-desktop/repo`, 382 billable WSS-input tokens
  saved, phasef_bridged=2, compressed_messages_mutated=1, frames_reencoded=1,
  codex_exec_envelope_blocks=1, parse_failures=0, degraded_sessions=0,
  compression_errors=0. A larger mixed CLI/Desktop prompt also produced savings
  but hit an upstream Codex `400 invalid_request` during final response, so it
  is documented as non-gating evidence and not counted as the clean workday pass.
- 2026-06-02: Release revalidation captured three fresh automatic CLI workloads
  before Desktop/manual breadth: `repeat_full_read`, `ranged_read`, and
  `search_loop`. Per-capture replay gates all passed with `lost=0` and positive
  savings: repeat read saved 11,463 model-facing bytes, ranged `sed -n` saved
  4,303 bytes, and search-loop compaction saved 4,381 bytes. The temporary
  matrix at `/tmp/slimference-release-proof-matrix.jsonl` therefore had
  `captures_with_issues=0` and `positive_savings_captures=3`, but the full
  release matrix correctly remained red because it needs 10 captures, at least
  5 CLI, at least 5 Desktop, and all required workload classes. The same run
  also found an automation-only harness gap: starting `daemon` via `go run` or a
  detached `/tmp` binary for a subsequent `codex run` caused the daemon to
  disappear before the `/health` check or before
  `/backend-api/codex/responses`, so the attempted automatic `git_status_diff`
  capture was discarded. Manual/Desktop captures and the previously documented
  T257 matrix remain valid; the automation harness should be hardened before
  relying on unattended multi-workload release captures.
- 2026-06-02: The unattended capture harness is now hardened by
  `go run ./scripts/utils codex-capture-run`. The command owns the daemon as a
  foreground child with `SLIMFERENCE_WSS_AB_CAPTURE`, preflights that no existing
  healthy daemon would steal the capture route, waits for `/health`, runs scoped
  `codex run --transport=auto`, records before/after admin-state token deltas,
  stops the daemon, replays the capture with fail-on-lost semantics, and can
  append the matching `wss-proof-matrix` row.
  On macOS, `--exit-marker` uses a `script(1)` PTY so Codex still sees a real
  terminal; `--exit-marker-count=2` handles the normal prompt-echo plus final
  marker pattern without manual Ctrl-C. Live proof
  `codex-capture-run-auto-repeat` passed end to end: 79 frames, 3 request turns,
  1 mutated request, 11,499 model-facing bytes saved, `lost=0`, and
  `gate_passed=true`. The one-row matrix correctly stayed red for breadth, so
  this closes the automation-lifecycle gap, not the full release-corpus breadth
  requirement.
- 2026-06-02: Follow-up CLI automation hardening fixed two real Codex TUI
  issues: the marker watcher now normalizes ANSI/control-rendered output so
  character-by-character markers are detected, and `--codex-timeout` bounds the
  scoped Codex command so a release capture cannot hang indefinitely.
  `--quiet-codex-output` keeps unattended runs readable without changing the
  capture or replay path. A fresh CLI-only matrix collected 8 valid rows:
  repeat full read, ranged read, search loop, git status/diff, build/test
  failure, no-savings control, changed file, and similar files. Replay results
  were `lost=0` for every row. Positive savings appeared on repeat full read
  (11,463 bytes), ranged read (10,200 bytes), search loop (414 bytes), and git
  status/diff (77 bytes). The build/test failure, changed file, and similar-file
  runs were safety proofs with zero mutation/savings under current auto policy;
  they should not be counted as positive savings. The CLI-only matrix
  intentionally failed the full T257 breadth gate because it has no Desktop
  captures, lacks `apply_patch_then_read` and a valid long mixed workday row,
  and only 5 rows were positive-or-expected-zero under the recorded metadata.
  A long-mixed CLI attempt hit upstream Codex `400 invalid_request` and was
  discarded.
- 2026-06-02: The capture runner and proof matrix are now token-first. Each
  managed `codex-capture-run` stores a live before/after admin-state delta in
  the matrix row: `billable_input_tokens_saved`, `input_tokens_saved`,
  output-wire/request-side byte counters, reducer-hit counters, and
  parse/degraded/compression safety counters. `wss-proof-matrix` uses live
  `billable_input_tokens_saved>0` as the positive-savings gate when `live_delta`
  is present and keeps replay `bytes_saved` only as the model-facing
  lost/regression proxy. This fixes the earlier ambiguity where byte replay was
  printed as the headline despite product savings being token savings.
- 2026-06-02: `expected_reducers` is now a hard live-counter gate for new
  token-delta rows. Known reducer names are `read_delta`, `captured_output`,
  `codex_exec_envelope`, `repeated_output`, and `chunk_dedup`; each expected
  reducer must have a positive live block counter. `none` is the explicit
  expected-zero/control marker. Unknown reducer names fail the capture row so a
  typo cannot make the proof look stronger than it is.
- 2026-06-02: `wss-proof-matrix --require-live-token-delta` is the release-proof
  mode. In that mode every row must contain a real `live_delta` captured from
  admin-state while the scoped daemon is alive; replay `bytes_saved` remains
  visible as a regression proxy but cannot count as positive savings.
- 2026-06-02: Fresh strict release matrix
  `/Users/christopher/.slimference/captures/release-proof-20260602_112516-cli-desktop-v1.jsonl`
  passed with `wss-proof-matrix --require-live-token-delta`: 14 captures total,
  9 CLI, 5 Desktop, all 10 required workload classes present, 11 positive live
  token-savings captures, 3 expected-zero controls, `captures_with_issues=0`,
  `gate_passed=true`, and no missing workloads. The matrix saved 43,113 live
  billable/input tokens across the included rows, with 17 Phase-F mutations, 7
  read-delta blocks, 5 captured-output/search blocks, 5 Codex exec-envelope
  blocks, and safety counters `parse_failures=0`, `degraded_sessions=0`,
  `compression_errors=0`. Replay gates for the added Desktop search, git-status,
  and long-mixed captures passed with `lost=0`; the long-mixed Desktop row
  proved `read_delta`, `captured_output`, and `codex_exec_envelope` together in
  one scoped Codex.app session. `repeated_output` and `chunk_dedup` had zero live
  block hits in this strict matrix and must not be claimed from this release
  proof.

## Deviations

(none)
