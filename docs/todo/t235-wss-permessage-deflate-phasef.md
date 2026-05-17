# TASK 235: WSS permessage-deflate Phase-F mutation

Status: QUEUED
Priority: P0 before T226 WSS auto-promotion
Scope: `internal/wscompact`, `internal/proxy/wsmitm`, raw scoped Codex WSS,
T224 certification

## Why

T234 removed the false-positive parser degradation: Codex 0.130 scoped WSS
sessions now complete with `parse_failures=0` and `degraded_sessions=0`.
However, live WSS retries still record `frames_reencoded=0` because Codex
negotiates compressed WebSocket payloads (`permessage-deflate`, RSV1). The
current frame bridge correctly forwards those frames byte-equal, but it cannot
inspect or mutate the JSON envelope inside them.

This is now the last WSS product-value blocker: WSS transport is functional and
safe, but it does not save tokens until Slimference can decode, run Phase-F, and
re-encode compressed text messages without breaking RFC 6455 or the negotiated
extension state.

## Target State

- Preserve the raw scoped WSS frontdoor and header-order/casing work from T222.
- Do not strip `Sec-WebSocket-Extensions` as the default strategy. Header
  stripping would make parsing easier but increases provider-visible drift and
  moves away from the old transparent-MITM parity target.
- Parse RSV1 compressed text messages only when the handshake negotiated a
  supported `permessage-deflate` profile.
- Maintain independent compression state per direction when context takeover is
  negotiated.
- If no-context-takeover is negotiated, reset compressor/decompressor state at
  message boundaries.
- Reassemble fragmented compressed messages before Phase-F mutation.
- Let control frames interleave without corrupting compression state.
- Recompress every compressed data message that flows through an active
  mutation-capable direction, even if the handler leaves it unchanged, so
  context takeover state stays coherent for later mutated messages.
- Preserve byte-equal passthrough for compressed profiles Slimference does not
  understand.
- Never count unsupported compressed passthrough as `parse_failures` or
  `degraded_sessions`.
- Keep `frames_reencoded` as the real mutation signal, not merely "compressed
  bytes were rewritten"; add a separate telemetry field only if operator
  visibility needs it.

## Engineering Plan

1. Handshake capture and profile parsing:
   - Extract request and response `Sec-WebSocket-Extensions` from the raw
     Upgrade exchange.
   - Add a tiny parser for `permessage-deflate` parameters:
     `client_no_context_takeover`, `server_no_context_takeover`,
     `client_max_window_bits`, `server_max_window_bits`.
   - Treat unknown extension tokens or unsupported window bits as
     passthrough-only.
2. Frame model:
   - Split `wscompact.Frame.RSV` into explicit `RSV1`, `RSV2`, `RSV3` while
     preserving the current aggregate helper for existing callers.
   - Extend frame writing so re-encoded frames can set RSV1 intentionally.
   - Keep tests proving RSV bits, masking, extended lengths, fragmentation, and
     write-error branches.
3. Deflate codec:
   - Implement RFC 7692 raw-deflate message inflate/deflate with the
     `00 00 ff ff` sync-flush tail handling required by permessage-deflate.
   - Support both context takeover and no-context-takeover. Context takeover is
     the important path because live Codex frames showed stateful compression
     behaviour during ad-hoc diagnostics.
   - Keep codecs direction-scoped: client-to-server and server-to-client never
     share state.
4. Session integration:
   - Decompress complete text messages before `wsmitm.Parse`.
   - Run the existing `FrameHandler` / Phase-F adapter on the decompressed
     envelope.
   - Recompress with the same negotiated direction profile and write RSV1 text
     frames back to the destination.
   - For fragmented messages, preserve legal control-frame interleaving and
     emit a valid fragmented compressed message.
5. Safety and fallback:
   - On codec initialization failure, unsupported extension profile, or
     decompression error: forward byte-equal without poisoning the session.
   - Only malformed decompressed JSON envelopes may increment `parse_failures`
     and degrade the session.
   - Do not write `~/.slimference/codex-wss-cert.json` from unit tests or by
     hand.

## Acceptance

- Unit tests cover:
  - permessage-deflate parameter parsing for native Codex-like headers;
  - unsupported extension profile fallback;
  - RSV1 read/write preservation;
  - stateless/no-context compressed envelope mutation;
  - context-takeover compressed envelope mutation across multiple messages;
  - fragmented compressed message reassembly and re-emission;
  - interleaved ping/pong control frames;
  - malformed decompressed object-shaped JSON degrades fail-open;
  - compressed non-envelope text is forwarded without parse/degrade.
- Focus tests pass:
  `go test ./internal/wscompact ./internal/proxy/wsmitm ./internal/proxy -count=1`.
- Full gates pass:
  `go test ./... -count=1 -timeout 300s`,
  `go vet ./...`,
  `go run ./scripts/ci`.
- Live scoped WSS retry with Codex CLI 0.130, no global lab and no hosts/pfctl:
  - response exits 0 and returns the requested sentinel;
  - `parse_failures=0`;
  - `degraded_sessions=0`;
  - `frames_reencoded>0` on a prompt that creates a mutation candidate;
  - `byte_bridge_only=false`;
  - `mutation_active=true`;
  - `stop_seq_injections=0`;
  - `~/.codex/config.toml` is bit-identical after `slimference disable`.
- Only after the live acceptance is reproducible across at least two clean runs
  may T226 write certification through the existing certification path.

## Sub-Tasks

- [ ] Parse negotiated `permessage-deflate` profiles from raw WSS handshakes.
- [ ] Split RSV frame bits and add RSV1-capable frame write support.
- [ ] Add permessage-deflate inflate/deflate codec with context takeover.
- [ ] Integrate compressed message decode/re-encode into `wsmitm.Session`.
- [ ] Add focused unit tests for codec, fragmentation, control frames, and
  fail-open branches.
- [ ] Run full gates.
- [ ] Rebuild/install/restart daemon.
- [ ] Run live scoped WSS mutation proof and append operation-log evidence.
- [ ] Leave T226 blocked unless live certification criteria pass twice.

## Notes

- This task is not allowed to solve the problem by disabling WebSocket
  compression globally. That is a debug/lab fallback only.
- Recompressing unchanged compressed frames is acceptable inside an active
  mutation bridge if context takeover requires it. Token payloads are already
  non-byte-identical once Phase-F mutates; the goal is transport compatibility
  plus minimized header/TLS drift, not impossible byte-identical upstream
  content.
- The old transparent SNI surface would face the same compressed-frame blocker
  if it preserved native permessage-deflate. Scoped WSS did not create the
  issue; it made the issue visible without affecting Browser ChatGPT or
  ChatGPT.app.

## Deviations

- None.
