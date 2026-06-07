# T322 Codex WSS Output-Reduce Post-Layer0 Tool Guard

## Why

Codex WSS output-reduce directives are safe only on real user-prompt turns. A
live Golem session showed an upstream `invalid_request_error` after Slimference
mutated a WSS request that contained tool/search output. The existing guard
blocked top-level and `response_item.payload.function_call_output` shapes before
Layer 0, but output-reduce ran after Layer 0. If Layer 0 compacted the tool
output first, the later output-reduce gate could no longer reliably see that the
turn was tool-output-derived.

## Acceptance

- WSS output-reduce must remain eligible for direct user-prompt request bodies.
- WSS output-reduce must be hard-blocked when the original request contains any
  normalized tool-result content.
- WSS output-reduce must also be hard-blocked when Layer 0 processed any
  tool-result block, even if the post-Layer0 body no longer looks like raw tool
  output.
- Layer 0 must still compact WSS tool/search/git/test output where policy allows
  it.
- The guard must not count blocked tool-output turns as output-reduce skipped
  candidates.
- Regression tests must cover a `response_item` tool-output turn that is first
  compacted by Layer 0 and therefore would have bypassed the old post-rewrite
  tool-output detector.

## Sub-Tasks

- [x] Preserve an original-request tool-output flag before WSS Layer 0 mutates
  the body.
- [x] Reuse normalized message tool-result detection for the original request.
- [x] Pass the original tool-output flag plus Layer-0 `ToolResultBlocks` into
  the WSS output-reduce gate.
- [x] Add a focused regression test for response-item tool output compacted by
  Layer 0 with output-reduce otherwise enabled.
- [x] Run focused proxy tests, full Go tests, CI, and reinstall the latest
  binary.

## Notes

- Root cause: output-reduce eligibility was decided on the post-Layer0 body. For
  tool-output turns this is the wrong truth source; the original request shape
  and Layer-0 stats are authoritative.
- The fix keeps the savings path intact: Layer 0 may still reduce tool output,
  but output-reduce cannot add model-facing instructions to tool-output frames.
- This is a product-safety guard, not a savings rollback. User-prompt turns keep
  output-reduce eligibility; tool-output turns stay on deterministic tool-output
  reducers only.

## Deviations

- None.
