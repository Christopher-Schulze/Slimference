# T24 - Structure Extraction inside the Sliding Window

Status: done
Priority: medium
Scope: internal/compression/structure.go, internal/compression/layer1.go

---

## Problem

Layer 1.4 (Structure Extraction) currently only runs on messages **outside**
the sliding window. Code blocks inside the window are forwarded verbatim. For
many sessions that is fine, but the failure mode is large: whenever a user
turn contains a big file paste or a long tool-output with heavy code, the full
raw text is shipped upstream.

Extracting signatures (function names, type definitions, imports) inside the
window is only lossy if the model needs the exact bodies. In most interactive
coding sessions the model reasons over signatures first and asks for bodies on
demand. Being selective here is the classic "spart genau dort wo es am
meisten kostet" lever.

---

## Desired End State

- Structure extraction can run inside the window, gated by size thresholds
  and an explicit safety rule: body is preserved unless it is clearly large
  boilerplate (e.g. >= N tokens, repeated structural patterns, or test data).
- Config knob `[compression.tuning] structure_in_window_min_tokens` (default
  large enough to be conservative, e.g. 1500) controls the floor.
- Never extract structure on the assistant's latest turn, never inside an
  explicit tool_use request, never on content the user just typed as a
  direct question.

---

## Work Packages

### WP1 - Safety rules

- Define `shouldExtractInWindow(msg, idx, total, window) bool`:
  - Role must be `tool_result` or `user` paste.
  - Block must exceed the size floor.
  - Must not be the last message.
  - Must not contain structured markers indicating "review this exactly"
    (e.g. diff markers, PR body).

### WP2 - Integrate into Layer 1

- After the existing structure step, also walk the in-window messages and
  apply extraction when the safety rule allows it.
- Record savings under a new bucket `structure_in_window` in
  `Layer1Result`.

### WP3 - Tests and fixtures

- Positive: large pasted file inside window gets signature-extracted.
- Negative: last user turn stays verbatim.
- Negative: small code snippet stays verbatim.
- Negative: explicit diff or PR body stays verbatim.
- Regression: zero-downside guard still reverts if net savings flip negative.

### WP4 - Docs

- Update `spec+.md` §5.4 to describe the extended scope.
- Update `docs/documentation.md` Layer 1 breakdown.

---

## Subtasks

- [x] Define and implement the safety rule helper.
- [x] Extend Layer 1 pipeline.
- [x] Add `structure_in_window` to `Layer1Result` and debug breakdown.
- [x] New tests for positive/negative cases.
- [x] Docs update.

## Acceptance Criteria

- Benchmark scenario "large file paste": measurable savings on the forwarded
  body with no regression on unit tests.
- Interactive edit flows still see the exact code the user just typed.
- Coverage stays at 100 %.
