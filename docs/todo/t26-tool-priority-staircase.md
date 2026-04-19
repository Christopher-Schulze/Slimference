# T26 - Tool-Result Priority Staircase for Repeated Heavy Outputs

Status: done
Priority: medium
Scope: internal/summarization/priority.go, internal/compression (tool classifier),
       internal/config (tuning)

---

## Problem

`priority.go` already classifies tool results by weight (HIGH/MEDIUM/LOW). The
current compression strategy is uniform per class: LOW gets aggressive
compression, HIGH is preserved. In practice, **repeats** of the same tool type
carry less marginal information than the first occurrence:

- First screenshot / image result: full info useful.
- Second screenshot of the same target 5 turns later: mostly redundant.
- Third grep result from the same directory: even more redundant.

A simple staircase (100 % / 50 % / 25 %) captures that monotonic decay without
losing the first, strongest signal.

---

## Desired End State

Priority classifier tracks occurrences of the same `(tool_name, content_hash_prefix)`
or `(tool_name, target)` tuple across the conversation history. On the
**Nth** repeat, it applies a staircase discount to the target token budget:

- 1st: 100 % (baseline).
- 2nd: 50 %.
- 3rd+: 25 %.

Staircase ratios configurable under `[compression.tuning] priority_staircase`.
Staircase never forces below a per-class floor (so the first and last frames
stay readable).

---

## Work Packages

### WP1 - Repetition detector

- Build a per-session dictionary keyed by `(tool_name, topic)` where topic is
  either the explicit target (e.g. file path, URL, grep pattern) or a hash
  prefix of the content.
- Walk messages in order, increment on each match.
- Keep memory bounded (session scope, cleared when session changes).

### WP2 - Staircase application

- In the existing priority-based compressor, multiply the allowed token
  budget for message N by the staircase factor corresponding to its repeat
  index.
- Apply after existing HIGH/MEDIUM/LOW class weighting.
- Always keep the first occurrence at 100 %.

### WP3 - Config

- New config knob `[compression.tuning] priority_staircase = [1.0, 0.5, 0.25]`.
- Validate: monotonically non-increasing, first must be 1.0, length >= 2.

### WP4 - Tests

- Sequence: three identical large grep outputs. Expect sizes 1.0x, 0.5x, 0.25x.
- Mixed sequence: two unrelated tool calls retain full size.
- Edge: exactly 2 repetitions. Staircase still correct.

---

## Subtasks

- [x] Build repetition detector.
- [x] Implement staircase factor application in priority-aware compressor.
- [x] Wire config.
- [x] Add unit and integration tests.
- [x] Document in `spec+.md` §6.

## Acceptance Criteria

- Benchmark scenario with repeated heavy tool calls shows additional savings
  compared to non-staircase baseline.
- First occurrence retains full info (verified by diff).
- Coverage stays at 100 %.
