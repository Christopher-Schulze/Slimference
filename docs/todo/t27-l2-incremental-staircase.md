# T27 - Layer 2 Incremental-Summary Tiered Thresholds

Status: done
Priority: medium
Scope: internal/summarization/layer2.go, internal/summarization/cache.go,
       internal/config (tuning)

---

## Problem

Layer 2 chooses between "rebuild full summary" and "incremental delta over
existing summary" using a single hard threshold: 70 % range-overlap. For long
sessions (>60 messages), this means a moderate drift often triggers a full
rebuild, which is the most expensive path.

A tiered threshold follows the natural cost-value curve:

- Short session (<= 20 msgs): always rebuild (cheap, no gain from delta).
- Medium session (20-60 msgs): current 70 % threshold.
- Long session (> 60 msgs): looser threshold (e.g. 50 %), because the cost
  of full rebuild grows with message count while delta stays cheap.

---

## Desired End State

`shouldUseIncremental(existing, current)` consults a staircase keyed on the
current message count:

| Msg count | Overlap threshold for incremental |
|-----------|-----------------------------------|
| <= 20     | n/a (always full)                 |
| 20-60     | 0.70 (current)                    |
| 60-120    | 0.55                              |
| > 120     | 0.40                              |

Each tier's threshold is configurable under `[compression.tuning] incremental_staircase`.

---

## Work Packages

### WP1 - Staircase helper

- Implement `incrementalThresholdFor(msgCount int) float64` reading from
  config.
- Unit tests cover boundaries.

### WP2 - Wire into Layer 2

- Replace the hardcoded `0.70` overlap check in `layer2.go` with a call to
  the staircase helper.
- Update decision-log reason strings so `slimference debug last` surfaces
  which tier was used.

### WP3 - Config

- `[compression.tuning] incremental_staircase` as `[[threshold]]` array or
  explicit triples: `{ msg_count_le, threshold }`. Prefer explicit.
- Validate monotonicity.

### WP4 - Tests

- Session of 30 messages drifts 35 % -> full rebuild (below 0.70).
- Session of 30 messages drifts 20 % -> incremental (above 0.70).
- Session of 100 messages drifts 45 % -> incremental (above 0.55 long-tier).
- Session of 100 messages drifts 60 % -> full rebuild.

---

## Subtasks

- [x] Staircase helper + tests.
- [x] Layer 2 integration.
- [x] Config schema and defaults.
- [x] Decision-log reason strings include tier.
- [x] Regression tests on canonical sizes.

## Acceptance Criteria

- Long sessions use incremental path more often.
- MiniMax API cost per session trends down on benchmark runs.
- Coverage stays at 100 %.
