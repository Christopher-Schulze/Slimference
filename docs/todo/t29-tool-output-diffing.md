# T29 - Semantic Tool-Output Diffing (Cross Tool-Call Delta)

Status: open
Priority: high
Scope: internal/compression (delta encoding extension), internal/summarization

---

## Problem

When the same tool is called multiple times with slight variations (e.g.
`git status` twice with one file changed in between, `ls` on the same dir,
repeated `npm test` runs), the full output is shipped every time. L1.6 Delta
Encoding exists, but needs verification: does it span **across tool_result
messages** from the same tool family, or does it only apply inside a single
message?

If the latter, this is the single largest untapped saving lever in the
compression stack. A one-line "same as before + added file X" carries the
same information as the full output at a fraction of the tokens.

---

## Desired End State

- Delta encoding walks the message history and, when a new `tool_result`
  matches a "same tool, same target, similar shape" predicate to an earlier
  tool result, emits a compact delta instead of the full body.
- The delta is textual, deterministic, and readable by the model (`"unchanged
  from message #N except: + src/new.go, - src/old.go"`).
- Safety guards: first occurrence always full, deltas only when savings
  clearly exceed overhead, disable on assistant's turn or last N messages.

---

## Work Packages

### WP1 - Audit current delta scope

- Read `internal/compression` delta encoder code.
- Write down: does it operate on adjacent messages only? Does it match across
  tool types? Result goes into `docs/delta-encoding-audit.md`.

### WP2 - Cross-message delta

- If current delta is per-message, extend it to look back across prior
  `tool_result` messages for the same `(tool_name, target)`.
- Compute a semantic diff: line-oriented for text outputs, structured for
  JSON outputs (detected by content-type or parse success).

### WP3 - Delta encoding format

- Pick a stable textual format that the model reads naturally, e.g.:

  ```
  [tool_result delta from message N]
  unchanged: 42 lines
  added:
  + src/new.go
  removed:
  - src/old.go
  ```

- Ship the format description in `spec+.md` §5.6 and provide one-shot
  examples in the system prompt area.

### WP4 - Safety and correctness

- Zero-downside: only emit delta if shorter than original and shorter than a
  naive compaction.
- Never delta-encode the latest user-visible tool result.
- Never delta-encode if the reference message was itself a delta (avoid
  delta-of-delta drift).

### WP5 - Tests

- Two `git status` calls with one changed file: delta is used, total tokens
  drop significantly.
- Two unrelated outputs: no delta.
- Reference message deleted by sliding window: fall back to full output.

### WP6 - Docs

- `spec+.md` §5.6 updated.
- `docs/documentation.md` cascade section: explain the interaction with L2.

---

## Subtasks

- [ ] Audit current delta scope.
- [ ] Implement cross-message delta on `tool_result`.
- [ ] Define textual delta format and document it.
- [ ] Add safety guards.
- [ ] Tests on realistic fixtures.
- [ ] Docs updates.

## Acceptance Criteria

- On benchmark sessions with repeated tool calls, delta encoding accounts for
  a measurable share of total savings.
- No regression in model answer quality on paired-session A/B runs.
- Coverage stays at 100 %.
