# TASK 90: Partial-repair on validator failure

Status: todo
Priority: P2
Scope: `internal/summarization/validator.go`, `internal/summarization/minimax.go`, `internal/proxy/handler.go`
Driver: When the validator rejects a MiniMax output (preamble, missing dash prefix, format violation), Slimference falls back to the raw, uncompressed body. If 90% of bullets are correct and one line violates, all of the work is wasted. A single cheap repair call recovers most of the savings.

---

## Problem

Today the path is binary: pass the validator -> use summary; fail -> revert to raw. Many real failures are localised: one stray paragraph at the start, a markdown header, a bullet with a wrong prefix. A short, targeted repair call (or a deterministic local fix) recovers the summary without paying full reroute cost.

## Target State

When the validator finds violations:

1. Try **deterministic repair** first: trim preamble, force `- ` prefix, strip stray markdown. Re-validate. If pass, done.
2. If still failing, optionally call MiniMax with a short `repair only the offending lines` prompt. Validate. If pass, done.
3. Only if both repair attempts fail, fall back to the raw body.

Each step has counters so the operator can see how often each path fires.

## Implementation Plan

### WP1 - Deterministic repair
- `internal/summarization/repair.go` with rules: trim preamble, normalise dashes, drop empty bullets, collapse blank lines.

### WP2 - Optional model-driven repair
- Short prompt: "Fix only the lines that don't begin with `- `. Return the full summary." Cap tokens to the original target.
- Disabled by default; enable via `[summarization] enable_model_repair = true`.

### WP3 - Wire into MiniMax client
- After validator failure: deterministic repair -> revalidate -> optional model repair -> revalidate -> raw body fallback.

### WP4 - Counters
- `summary_repair_deterministic_total`, `summary_repair_model_total`, `summary_repair_failed_total`.

### WP5 - Tests
- Fixtures for each failure pattern; assert deterministic repair fixes them.

## Acceptance Criteria

- [ ] Deterministic repair recovers the summary on a documented set of failure patterns.
- [ ] Model-driven repair is opt-in; default off.
- [ ] Counters are exposed in `/admin/status.summarization`.
- [ ] Raw-body fallback only fires after both repair attempts fail.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- LLM-based output rewriting beyond the targeted repair prompt.
- Quality scoring of the repaired output.

## Validation

```
go test ./internal/summarization/...
```
