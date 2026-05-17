# TASK 180: SSE chunk coalescing (latency / throughput improvement)

Status: TODO (planning 2026-05-16)
Priority: P3 (latency win, not direct token win)
Scope: `internal/proxy/streaming.go`

## Why

Anthropic and OpenAI emit fine-grained SSE deltas (sometimes 1-3 tokens per chunk). Each chunk crosses the proxy. Coalescing 10-20 deltas into larger frames before forwarding reduces the per-chunk overhead (parsing, write syscalls) and trims observed latency variance.

**Why:** Not a token saving but a real UX improvement on slower networks. Free side-effect: cleaner stream tracing for debugging.
**How to apply:** Aggregate deltas in a 5-20ms window; flush at end of window or on event boundary (whichever comes first).

## Target State

1. Configurable coalesce window: `[proxy] sse_coalesce_window_ms = 10` (default).
2. Always flush on token-boundary events (`message_stop`, `content_block_stop`).
3. Backpressure-safe: if the upstream is faster than we can flush, increase window adaptively.

## Acceptance

- A streaming response with 200 deltas shows ≤25 forwarded frames after coalescing.
- End-to-end latency unchanged or improved.
- No tokens lost.

## Sub-Tasks

- [ ] Window-based aggregator.
- [ ] Adaptive backpressure handling.
- [ ] Tests with synthetic SSE upstreams.

## Notes

- Pure infrastructure, no token impact.
- Important for UX, especially over high-latency links.

## Deviations

(none yet)
