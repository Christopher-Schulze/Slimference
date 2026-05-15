# TASK 142: Codex WebSocket message-boundary compression

Status: IN PROGRESS (T142a inspect-only frame parser landed 2026-05-14; shadow/mutation still blocked on live frame corpus)
Priority: P0
Scope: `internal/proxy/ws.go`, `internal/proxy/connect.go`, `internal/wscompact/`, `internal/flight/`, `internal/sessions/`, `cmd/slimference/proxy_cmd.go`, `cmd/slimference/debug_cmd.go`, `tests/fixtures/websocket_corpus/`, `docs/transparent-mode.md`.

## Why

The current Codex WebSocket path is intentionally a byte-for-byte tunnel. That is correct for product safety: it preserves Codex's internal protocol without pretending we understand message boundaries. It also means the largest current gap is real: WebSocket traffic can be routed and observed at connection level, but request JSON inside the stream is not compressed.

The target is not a blind frame mutator. The target is a staged, inspect-first WebSocket pipeline that only mutates known, versioned, proven-safe Codex request messages, and falls back to byte tunnel on anything ambiguous.

## Hard Reality

- WebSocket frames are not the API contract; Codex's internal message schema is.
- Codex may fragment JSON across frames.
- Codex may use binary frames, compressed WebSocket extensions, ping/pong, close codes, reconnects, ACKs, or event IDs.
- If Slimference changes frame order, timing, message IDs, response IDs, or ACK relationships, the session can fail in subtle ways.
- Therefore mutation mode is blocked until a live frame corpus proves the stable shapes.

## Target State

WebSocket support has four modes:

1. `tunnel`: current byte-for-byte behavior.
2. `inspect`: parse frames and record redacted shape metadata; no mutation.
3. `shadow`: compute would-compress deltas and quality checks; send original bytes upstream.
4. `mutate`: compress known request messages only when every gate passes.

Default remains `tunnel` until T146/T140 live evidence and this task's inspect/shadow acceptance are met.

## Work Packages

### WP1 - Frame pump extraction

- [x] Split the current tunnel loop into a reusable bidirectional frame pump when an inspector is attached.
- [x] Preserve existing byte-for-byte behavior as the default path.
- [x] Cover:
  - text frames.
  - binary frames.
  - continuation frames.
  - ping.
  - pong.
  - close.
  - large frames.
  - half-close and upstream errors.
- Keep buffered bytes from CONNECT/MITM request parsing intact.

### WP2 - Inspect-only parser

- [x] Add `internal/wscompact` with a parser that can:
  - reassemble fragmented text messages.
  - detect JSON object, JSON array, and non-JSON text.
  - record top-level keys without storing raw content.
  - identify candidate request-bearing messages.
  - reject compressed WebSocket extensions unless explicitly supported.
- [x] Unknown frames are always passthrough.

### WP3 - Redacted frame corpus

- Add `slimference debug websocket capture` or extend `debug flight export` for WebSocket shape capture.
- Store scrubbed fixtures under `tests/fixtures/websocket_corpus/`.
- Redaction must remove:
  - user text.
  - tool output content.
  - file paths.
  - auth/cookies/headers.
  - screenshots/images.
- Corpus keeps:
  - direction.
  - opcode.
  - fragmentation pattern.
  - JSON top-level shape.
  - message type/event type.
  - payload byte/token counts.
  - response/previous-response IDs if non-secret.

### WP4 - Shape registry and version gate

- Build a registry of known safe Codex WebSocket message shapes.
- Key by:
  - Codex CLI/App version if available.
  - host/path.
  - message type.
  - direction.
  - required fields.
  - mutation policy.
- If the version is unknown or shape mismatch occurs, use `tunnel`.

### WP5 - Shadow compression

- [x] Run a deterministic no-mutation shadow estimator on reconstructed JSON text in memory.
- [x] Do not send the compressed result.
- [x] Record:
  - would-compress input bytes/tokens.
  - applied layers.
  - expected net saving.
  - unsupported fields.
  - mutation blockers.
- Full HTTP request compression replay remains pending until a live frame corpus
  proves which WebSocket payloads are request-bearing Codex messages.
