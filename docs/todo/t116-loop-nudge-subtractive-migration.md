# TASK 116: Loop nudge - measurement-driven migration to subtractive form

Status: DONE 2026-05-01 (audit-driven mitigation 2026-04-30)
Priority: P2
Scope: `internal/compression/loop_detect.go`, `internal/compression/layer1.go`, `internal/quality/`, `internal/proxy/handler.go`
Driver: `ApplyLoopNudge` violates the spec's "additive-only transformation" principle (`spec+.md` §1) by INSERTING a synthetic assistant note when 4+ near-duplicate user messages are detected. The token-savings number it reports (`(streak - 1) * 5000`) is a **hardcoded estimate without A/B data**. Two problems: (a) the math is fictitious until measured; (b) the spec wants subtractive transformations only. Fix: instrument the existing nudge to gather real data on whether it actually breaks loops, then migrate to a subtractive form (collapse near-duplicate user messages with `[Near-duplicate of msg N - omitted]`) once the data confirms the upstream behaviour change is real.

---

## Problem

`internal/compression/loop_detect.go:106`:
```go
const loopNudgeSavingsPerStreakMsg = 5000
```

`ApplyLoopNudge` returns `(streak - 1) * 5000` as the saved-token count. This figure shows up in `Layer1Result.LoopNudgeSaved` and lands in dashboards as if it were measured. It is not. It is a guess inherited from a different tool's prior reading.

Worse, the mechanism is additive: it prepends a `[slimference-loop-nudge] ...` text block to the latest user message. This is the only spec-violation in Layer 1 today. The spec is clear: the proxy "only REMOVES or REPLACES tokens. It never ADDS content to the conversation that the user/model did not produce."

Counter-argument: nudging is empirically useful. The literature behind RTK's loop detection saw real loop breakouts. The subtractive form might be less effective.

Solution: don't guess. Measure, then migrate.

## Target State

Two phases:

**Phase 1 (this task) - Honest measurement**:
- Existing nudge stays in place but its `LoopNudgeSaved` counter is replaced with a measured signal: did the next user turn diverge from the streak (loop broken) or not (loop continued)?
- Per-loop telemetry: `(detected_streak_len, nudge_emitted, next_turn_jaccard, loop_broken bool, observed_savings_tokens)`.
- After 30+ observed nudges, `slimference quality` surfaces a measured loop-break rate and an empirical token-saving distribution.

**Phase 2 (T116b, gated on Phase 1 data)** - **Migration to subtractive form**:
- New `CollapseLoopMessages` rewrites the streak: keep the first user message of the streak, replace messages 2..N with `[Near-duplicate of message I - collapsed at message J]` references (same shape as the existing dedup output).
- `ApplyLoopNudge` is removed from the compression pipeline.
- The collapsed-references math IS measurable (savings = sum of replaced-message lengths) and spec-compliant.

## Implementation Plan

### WP1 - Measurement instrumentation
- `Layer1Result.LoopNudgeMeasurement{StreakLen, NudgeEmitted, NextTurnSimilarity, LoopBroken bool, ObservedSavedTokens int}`.
- `internal/quality/loop_break.go` accumulates these per session.
- `/admin/status.quality.loop_nudges.{detected_total, broken_total, broken_rate, p50_observed_savings, p95_observed_savings}`.

### WP2 - Drop the hardcoded 5000
- `LoopNudgeSaved` gets replaced with the post-loop measurement: when the next request shows the streak ended, attribute the difference to the nudge; otherwise zero.
- Until the next request arrives, the saving number is `0` (not the misleading 5000). Dashboards reflect honest "to be confirmed" state.

### WP3 - Subtractive design (parallel branch)
- Implement `CollapseLoopMessages` in a sibling file, gated behind `[compression.tuning] loop_collapse_subtractive = false` (default off).
- When on: replaces the additive nudge with the collapse path.
- When off: today's behaviour (additive nudge) - no live regression.

### WP4 - A/B experiment scaffold
- `[compression.tuning] loop_strategy = "additive" | "subtractive" | "off"`.
- Telemetry buckets results per strategy so the operator can run the A/B against their own traffic.

### WP5 - Decision gate (Phase 2 / T116b)
- Open T116b after 30+ measured loops on each strategy.
- Decision: if subtractive achieves >= 80% of additive's loop-break rate, migrate fully and remove additive.
- If subtractive underperforms substantially, keep additive but reframe the spec deviation explicitly as "additive-with-justification" in `spec+.md`.

### WP6 - Tests
- Honest-savings unit test: `LoopNudgeMeasurement` records 0 until the next-turn signal confirms break.
- Strategy switch test: each of the three modes produces the expected pipeline behaviour.
- Race test on the quality counter.

## Acceptance Criteria

- [ ] `LoopNudgeSaved` no longer reports 5000-per-streak as if measured.
- [ ] `internal/quality/loop_break.go` records honest measurements.
- [ ] `[compression.tuning] loop_strategy` switches between `additive` (default), `subtractive`, `off`.
- [ ] Telemetry distinguishes the three strategies.
- [ ] Coverage 100%; race tests green.
- [ ] T116b created as the migration-decision follow-up.

## Out of Scope

- Removing the additive form before measurement supports doing so.
- Rewriting the Jaccard similarity metric (T56 owns that).
- Detecting non-text loops (tool_use cycles - separate concern).

## Validation

```
go test -race ./internal/compression/... ./internal/quality/...
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
```
