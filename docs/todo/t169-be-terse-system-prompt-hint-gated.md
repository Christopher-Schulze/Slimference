# TASK 169: Be-terse system-prompt hint (Quality-gated)

Status: TODO (planning 2026-05-16)
Priority: P2 (high token win, real Quality risk — needs A/B)
Scope: `internal/proxy/handler.go`, `internal/proxy/provider.go`, `internal/config/config.go`, `internal/quality/` (regression detector)

## Why

The most-cited intervention from LLMLingua / instruction-tuning literature: a single line like "Reply concisely. No commentary unless asked." appended to the system prompt reduces output tokens 15-30% across most general workflows. The cost: Quality. Some tasks legitimately need verbose explanation; cutting it harms accuracy.

This task ships the feature as **off by default**, with an explicit Quality-gate that watches for regressions and auto-rolls-back if accuracy degrades.

**Why:** Largest potential single-knob output-token win. But MUST be measured against task accuracy. Ship it behind an A/B gate that disables itself when accuracy drops measurably below baseline.
**How to apply:** Inject the hint into the request as a high-priority system-prompt suffix. Track quality via the existing `internal/quality/` detector. When detector reports degradation → automatic rollback for the session.

## Target State

1. `[compression.output_reduce] terse_hint = "off|on|adaptive"` (default `"off"`).
   - `off`: never inject
   - `on`: always inject
   - `adaptive`: inject for N requests, monitor quality, disable session-wide if Quality regresses
2. The injected line is configurable: `[compression.output_reduce] terse_hint_text = "Reply concisely. No commentary unless asked."`
3. Quality detector integration: re-use `internal/quality/` re-read + cache-miss-spike + custom output-length-degradation signals.
4. CLI: `slimference terse status|enable|disable [--adaptive]`.

## Acceptance

- `terse_hint=off`: no injection; tokens identical to baseline.
- `terse_hint=on`: hint suffixed to system prompt; output tokens ≥10% lower on a captured benchmark.
- `terse_hint=adaptive`: hint active for first N=10 requests; if Quality regressor fires, hint disabled session-wide for remainder.
- Quality A/B harness: run a 50-request workflow with/without hint and compare task-completion accuracy. Pass = ≥95% of baseline.
- 100% coverage on the gating code; integration test for adaptive-mode rollback.

## Sub-Tasks

- [ ] Design the Quality signals that constitute "regression" (re-read rate, cache-miss spike, output-length anomaly).
- [ ] Implement the `terse_hint` injection in `injectOpenAIPromptCache` neighbouring code.
- [ ] Adaptive mode: per-session state machine with rollback.
- [ ] CLI subcommand.
- [ ] Quality benchmark suite (capture-session driven).
- [ ] Documentation: when to enable, what the risk looks like, how to roll back.

## Notes

- This is the **most-likely-to-regress** task in this batch. Default-off is intentional.
- Without quality gating, this is irresponsible. With it, it's a real lever.
- Consider per-provider behaviour: Claude reacts differently to terseness hints than GPT-4o.

## Deviations

(none yet)
