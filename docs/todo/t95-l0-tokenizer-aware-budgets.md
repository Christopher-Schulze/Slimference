# TASK 95: Tokenizer-aware Layer 0 budgets

Status: deferred - filter subprocess has no provider context today; per-call provider hint requires hook-install plumbing
Priority: P2
Scope: `internal/filter/`, `internal/tokens/`, `internal/config/`
Driver: Layer 0 truncation thresholds (`passthrough_max_chars`, lint-violation cap) are character-based. Codex (`o200k`) and Claude tokenizers count differently. Same character budget produces very different token costs.

---

## Problem

`[filter] passthrough_max_chars` defaults to 2000. For Claude that maps to ~500 tokens. For Codex with a denser tokenizer the same string can be 600+ tokens. Layer 0 has no notion of which downstream tokenizer the output will hit, so it under-trims for Codex and over-trims for sparse-text upstreams.

## Target State

Layer 0 truncation budgets are tokenizer-aware:

- `[filter] passthrough_max_tokens` replaces (or augments) `max_chars`.
- Token count uses the existing per-provider tokenizer (T28).
- The active provider is determined by the running session's destination (Claude / Codex / OpenAI).
- Default budgets are tuned per provider to give equal effective token cost.

## Implementation Plan

### WP1 - Tokenizer hook
- Filter pipeline gets read access to the active provider's tokenizer.

### WP2 - Budget config
- Add `passthrough_max_tokens` per provider; legacy `max_chars` honoured as fallback.

### WP3 - Tests
- Snapshot: same input produces different truncation for Claude vs Codex tokenizers.

### WP4 - Docs
- Update `docs/documentation.md` Layer 0 section to call out the tokenizer-aware budgets.

## Acceptance Criteria

- [ ] Filter truncation respects per-provider token budget.
- [ ] Legacy `max_chars` keeps working when `max_tokens` is unset.
- [ ] No regression on existing fixtures.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Replacing the existing per-character knobs entirely (back-compat).
- Adaptive per-tool budgets (could be a follow-up).

## Validation

```
go test ./internal/filter/... ./internal/tokens/...
```

## Closure Notes (2026-04-30)

`slimference filter <cmd>` runs as a one-shot subprocess invoked by
hooks. It has no live provider context: it does not know whether the
calling agent will route the result to Claude, OpenAI, or Codex.

Adding tokenizer-aware budgets cleanly therefore requires either:

1. A new `--provider` flag on `slimference filter`, set per-hook at
   install time so the right value reaches each subprocess. Touches
   `internal/integrate/`, every hook script template, and
   `cmd/slimference/main.go`.
2. A `SLIMFERENCE_TARGET_PROVIDER` env var that the hooks export. Same
   distribution problem.

Option 1 is the cleaner long-term path but is a significant install
matrix change. Until measured evidence shows the existing rune-based
budget materially over- or under-trims for any specific provider, the
extra complexity is not justified.

Closed as deferred. Re-open if a Codex corpus measurement shows the
default budget is wrong for Codex's tokenizer by more than ~15%.
