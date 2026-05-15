# TASK 148: Output-reduce real-session aggressive autotuning

Status: IN PROGRESS (T148a repair-turn detection and T148b profile-row reporting landed 2026-05-15; real A/B baseline still pending)
Priority: P1
Scope: `internal/outputreduce/`, `internal/quality/`, `internal/analytics/`, `internal/flight/`, `cmd/slimference/output_reduce_cmd.go`, `cmd/slimference/gain_cmd.go`, `tests/fixtures/output_reduce_corpus/`, `docs/output-reduce.md`.

## Why

T141 made output-reduce adaptive locally, but true savings require real baselines. Output tokens are expensive, and over-aggressive brevity can create repair turns that cost more than the saved text. This task makes output-reduce learn from real coding sessions, per provider/model/task shape, without pretending a mini smoke test proves usage savings.

## Target State

Output-reduce has a real-session tuning loop:

1. Capture baseline sessions.
2. Replay or A/B comparable prompts with profiles.
3. Measure output tokens, repair turns, failures, and user re-asks.
4. Evolve provider/model/task-shape directives.
5. Promote only profiles that save tokens without quality loss.

## Work Packages

### WP1 - Baseline collection

- For each corpus category, record:
  - output tokens.
  - task shape.
  - provider/model.
  - tool calls after response.
  - repair turns.
  - user re-ask indicators.
  - success/failure.
- Mark whether output-reduce was off/mild/standard/aggressive.

### WP2 - Comparable A/B mode

- A/B must be honest:
  - same or comparable prompt shape.
  - same model where possible.
  - same repository state where possible.
  - cache state recorded.
- If exact replay is not possible, report as "comparable", not measured baseline.

### WP3 - Directive variant library

- Maintain variants by provider/model/task:
  - direct answer.
  - code edit.
  - debugging.
  - review.
  - planning.
  - long tool-result reasoning.
  - new file generation.
- Variant fields:
  - directive text.
  - max added tokens.
  - required preservation rules.
  - disallowed behavior.
  - rollback threshold.

### WP4 - Repair-turn detection

- Detect negative outcomes:
  - [x] tool failure after response via HTTP error outcome.
  - [x] apply_patch / malformed-patch follow-up wording.
  - [x] user asks "what did you do" / "explain more" / "you skipped".
  - same task repeated.
  - [x] model outputs malformed patch/diff because directive was too strong
    when the next turn reports patch/application failure.
- [x] Feed into auto-downgrade for the previous provider/model/profile/task
  bucket. The signal is one-shot per session, so stale repair text cannot keep
  punishing older output-reduce decisions.

### WP5 - Quality validators

- Task-shape validators:
  - reviews still list all findings.
  - debugging preserves exact errors/paths/commands.
  - code edits do not omit required file content.
  - direct answers answer yes/no first when appropriate.
  - planning preserves blockers and sequence.
- If quality validator fails, savings are counted as invalid.

### WP6 - Profile evolution

- Promote/demote per provider/model/task shape.
- Keep global defaults conservative.
- Allow `codex_aggressive` only where live data is positive.
- Export a profile report for manual inspection before changing defaults.
- Implemented 2026-05-15:
  - `gain --output` now exports provider/model/profile/task-shape rows.
  - Rows include request count, applied/skipped counts, directive input overhead, observed output tokens, applied-turn output tokens, and averages.
  - Text, JSON, and CSV all expose the rows.
  - The report stays baseline-honest: it does not infer output-token savings without comparable baseline data.

## Acceptance

- [ ] `gain --output` can compare baseline and output-reduce sessions with confidence labels.
- [x] Repair-turn detection feeds the auto-tuner.
- [ ] Directive variants are provider/model/task-shape specific.
- [ ] Output savings are not counted when quality validators fail.
- [ ] Live corpus proves at least one promoted profile saves net output tokens.
- [ ] Aggressive profile can auto-downgrade without operator intervention.
- [x] `gain --output` exports provider/model/profile/task-shape rows for manual profile evolution without inventing savings.
- [x] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Realistic 10-30% output-token reduction on coding sessions.
- Higher on verbose models/tasks, lower on already terse models.
- Biggest dollar impact when output token pricing dominates input.

## Non-Goals

- Do not force diff-only output for tasks that require full file generation.
- Do not hide important review/debug information to save tokens.
- Do not claim savings without baseline or confidence label.

## Implementation Notes

- 2026-05-15 T148a:
  - Added `repair_followup` task-shape detection for "you skipped" / "explain
    more" / "what did you do" and German equivalents, plus patch/application
    failure follow-up phrases.
  - Output-reduce now skips repair follow-up turns with
    `repair_followup_low_roi`, because adding brevity rules to a user asking
    for missing detail is negative ROI.
  - The proxy remembers the last applied output-reduce bucket per session. If
    the next request is a repair/user-reask signal, it feeds that signal back
    into the auto-tuner for the previous provider/model/profile/task-shape
    bucket and can downgrade aggressive profiles.
  - Focus tests: `go test ./internal/outputreduce ./internal/proxy -cover` at
    100% for both packages.
- 2026-05-15 T148b:
  - Added output-reduce profile rows grouped by provider/model/profile/task
    shape to `internal/analytics.OutputReduceReport`.
  - `gain --output` prints the rows and exports them through JSON/CSV so manual
    profile evolution can inspect exactly where overhead and output volume came
    from before any default changes.
  - Focus tests: `go test ./internal/analytics ./cmd/slimference -cover`.
