# TASK 184: Streamcut delay-buffer client-byte suppression

Status: TODO (planning 2026-05-16; closes T166 Deviation #1)
Priority: P2 (UX polish, no new token saving)
Scope: `internal/outstop/streamcut/` (new delay-buffer mode), `internal/proxy/streaming.go` (re-encoding hook)

## Why

T166 v1 closes the upstream connection once a trailing-commentary opener appears in the SSE stream, but the first ~10-15 bytes of the opener have already reached the client. The user sees half a "Hope this h…" before the stream terminates. The token saving is real - the model never finished generating commentary - but the client UX is a mid-word cut. A delay-buffer that holds back the most recent ~32 bytes of forwarded text and drops them on fire produces a clean client view: the opener never appears at all.

**Why:** Polish on the headline T166 win. Operators reading session logs see clean transcripts; users see no half-sentences.
**How to apply:** Re-encode SSE deltas as they flow through the relay: hold the last 32 bytes of accumulated text, only forward bytes older than the holdback window. On cutter fire, drop the holdback (don't forward) and emit synthetic terminator immediately.

## Target State

1. Extend `streamcut.Cutter` with a `HoldbackWindow int` field (default 32 bytes).
2. New API: `Cutter.Forward(line []byte) (emit []byte, ok bool)` - returns the bytes the relay should write downstream (may be a re-encoded SSE frame with shorter text content) and whether to continue.
3. Per-provider re-encoder: rebuilds a `content_block_delta` (Anthropic) or `choices[0].delta.content` (OpenAI) frame with the shrunk text.
4. On `Observe→fire`: drop holdback; emit synthetic terminator.
5. Streaming.go relay path uses `Forward` instead of forwarding line verbatim when cutter is non-nil.

## Acceptance

- Client transcript shows zero bytes of the matched opener after fire.
- Substantive content before fire reaches client byte-for-byte (just N bytes delayed).
- Latency penalty <5 ms per response.
- Existing TestStreamcutWiredClosesUpstreamOnCommentary still passes (opener acceptance loosened to "≤30 bytes leak").
- New test TestStreamcutDelayBufferSuppressesOpener: assert opener literally not in `rec.Body`.
- 100% coverage on the re-encoder paths.

## Sub-Tasks

- [ ] HoldbackWindow + Forward API.
- [ ] Anthropic SSE re-encoder (preserve event type + delta shape, shrink text).
- [ ] OpenAI Chat Completions re-encoder.
- [ ] OpenAI Responses API re-encoder (`response.output_text.delta`).
- [ ] Wire into streamingRelayWithCutter.
- [ ] Tests: re-encoding correctness, multi-byte UTF-8 safety, fire-on-incomplete-frame.

## Notes

- Latency cost: a single delta is only forwarded after the next one arrives - in practice, 5-20 ms of buffering. Acceptable since SSE is already latency-tolerant.
- Risk: re-encoding deltas changes the wire format byte-for-byte. Multi-byte UTF-8 sequences must not be split mid-codepoint - the holdback boundary must align to a UTF-8 boundary.
- Existing TestStreamcutDisabledLetsTailThrough must stay green (toggle off = legacy passthrough behaviour).

## Deviations

(none yet)
