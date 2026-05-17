# TASK 166: Streaming trailing-commentary cutter (post-stop-seq safety net)

Status: TODO (planning 2026-05-16)
Priority: P0
Scope: `internal/proxy/streaming.go` (or wherever SSE response transform lives), new `internal/outstop/streamcut/`, `internal/proxy/handler.go`

## Why

Stop-sequence engineering (t165) prevents the model from emitting trailing commentary at the API level. But:
- Phrase list is conservative — model may invent new openers we don't list.
- Multi-turn responses (esp. Codex chain-of-thought outputs) sometimes emit commentary in the middle of a stream that's still "alive" technically.
- API stop_sequences are capped at 4 (OpenAI) — we can list 4 phrases via the API; everything else needs streaming-side detection.

A streaming cutter inspects the SSE stream as it leaves Anthropic/OpenAI, watches for trailing-commentary patterns near the end of a generation, and severs the stream early. Combined with t165 this catches both pre-emption (model doesn't emit) and detection (model did emit, we cut).

**Why:** Backup belt-and-suspenders to t165. Catches whatever the API-level filter misses. Sub-millisecond on hot path because all matches are deterministic rolling-window regex.
**How to apply:** Wrap the SSE response stream. Sliding 256-char buffer over the latest text. When a registered pattern matches AND we are within ~200 chars of the latest message_stop event, close the stream cleanly and let the model think it finished naturally.

## Target State

1. New package `internal/outstop/streamcut/`:
   - `type Cutter struct{}` with `Wrap(r io.Reader) io.Reader`
   - Deterministic matcher: list of compiled regexes ("^\\s*Let me know.*$", "^\\s*Hope this.*$", "^\\s*Would you like.*$", "^\\s*Is there anything.*$"…)
   - Operates on the SSE `data:` payload accumulation; detects "newly emitted text" deltas and runs them through the matcher
   - When match → emit `data: {"type":"message_stop"}` (Anthropic) or `data: [DONE]` (OpenAI) and close
2. Configurable via `[compression.output_reduce] streamcut_enabled = true` (default on).
3. Telemetry: counter `streamcut_fired_count` per session.
4. Pattern library shared with t165 for consistency.

## Acceptance

- A response that ends with "Hope this helps!\n" gets the "Hope this helps!" line stripped before the user/client sees it.
- A response that *contains* "hope" mid-sentence is unaffected (line-start anchored).
- The cut is transparent: Codex receives a valid `message_stop` and treats the turn as complete.
- Zero impact on response latency (sub-ms overhead via pre-compiled regexes).
- 100% coverage on the new package.

## Sub-Tasks

- [ ] Audit `internal/proxy/streaming.go` to find SSE plumbing.
- [ ] New package skeleton + Cutter type.
- [ ] Pattern library shared with t165.
- [ ] Anthropic SSE parser (`event: content_block_delta` extraction).
- [ ] OpenAI SSE parser (`choices[0].delta.content` extraction).
- [ ] Trigger logic: only fire after detecting end-of-substantial-content (e.g. seen ≥100 chars of real content, then detected closing-fluff pattern).
- [ ] Graceful stream close: emit synthetic end-event matching upstream's protocol.
- [ ] Tests: end-of-stream patterns, mid-stream false-positive avoidance, multi-byte UTF-8 splits across SSE frames.
- [ ] Telemetry + docs.

## Notes

- The cutter must **never** swallow tokens the user requested. False-positive cost is high.
- Use sliding-window regex over decoded UTF-8 text, not raw bytes — multi-byte chars must not be split.
- Anthropic and OpenAI both emit `usage` stats on `message_stop`; we can opt to either pass through the upstream-reported usage or override with our streamed-byte count.
- For Codex responses API: the SSE shape differs (sequential `response.output_text.delta` events).

## Deviations

- **v1 cuts upstream generation, does not retroactively rewrite client bytes.** The acceptance line "Hope this helps!" stripped before the user/client sees it" is aspirational. v1 lets the first ~10-15 bytes of the opener reach the client, then closes the upstream HTTP body so further commentary is never generated (and we never pay for it). Real saving stays - the model would have continued past "Hope this helps! Let me know if you have any other questions about…" - we cap that at the opener.
- Achieving the fully-clean client-side cut would require a delay-buffer (~32 bytes) on every text delta, re-encoding each SSE frame downward in size. Deferred to a follow-up if user-visible polish becomes a priority.
