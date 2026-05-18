# TASK 235: WSS permessage-deflate Phase-F mutation

Status: DONE - live scoped WSS mutation proved twice; T226 promotion unblocked
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

This is now the last WSS transport-value blocker: WSS transport is functional
and safe, but it does not save tokens until Slimference can decode, run Phase-F,
and re-encode compressed text messages without breaking RFC 6455 or the
negotiated extension state.

Implementation note from the first live mutation proof: the codec can inspect
compressed frames and can re-encode mutated frames, but WSS streamcut by delta
blanking hung Codex CLI. That reducer is now explicitly out of T235 and split
to T236. T235 can still certify WSS Phase-F via request-side reductions,
stale/obsolete read pruning, BeTerse, and repdet; it must not rely on unsafe WSS
streamcut to satisfy `frames_reencoded>0`.

Final live proof showed the valuable WSS mutation path is request-side Layer 0
compaction on tool outputs. Codex Responses WSS splits the relevant state across
directions: model `function_call` metadata is emitted in server-to-client output
item frames, while later `function_call_output` payloads arrive in
client-to-server request frames. The Phase-F adapter therefore keeps a
session-local, in-memory tool-use map learned from server frames and applies it
when later request frames carry matching tool outputs.

## Target State

- Preserve the raw scoped WSS frontdoor and header-order/casing work from T222.
- Do not strip or rewrite `Sec-WebSocket-Extensions` as the default strategy.
  Header stripping would make parsing easier but increases provider-visible
  drift and moves away from the old transparent-MITM parity target.
- Parse RSV1 compressed text messages only when the handshake negotiated a
  supported `permessage-deflate` profile.
- Maintain independent compression state per direction when context takeover is
  negotiated.
- If no-context-takeover is negotiated, reset compressor/decompressor state at
  message boundaries.
- For context takeover, keep two rolling dictionaries per direction:
  one for inflating what the source peer actually sent, and one for deflating
  what the destination peer actually receives. Forwarded-unmodified messages
  still update the destination dictionary with their decompressed plaintext so a
  later mutated message can be compressed against the same history the
  destination has.
- Reassemble fragmented compressed messages before Phase-F mutation.
- Let control frames interleave without corrupting compression state.
- Forward unmodified compressed data frames byte-equal when the handler returns
  `replace=false`; only re-encode when Phase-F actually mutates. This preserves
  maximum transport fidelity while the dual-dictionary state remains coherent
  for later mutations.
- Preserve byte-equal passthrough for compressed profiles Slimference does not
  understand.
- Bound both the reassembled compressed payload and the inflated plaintext
  payload. Over-limit messages fail open to byte-equal forwarding and disable
  compressed mutation for that direction instead of risking unbounded memory.
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
   - Use `compress/flate` with explicit rolling dictionaries instead of a
     long-lived blocking inflate stream. Per-message `NewReaderDict` /
     `NewWriterDict` keeps message boundaries simple while still supporting
     context takeover through the last 32 KiB of plaintext.
   - Support both context takeover and no-context-takeover.
   - Keep codecs direction-scoped and role-scoped: client-to-server source
     inflate, client-to-server destination deflate, server-to-client source
     inflate, and server-to-client destination deflate never share state.
4. Session integration:
   - Decompress complete text messages before `wsmitm.Parse`.
   - Run the existing `FrameHandler` / Phase-F adapter on the decompressed
     envelope.
   - Recompress with the same negotiated direction profile and write RSV1 text
     frames back to the destination.
   - For fragmented messages, preserve legal control-frame interleaving and
     emit a valid compressed message. Prefer preserving the original data-frame
     count where practical; exact compressed byte lengths may change after
     mutation.
5. Safety and fallback:
   - On codec initialization failure, unsupported extension profile, or
     decompression error: forward byte-equal without poisoning the session.
   - On compressed or inflated message size-cap hits: forward byte-equal,
     record a compression error, block further compressed mutation for that
     direction, and keep the session non-degraded.
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
  - original-frame byte-equal forwarding with destination dictionary sync when
    `replace=false`;
  - fragmented compressed message reassembly and re-emission;
  - interleaved ping/pong control frames;
  - compressed and inflated message size caps fail open without parse/degrade;
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
  - `streamcut_fires=0` for WSS traffic until T236 passes;
  - `~/.codex/config.toml` is bit-identical after `slimference disable`.
- Only after the live acceptance is reproducible across at least two clean runs
  may T226 write certification through the existing certification path.

## Sub-Tasks

- [x] Parse negotiated `permessage-deflate` profiles from raw WSS handshakes.
- [x] Split RSV frame bits and add RSV1-capable frame write support.
- [x] Add permessage-deflate inflate/deflate codec with dual rolling
  dictionaries.
- [x] Integrate compressed message decode/re-encode into `wsmitm.Session`.
- [x] Add focused unit tests for codec, fragmentation, control frames, and
  fail-open branches.
- [x] Add compressed-payload and inflated-payload size caps with fail-open
  tests.
- [x] Run full gates.
- [x] Rebuild/install/restart daemon.
- [x] Run live scoped WSS mutation proof and append operation-log evidence.
- [x] Leave T226 blocked until the criteria passed twice; T226 may now consume
  the proof through the certification path.
- [x] Split unsafe WSS streamcut terminal behavior into T236 and keep it off in
  WSS Phase-F.

## Notes

- This task is not allowed to solve the problem by disabling WebSocket
  compression globally. That is a debug/lab fallback only.
- Do not use the shortcut "forward unchanged compressed frame, but ignore
  destination compression state". That breaks later context-takeover mutation.
  Forwarding raw is allowed only after inflating the message and updating the
  destination deflate dictionary with the same plaintext the destination peer
  will decode.
- Recompressing unchanged compressed frames is a fallback only if the
  dictionary-sync invariant cannot be upheld. It is worse for drift, so prefer
  raw forwarding plus dictionary observation.
- The old transparent SNI surface would face the same compressed-frame blocker
  if it preserved native permessage-deflate. Scoped WSS did not create the
  issue; it made the issue visible without affecting Browser ChatGPT or
  ChatGPT.app.
- Final T235 evidence: two independent scoped Codex CLI WSS runs returned
  `L0_GIT_OK`, exited 0, recorded `frames_reencoded=1`,
  `compressed_messages_mutated=1`, `phasef_mutations=1`,
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  `streamcut_fires=0`, `stop_seq_injections=0`, and kept
  `~/.codex/config.toml` bit-identical to the baseline SHA. Input token savings
  were 1059 and 1035 respectively on the two live runs.
- No `~/.slimference/codex-wss-cert.json` file was written by hand in T235.
  T226 owns recording certification state through the product certification
  path and promoting `transport=auto` to WSS for the certified version tuple.
- The stdlib `compress/flate` path intentionally supports only full 15-bit
  windows. If a future Codex build negotiates smaller `max_window_bits`,
  Slimference marks that extension profile unsupported and forwards compressed
  frames byte-equal. This avoids a disproportionate custom-deflate fork.

## Deviations

- WSS streamcut is intentionally disabled in this task even when the global
  output-reduce streamcut toggle is on. This is a safety deviation from the
  original T235 plan, justified by live Codex CLI hang evidence. HTTP/SSE
  streamcut remains unchanged.
