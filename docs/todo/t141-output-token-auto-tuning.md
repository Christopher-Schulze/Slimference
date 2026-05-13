# TASK 141: Output-token reduction auto-tuning and failure guard

Status: PENDING (opened 2026-05-13)
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

- Replace one profile string with tiered profiles:
  - `off`
  - `mild`
  - `standard`
  - `aggressive`
  - `codex_aggressive`
  - `custom`
- Each profile has:
  - directive text.
  - max added bytes.
  - min input tokens.
  - supported providers.
  - rollback threshold.

### WP2 - Task-shape detection

- Detect task shape from request:
  - direct answer
  - code edit
  - debugging
  - planning
  - review
  - tool-result reasoning
  - new file generation
- Rules:
  - code edit: prefer diff/tool action, no recap.
  - new file generation: do not force diff-only if full file is required.
  - review: keep findings complete, do not over-compress.
  - debugging: preserve exact error/path/line.

### WP3 - Measurement

- Track:
  - added input tokens from directive.
  - output tokens observed.
  - moving average by provider/model/profile/task shape.
  - tool failure rate after injection.
  - repair turns after injection.
  - user re-ask/re-read signals.
  - net token savings.
- Use flight recorder from T134 as the durable source.

### WP4 - Auto-soften/disable

- Add config:
  - `auto_tune_enabled`
  - `min_samples`
  - `min_net_savings_pct`
  - `max_failure_rate_delta`
  - `cooldown_turns`
- Behavior:
  - if aggressive underperforms, downgrade to standard.
  - if standard underperforms, downgrade to mild.
  - if mild underperforms, disable for that provider/model/task shape.
  - allow manual override.

### WP5 - Codex-specific maximum rules

- Codex aggressive directive should cover:
  - no preamble.
  - no recap of tool output.
  - no "let me know".
  - after successful patch/tool action, result plus verification only.
  - no full file unless requested or required.
  - comments only for non-obvious invariants.
  - yes/no first for binary questions.
  - preserve exact errors/paths/commands when relevant.
- Must not conflict with repository AGENTS instructions.

### WP6 - UI/CLI

- `slimference output-reduce status` shows:
  - active profile.
  - auto-tune status.
  - current provider/model downgrades.
  - net savings.
  - failure correlation.
- TUI card shows output-reduce as its own layer.
- `gain --output` reports real output savings only when baseline exists; otherwise reports observed output tokens and confidence.

### WP7 - Tests

- Injection per profile/provider/task shape.
- No duplicate marker.
- Auto-tune downgrade logic.
- Failure correlation logic.
- Reporting.

## Acceptance

- [ ] Output-reduce profiles are tiered and provider/model/task-aware.
- [ ] Auto-soften/disable prevents negative net outcomes.
- [ ] Codex aggressive profile is available and measured.
- [ ] TUI/CLI show output-reduce performance and confidence.
- [ ] No output-saving claim is made without baseline or confidence label.
- [ ] `go run ./scripts/ci` passes.

## Notes

- This task maximizes T130 without turning output reduction into brittle prompt spam.