- This is the evidence bridge between tunnel safety and mutation mode.

### WP6 - Mutation mode

- Enable only for known request messages with:
  - stable schema.
  - no streaming ACK dependency inside the mutated content.
  - deterministic serialization.
  - size improvement above threshold.
  - no quality-risk flags from T149.
- Preserve:
  - message IDs.
  - response IDs.
  - event IDs.
  - ACK relationships.
  - ordering.
  - ping/pong/close behavior.
  - original opcode class where possible.

### WP7 - Fallback and kill switch

- Per-connection fallback to tunnel when:
  - parser error.
  - unknown critical frame.
  - provider returns protocol error.
  - close code indicates malformed data.
  - latency budget exceeded.
- Global flags:
  - `websocket_compaction_enabled`.
  - `websocket_inspect_enabled`.
  - `websocket_shadow_enabled`.
  - `websocket_mutate_enabled`.
  - `websocket_unknown_shape_policy=tunnel|fail_closed` with default `tunnel`.

### WP8 - Tests

- Unit tests for:
  - fragmentation.
  - continuation reassembly.
  - binary passthrough.
  - ping/pong passthrough.
  - close passthrough.
  - malformed JSON passthrough.
  - compressed extension rejection.
  - known-shape mutation.
  - unknown-shape tunnel fallback.
  - no mutation in inspect/shadow mode.
- Integration tests:
  - CONNECT/MITM WebSocket upgrade still reaches the tunnel.
  - buffered post-header bytes are preserved.
  - mutation does not reorder frames.

## Acceptance

- [x] Existing `tunnel` behavior remains byte-for-byte compatible and tested.
- [x] `inspect` mode records frame shapes without raw content.
- [x] `shadow` mode reports would-save numbers without sending mutated bytes.
- [ ] `mutate` mode is blocked until at least one live Codex WebSocket corpus is captured and replayed.
- [ ] Unknown or drifted message shapes fall back to tunnel, not best-effort mutation.
- [ ] Flight records show `websocket_tunnel`, `websocket_inspect`, `websocket_shadow`, or `websocket_compact`.
- [x] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- Short term: no direct token saving, but exact proof of whether WebSocket compression is worth doing.
- Medium term: if Codex WebSocket request frames contain the same conversation/tool payloads as HTTP responses calls, expected input saving can match the HTTP path: roughly 25-55% on tool-heavy sessions.
- Strategic: removes the need to force `supports_websockets=false` for compression-heavy CLI operation once mutation mode is proven.

## Explicit Non-Goals

- Do not install Chromium or any browser for this. `chromium_stable` in TLS code is a fingerprint profile, not a browser dependency.
- Do not claim "WebSocket compression works" until mutation mode is live-tested.
- Do not parse or inspect Browser-Use site traffic. Non-LLM hosts remain raw passthrough.
- Do not touch WebRTC/voice traffic.

## Implementation Notes

- 2026-05-14 T142a:
  - Added `internal/wscompact` with a 100%-covered RFC 6455 frame reader/inspector.
  - The inspector preserves raw frames while emitting redacted `FrameSummary` records with direction, opcode, FIN/masked flags, payload length, fragmented state, JSON top-level shape, sorted top-level keys, and message type from `type`/`event`/`method`.
  - Fragmented text messages are reassembled only for shape inspection; raw frame order and bytes are unchanged.
  - RSV/compressed-extension frames are marked as blockers via `inspect_note`, not parsed as JSON.
  - `WebSocketTunnel` still uses byte tunnel by default; inspect mode only activates when an inspector is explicitly attached.
  - Focus tests: `go test ./internal/wscompact ./internal/proxy`; `internal/wscompact` coverage is 100%.
- 2026-05-15 T142b:
  - `FrameSummary` now includes a content-free `shadow` block for text payloads.
  - JSON payloads get deterministic `json_compact` would-save bytes/tokens and
    applied-layer labels without changing the raw frame.
  - RSV/compressed-extension frames and non-JSON text frames emit explicit
    shadow blockers.
  - This is shadow evidence only; mutation remains blocked on live Codex frame
    corpus and known-shape replay.
  - Focus test: `go test ./internal/wscompact -cover`.
