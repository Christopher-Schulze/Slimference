# T330 Codex WSS Output-Reduce Directive Guard

## Why

A fresh scoped Codex CLI session in `/Users/christopher/CODE/Golem` still hit an
upstream `invalid_request_error` after T329. The rejected follow-up frame was
byte-equal in Slimference telemetry, so the latest failing request was not the
mutator. The same WSS session, however, had a previous user-turn request where
Slimference injected an output-reduce directive into Codex `instructions`
(`output_reduce.applied=true`, `added_tokens=23`, no input savings). That is the
only model-facing WSS mutation correlated with the new failure.

The fix must be narrow: do not disable Layer 0, read-delta, chunk-dedup,
tool-prune, provider-cache accounting, or non-WSS output-reduce. Disable only
Codex WSS Phase-F model-facing output-reduce directive injection.

## Acceptance

- Codex WSS Phase-F never injects output-reduce directives into request
  `instructions`.
- The runtime records WSS directive candidates as
  `codex_wss_directive_disabled`.
- Central planner telemetry also reports WSS Layer 3 output-reduce as bypassed
  with the same reason.
- WSS Layer 0/tool-output savings tests stay active and unchanged except where
  they previously expected directive injection.
- Product documentation separates historical WSS output-reduce A/B evidence
  from the current default-safe WSS product path.
- Focused proxy/planner tests, full Go tests, CI, and local rebuild/install pass.

## Sub-Tasks

- [x] Add the WSS output-reduce directive guard in the Phase-F runtime path.
- [x] Align the planner with the runtime guard.
- [x] Replace the old WSS injection regression with a byte-equal safety
  regression.
- [x] Update documentation and task notes.
- [x] Run focused tests, full tests, CI, and rebuild/install the binary.

## Notes

- This does not reduce deterministic input-token savings. It removes a
  speculative output-token instruction on WSS that can add input overhead and
  poison the server-side conversation state.
- Non-WSS output-reduce remains governed by T267 paired A/B proof and shape
  guards.
- Provider output-token accounting remains available on WSS; it just does not
  imply that WSS directive injection is currently safe.
- `benchmark-corpus --maxx-check` no longer requires WSS output-reduce workload
  classes. The category validators still cover archived output-reduce
  diagnostics.

## Deviations

- None.
