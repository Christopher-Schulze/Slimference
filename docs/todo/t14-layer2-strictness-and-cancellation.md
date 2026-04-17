# T14 - Layer 2 Strictness and Cancellation

Status: open
Priority: high
Scope: `internal/summarization/*`, Layer 2 policy, MiniMax integration behavior

---

## Problem

The project wants MiniMax to be used as strongly as possible, but it also wants
zero downside and near-zero perceived latency. The current implementation is an
async best-effort system, which is safe in spirit but not explicit enough in
policy and not strong enough in proof.

Also, some summarization paths still ignore caller cancellation by using
`context.Background()`.

---

## Desired End State

1. The repository has explicit Layer 2 operating modes.
2. Cancellation propagates through every MiniMax call path.
3. The validator checks real preservation obligations, not markdown accidents.
4. MiniMax usage is aggressive only where correctness remains provable.

---

## Proposed Policy Model

Introduce documented Layer 2 modes:

- `best_effort_async`
  - current user-facing behavior
  - never blocks the hot path
  - skips on failure

- `strict_verified`
  - only applies summaries that satisfy stricter validation and freshness rules
  - still avoids unsafe hot-path behavior
  - exposes stronger proof for users who want maximum compression discipline

This makes the trade-off explicit instead of implicit.

---

## Work Packages

### WP1 - Full context propagation

- Remove remaining `context.Background()` call sites in summarization execution paths.
- Thread caller context through `RunCompressionJob`, progressive compression,
  retries, and provider fallback.

### WP2 - Validator hardening

- Validate against structured message content, not only markdown-fenced code.
- Improve preservation checks for:
  - function and method names
  - tool names and tool results
  - file paths
  - explicit errors and decisions

### WP3 - MiniMax policy surface

- Add explicit config for Layer 2 operating mode.
- Document the precedence rules:
  - correctness
  - cancellation
  - validation
  - then compression yield

### WP4 - Proof tests

- cancellation tests for all summarization paths
- validator rejection tests for missing function/file/error preservation
- mode-specific behavior tests

---

## Subtasks

- [ ] Remove remaining `context.Background()` usage from Layer 2 execution paths.
- [ ] Design explicit Layer 2 operating modes and config semantics.
- [ ] Rework validator inputs to inspect structured content blocks.
- [ ] Add regression tests for function, file, tool, and error preservation.
- [ ] Add mode-driven tests for best-effort vs strict-verified behavior.

---

## Acceptance Criteria

- No MiniMax request path ignores cancellation.
- Validator decisions are based on the actual structured session content.
- The repository has a documented answer to the tension between strictness,
  zero downside, and latency.
