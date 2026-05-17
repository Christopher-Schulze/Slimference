# TASK 183: OpenAI / Codex repdet rewrite path

Status: TODO (planning 2026-05-16; closes T167 Deviation #1)
Priority: P1 (closes a documented Deviation from the T165/166/167 sprint)
Scope: `internal/proxy/repdet_wire.go` (new helper), `internal/proxy/handler.go` (provider switch), tests under `internal/proxy/`

## Why

T167 v1 ships repdet rewriting only for the Anthropic non-streaming path; OpenAI and Codex ChatGPT non-streaming responses pass through untouched. The user actually tests against Codex CLI, so the OpenAI/Codex wire is the one that matters in practice. Mirroring the Anthropic helper closes the deviation and unlocks the same 10-30% non-streaming output saving on the Codex path.

**Why:** Half of the user's real traffic flows through OpenAI/Codex - leaving it on the deviation list means the headline T167 win lands at half strength on the wire that gets used most.
**How to apply:** Add `passthroughOpenAIWithRepdet` mirroring the Anthropic helper. OpenAI Chat Completions responses carry text in `choices[i].message.content` (string); Codex Responses API uses `output[i].content[k].text`. Both shapes are stable, both can be re-marshalled losslessly.

## Target State

1. New `rewriteOpenAIResponseBody(body []byte, idx *repdet.Index) ([]byte, int)` in `internal/proxy/repdet_wire.go`, structurally identical to the Anthropic variant: parse, walk content, rewrite text fields, re-marshal, return saved byte count.
2. New `passthroughOpenAIWithRepdet(w, upstreamResp, messages, log)` that buffers + rewrites + writes with adjusted Content-Length.
3. Handler.go branch selection extended to dispatch on provider: Anthropic → Anthropic helper, OpenAI / Codex → OpenAI helper, else original `passthrough`.
4. End-to-end test: prompt carries a tool_result with verbatim echoed content, OpenAI upstream stub returns content containing that block, proxy serves marker-rewritten body.
5. 100% statement coverage on `rewriteOpenAIResponseBody`.

## Acceptance

- E2E test green: OpenAI response with echoed prompt block returns `[unchanged: …]` marker; raw block is gone.
- Toggle off (`RepetitionDetectionEnabled=false`): original body passes through untouched.
- Codex ChatGPT provider routes to the same OpenAI helper.
- No regression on existing Anthropic rewrite tests.
- Body shape for Responses API (`output[].content[].text`) parses cleanly; falls back to passthrough on shapes we don't recognise.

## Sub-Tasks

- [ ] `rewriteOpenAIResponseBody` for Chat Completions shape (`choices[].message.content`).
- [ ] Variant for Responses API (`output[].content[].text`).
- [ ] `passthroughOpenAIWithRepdet` helper.
- [ ] Handler dispatch extension.
- [ ] Unit tests: shape variants, malformed JSON, missing fields, type mismatches.
- [ ] E2E test mirroring `TestRepdetWiredRewritesAnthropicResponse`.

## Notes

- Chat Completions `message.content` can be `null` for tool-call-only responses - must handle the nullable string carefully.
- Codex ChatGPT routes through the same OpenAI wire format per `internal/types/types.go` provider enum.
- Marker shape stays `[unchanged: <name>]` - matches Anthropic for cross-wire consistency.

## Deviations

(none yet)
