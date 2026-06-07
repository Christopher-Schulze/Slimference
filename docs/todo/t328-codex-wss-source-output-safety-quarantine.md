# T328 Codex WSS Source-Output Safety Quarantine

## Why

Scoped Codex CLI runs through Slimference still hit OpenAI WSS
`invalid_request_error` during real Golem inspection sessions after the Reconc
and search-output guards. The live evidence showed the failure followed a
Codex WSS `/backend-api/codex/responses` continuation chain with
`previous_response_id` and source-like tool output.

Reconc was not the root cause. Reconc outputs are already bypassed by Layer 0.
The unsafe class is mutating source-like WSS tool output inside a
server-state continuation where Codex/OpenAI can be stricter than the local
request normalizer.

## Acceptance

- Codex WSS Phase-F full-passes source-like tool output when the request carries
  `previous_response_id`.
- The guard is WSS-only and does not disable deterministic non-WSS reducers.
- Upstream WSS `error`, `response.failed`, or `response.incomplete` frames
  quarantine the current WSS adapter so the rest of that socket full-passes
  until reconnect.
- Edit/re-read observation still runs before the full-pass guard, so workflow
  recovery and recent-edit safety do not regress.
- Debug and flight records include content-free WSS shape facts for later field
  diagnosis.
- Focused WSS regressions and the full proxy package pass.

## Sub-Tasks

- [x] Identify the repeated live 400 class from Codex logs plus Slimference WSS
  decision records.
- [x] Add a `previous_response_id` plus source-tool-output full-pass gate.
- [x] Add WSS-session quarantine after upstream error frames.
- [x] Preserve edit/re-read observation before the full-pass decision.
- [x] Add redacted `debug_facts` to request and flight summaries.
- [x] Cover source full-pass, upstream-error quarantine, upstream-error logging,
  planner telemetry, and existing edit/read-delta behavior with Go tests.
- [x] Run the full proxy package gate.

## Notes

- Savings impact is intentionally narrow: WSS source-file tool outputs in
  continuation turns stop claiming Layer 0/read-delta/chunk savings until a
  future live capture proves that exact shape safe. This is not a global WSS
  savings rollback.
- Follow-up T329 narrowed this quarantine to large source-like tool results
  only. Small source snippets keep the normal route; the large continuation
  class remains byte-equal until live proof says otherwise.
- The product contract wins over local token savings: an optimization that can
  trigger upstream 400s is a workflow drawdown and cannot stay default-on for
  that shape.
- `debug_facts` are content-free counts and booleans such as
  `wss.previous_response_id`, `wss.source_tool_results`, and
  `wss.layer0_tokens_saved`. They do not store raw prompt, tool output, code, or
  auth material.
- Reconnect creates a fresh adapter and can leave quarantine. The source-output
  guard remains active independently of quarantine.

## Deviations

None.
