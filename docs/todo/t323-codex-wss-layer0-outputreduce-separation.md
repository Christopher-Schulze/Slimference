# T323 Codex WSS Layer0/Output-Reduce Separation

## Why

A fresh Golem live session still hit an upstream Codex WSS
`invalid_request_error` after a request in the same session showed a Layer 0
`search_output` mutation and debug telemetry reported output-reduce as applied.
The previous T322 guard blocked obvious tool-result shapes, but the WSS planner
summary still conflated any Phase-F replacement with output-reduce application.
That made a risky combined path hard to distinguish from safe deterministic
Layer 0 savings.

## Acceptance

- WSS output-reduce must not run on any request that already received a Layer 0
  mutation.
- WSS output-reduce telemetry must reflect only actual directive injection, not
  Layer 0 replacement.
- Existing WSS Layer 0 read/git/test compaction must continue to work; WSS
  search-output safety is handled separately in T324 after the follow-up live
  failure reproduced without output-reduce.
- Regression coverage must prove that a Layer0-compacted tool-output turn has
  positive Layer 0 savings but `output_reduce.applied=false`.
- The installed local binary must be rebuilt after the fix.

## Sub-Tasks

- [x] Pass actual output-reduce stats through the WSS input pipeline.
- [x] Block output-reduce whenever WSS Layer 0 modifies the request body.
- [x] Replace the legacy planner summary reason that treated every WSS
  replacement as output-reduce.
- [x] Extend regression tests for compacted response-item tool output and WSS
  planner summaries.
- [x] Run focused proxy tests, full Go tests, CI, and reinstall the latest
  binary.

## Notes

- Root cause: Phase-F telemetry and gating were coupled too loosely. A WSS
  request could be Layer0-mutated and still be treated as an output-reduce
  candidate in the summary path. The live error followed a compacted
  `search_output` turn, so the safe contract is stricter: deterministic WSS
  Layer 0 mutation and output-reduce instruction injection never stack.
- Savings impact: read/git/test turns keep deterministic Layer 0 savings.
  Output-reduce remains eligible only for pure user-prompt WSS bodies. Search
  output moved to T324 fail-open after the live retest still reproduced upstream
  400s with output-reduce disabled.
- Product safety: this removes a model-facing instruction layer from already
  rewritten tool-output traffic and keeps debug records truthful for future
  live captures.

## Deviations

- None.
