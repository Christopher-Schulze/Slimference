# TASK 148: Output-reduce real-session aggressive autotuning

Status: PENDING (planned 2026-05-13)
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
  - tool failure after response.
  - apply_patch failure.
  - user asks "what did you do" / "explain more" / "you skipped".
  - same task repeated.
  - model outputs malformed patch/diff because directive was too strong.
- Feed into auto-downgrade.

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

## Acceptance

- [ ] `gain --output` can compare baseline and output-reduce sessions with confidence labels.
- [ ] Repair-turn detection feeds the auto-tuner.
- [ ] Directive variants are provider/model/task-shape specific.
- [ ] Output savings are not counted when quality validators fail.
- [ ] Live corpus proves at least one promoted profile saves net output tokens.
- [ ] Aggressive profile can auto-downgrade without operator intervention.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Realistic 10-30% output-token reduction on coding sessions.
- Higher on verbose models/tasks, lower on already terse models.
- Biggest dollar impact when output token pricing dominates input.

## Non-Goals

- Do not force diff-only output for tasks that require full file generation.
- Do not hide important review/debug information to save tokens.
- Do not claim savings without baseline or confidence label.

