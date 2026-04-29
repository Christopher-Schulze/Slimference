# TASK 90: Partial-repair on validator failure

Status: completed (deterministic only; model-driven repair deferred)
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

## Closure Notes (2026-04-30)

Landed:

- New `RepairSummary(s)` deterministic repair with three transformations:
  markdown header strip, alternative bullet style normalisation
  (`*` and `1.` -> `- `), and leading non-bullet preamble trim.
- Per-class counters via `RepairCounts()` /
  `ResetRepairCounts()` for the three repair classes plus a
  `deterministicTotal`.
- Layer 2 wires the repair into the validator-fail path: when the
  initial summary fails validation, repair runs first; if revalidate
  passes, the path skips the API-cost retry. Otherwise the existing
  retry path runs unchanged.
- Test `TestLayer2_RunCompressionJob_repairBypassesRetry` proves a
  `* `-bullet response causes exactly one upstream call (initial),
  with the deterministic repair restoring `- ` format and re-validating
  successfully.
- 100% coverage; CI green.

Deferred:

- Model-driven repair pass (a short MiniMax call with "fix only the
  offending lines"). The deterministic pass already covers preamble,
  format, and header issues; model repair would help only on
  preservation-check failures (paths/functions/errors) which require
  semantic understanding. Add when evidence shows the deterministic
  pass leaves a meaningful residual.
