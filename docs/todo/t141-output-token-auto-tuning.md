# TASK 141: Output-token reduction auto-tuning and failure guard

Status: DONE (local implementation 2026-05-13; live saving proof and next-level tuning remain T140/T146/T148)
Priority: P1
Scope: `internal/outputreduce/`, `internal/proxy/handler.go`, `internal/analytics/`, `internal/quality/`, `cmd/slimference/output_reduce_cmd.go`, `cmd/slimference/gain_cmd.go`, `internal/tui/`, `docs/output-reduce.md`.

## Why

T130 added static provider-specific output-discipline injection. That is useful but not maximum. Output tokens are expensive, and the model's compliance varies by provider/model/task. Aggressive instructions can save tokens, but they can also cause tool failures, missing explanations, or extra repair turns. We need measured auto-tuning, not static optimism.

## Target State

Output-reduce becomes a feedback-controlled system:

1. Inject concise rules only when request size/task shape justifies overhead.
2. Measure output-token changes per provider/model/profile.
3. Correlate output-reduce with tool failure, repair turns, user re-asks, and negative savings.
4. Auto-soften or disable profiles that underperform.
5. Keep aggressive Codex rules where they demonstrably save tokens without harming task success.

## Work Packages

### WP1 - Profile tiers

- [x] Replace one profile string with tiered profiles:
  - `off`
  - `mild`
  - `standard`
  - `aggressive`
  - `codex_aggressive`
  - `custom`
- [x] Each profile has:
  - directive text.
  - max added bytes.
  - min input tokens.
  - supported providers.
  - rollback threshold.

### WP2 - Task-shape detection

- [x] Detect task shape from request:
  - direct answer
  - code edit
  - debugging
  - planning
  - review
  - read-only analysis
  - tool-result reasoning
  - new file generation
- [x] Rules:
  - code edit: prefer diff/tool action, no recap.
  - new file generation: do not force diff-only if full file is required.
  - review: keep findings complete, do not over-compress.
  - read-only analysis: do not push patch/diff/full-file behavior when the operator explicitly asked for inspect/analyze/audit/report without edits.
  - debugging: preserve exact error/path/line.

### WP3 - Measurement

- [x] Track:
  - added input tokens from directive.
  - output tokens observed.
  - moving average in admin tracker.
  - provider/model/profile/task-shape downgrade buckets.
  - tool failure after injection via HTTP status.
  - repair/user-reask signals in the `Outcome` API for future hook/session integrations.
- [x] Use flight recorder / analytics as durable source for applied profile, reason, added tokens, output tokens, and task shape.
- [ ] True net token savings still requires a baseline corpus or A/B live run; no local fake baseline was added.

### WP4 - Auto-soften/disable

- [x] Add config:
  - `auto_tune_enabled`
  - `min_samples`
  - `min_net_savings_pct`
  - `max_failure_rate_delta`
  - `cooldown_turns`
- [x] Behavior:
  - if aggressive underperforms, downgrade to standard.
  - if standard underperforms, downgrade to mild.
  - if mild underperforms, disable for that provider/model/task shape.
  - allow manual override.

### WP5 - Codex-specific maximum rules

- [x] Codex aggressive directive covers:
  - no preamble.
  - no recap of tool output.
  - no "let me know".
  - after successful patch/tool action, result plus verification only.
  - no full file unless requested or required.
  - comments only for non-obvious invariants.
  - yes/no first for binary questions.
  - preserve exact errors/paths/commands when relevant.
- [x] Must not conflict with repository AGENTS instructions.

### WP6 - UI/CLI

- [x] `slimference output-reduce status` shows:
  - active profile.
  - auto-tune status.
  - thresholds and cooldowns.
- [x] Admin status exposes current downgrade buckets through `output_reduce.downgrades`.
- [x] TUI consumes admin status for output-reduce visibility through the existing runtime stats surface.
- [x] `gain --output` reports observed output tokens and bucket breakdowns only. It still refuses to claim savings without baseline.

### WP7 - Tests

- [x] Injection per profile/provider/task shape.
- [x] No duplicate marker.
- [x] Auto-tune downgrade logic.
- [x] Failure/overhead downgrade logic.
- [x] Reporting.

## Acceptance

- [x] Output-reduce profiles are tiered and provider/model/task-aware.
- [x] Auto-soften/disable prevents repeated negative local outcomes.
- [x] Codex aggressive profile is available and measured.
- [x] TUI/CLI/admin show output-reduce telemetry and auto-tune state.
- [x] No output-saving claim is made without baseline or confidence label.
- [x] `go run ./scripts/ci` passes.

## Notes

- This task maximizes T130 without turning output reduction into brittle prompt spam.
- Implemented local scope: tiered profiles, task-shape classifier, auto-tune tracker, config knobs, analytics/flight task-shape metadata, CLI/docs/tests.
- 2026-05-14 follow-up: the auto-tune cooldown state is now queryable through
  `Tracker.InCooldown` and is fed into T149 planner facts before profile
  selection, so a cooled aggressive bucket is visible as a planned
  `cheap_only` profile-softening decision.
- 2026-05-14 follow-up after the larger Codex CLI read-only probe: explicit
  read-only/audit prompts now classify as `read_only_analysis` before edit or
  new-file keywords are considered. This prevents file-path-heavy inspection
  prompts from receiving new-file/edit-oriented output rules.
- Live quality proof still belongs to T140/T146, and next-level real-session output profile evolution is tracked as T148. Local T141 code only prevents obvious repeated failure/overhead patterns.
- Verification: `go run ./scripts/ci` passed 8/8 on 2026-05-13 with 100.0% statement coverage.
