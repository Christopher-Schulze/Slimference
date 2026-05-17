# TASK 188: Responses-API WebSocket conversation MITM wire

Status: PLANNING 2026-05-16
Priority: P0 (the load-bearing technical piece of Phase G)
Scope: new `internal/proxy/wsmitm/`, `internal/wscompact/` (existing - extend),
       `internal/proxy/transparent_listener.go` (new), wires Phase F handlers
       (T165/T166/T167/T170/T174/T169/T183/T184/T185/T186) into the live
       responses_websocket stream

## Why

Codex 0.130 carries the model conversation over `wss://chatgpt.com/backend-api/
codex/responses` with the `responses_websockets=2026-02-06` protocol. The
Responses API runs as a binary-ish framed WebSocket stream rather than HTTPS
POST with SSE. To apply Slimference's Phase F transforms to that traffic, we
must terminate the WSS at our transparent :443 listener, decode each frame
into a request/response envelope, run it through the existing pipeline, and
re-encode + forward to the real upstream.

This file specifies the wire-level engine. The router that decides "this
connection is a responses_websocket conversation, dispatch to wsmitm" lives in
T189.

## Wire shape (source-verified)

From `openai/codex` Rust sources read 2026-05-16:

- Client opens TLS to `chatgpt.com:443` with ALPN `h2,http/1.1`. WebSocket
  upgrade goes over HTTP/1.1 over TLS (RFC 6455).
- Request line: `GET /backend-api/codex/responses?... HTTP/1.1`
- Required headers seen:
  `Host: chatgpt.com`
  `Upgrade: websocket`
  `Connection: Upgrade`
  `Sec-WebSocket-Version: 13`
  `Sec-WebSocket-Key: <base64-16>`
  `Sec-WebSocket-Protocol: responses_websockets=2026-02-06`
  `Authorization: Bearer <chatgpt token>` (from auth.json)
  `User-Agent: codex_cli_rs/0.130.0 (...)` or `codex_desktop_app/...`
  `OAI-Product-SKU: <sku>`
  `x-codex-ws-stream-request-start-ms: <ms>`
  `x-responsesapi-include-timing-metrics`
- Server responds `101 Switching Protocols` with matching `Sec-WebSocket-
  Accept` and `Sec-WebSocket-Protocol: responses_websockets=2026-02-06`.
- Frames are JSON envelopes (RFC 6455 text frames).
  - `{"type":"request","id":"<req-id>","body":{...Responses-API-request...}}`
  - `{"type":"response.event","id":"<req-id>","seq":N,"event":{...SSE-event...}}`
  - `{"type":"response.end","id":"<req-id>"}`
  - `{"type":"ping"}` / `{"type":"pong"}` (RFC 6455 control + app-level)
  - error frames carry `{"type":"error",...}` with `usage` carry-overs on
    finalization.

(Exact field names live in `openai/codex` `codex-rs/codex-api/src/endpoint/
responses_websocket.rs` and `codex-api/src/wire.rs`. The implementation must
parse what's actually on the wire from a fresh capture, not guess.)

## Target state

1. New package `internal/proxy/wsmitm/` with:

   ```go
   type Session struct {
       SessionID string
       Provider  types.Provider
       Cohort    qualityab.Cohort
       startedAt time.Time
   }

   // Serve takes the upgraded client connection (post-TLS-handshake,
   // post-101) and the upstream WSS connection, mediates the two until
   // either side closes.
   func (s *Session) Serve(ctx context.Context, client *websocket.Conn,
                          upstream *websocket.Conn,
                          counters *proxy.OutputReduceCounters) error
   ```

2. Frame interpreter (`wsmitm/frames.go`):
   - Decode each text frame into an `Envelope` struct with a discriminator
     field (`Type string` + raw `Payload json.RawMessage`).
   - Pass control frames (`ping`/`pong`) through untouched.
   - Mutate `request` frames inbound: extract `body` Responses-API JSON,
     run `outstop.MergeIntoBody`, `beterse.Inject` (cohort-gated), apply
     `staleread.AgeMessages` + `staleread.PruneObsoleteReads` to the input
     items, optionally apply other input-side transforms. Re-encode.
   - Mutate `response.event` frames outbound: SSE-shaped events flow through
     the same logic as `streamingRelayWithCutter`. Watch text deltas via
     `streamcut.Cutter` (3-line holdback). On cutter fire: drop holdback,
     emit synthetic `response.event` matching upstream's end-of-message
     event, send `response.end` to client, close the upstream connection.
   - `response.end` frame: trigger repdet pass over accumulated assistant
     text, write rewritten frames to client. Update qualityab outcome.
   - `usage` carry-overs: forward verbatim (we don't lie to the model
     about token billing).
   - Telemetry: increment `output_reduce_counters` at each fire site -
     same counters as the HTTP path.

3. Frame integrity guarantees:
   - Strict ordering preserved (a `response.event` with `seq=5` always
     reaches the client after `seq=4`).
   - When we drop frames (cutter fire), we synthesise a closing event so
     the client state machine doesn't get stuck waiting.
   - If our compression mutates the request body, the upstream sees a
     consistent body shape (re-marshal the whole envelope, not just the
     mutated field).

4. Reconnect / resume semantics:
   - Codex 0.130 attempts session resume after transient disconnects with
     a `last_seq` parameter. The MITM must remember the last seq it sent
     to the client and the last seq it forwarded to upstream so a resume
     replays missing events.
   - Per-session state stored in memory only; on daemon restart, in-flight
     sessions are dropped cleanly (client sees a clean close, retries).

5. Bypass on parse failure:
   - If the frame interpreter fails to decode any frame (new field, schema
     drift), the session immediately downgrades to pure-tunnel mode:
     subsequent frames are byte-copied client↔upstream without inspection.
     Telemetry counter `wsmitm_bypass_count` increments. Operator sees this
     in `/admin/status`.

6. Concurrency: each WebSocket session runs in its own goroutine. The
   counters update path is atomic. Frame parsing must not block the relay -
   a slow goroutine for token counting offloads via a buffered channel.

## Acceptance

- Capture a real Codex 0.130 → chatgpt.com WSS turn (mitmproxy or
  Wireshark with the right keylog). Replay through `wsmitm.Session.Serve`
  with a fake upstream. Output to fake client must be valid Responses-API
  frames; if no Phase F mechanism fires, output is byte-equal to input
  (modulo header order from re-marshal).
- 100% statement coverage on the frame parser and envelope re-marshaller.
- Live test on a real Codex 0.130 + ChatGPT-Sub session: model receives
  injected `stop_sequences`, streamcut fires on a commentary tail, repdet
  rewrites a verbatim echo. Counters increment. No client-side error.
- Schema drift test: a frame with an unknown `type` value forces session
  downgrade to pure tunnel. The test confirms the downgrade activates and
  the rest of the session flows untouched.
- Performance: p50 added latency per frame ≤ 1 ms, p95 ≤ 5 ms.

## Sub-Tasks

- [ ] Capture a Codex 0.130 WSS conversation for replay tests (operator
      task with mitmproxy + custom CA).
- [ ] Sketch `Envelope` struct from the capture; iterate until
      round-trip-marshalling is byte-equal.
- [ ] `frames.go` + tests with the recorded corpus.
- [ ] `wsmitm/session.go` with `Serve`; wire counters.
- [ ] Hook into Phase F handlers; reuse the same code paths as the HTTP
      path (do not duplicate compression logic).
- [ ] Reconnect / resume handler + tests.
- [ ] Schema-drift bypass test.
- [ ] Live single-turn test against real chatgpt.com (operator).
- [ ] Latency benchmark vs direct chatgpt.com baseline.

## Notes

- Existing `internal/wscompact/` carries a "shape registry" for tracking
  observed WebSocket shapes. T188 extends it with the
  `responses_websockets=2026-02-06` shape.
- A bypass mode is mandatory because OpenAI will iterate the protocol;
  we must fail open, never block traffic on schema drift.
- Phase F mechanisms run in the same order as the HTTP path:
  stop-seq → server-state rewrite → be-terse (cohort-gated). Output side:
  streamcut delay-buffer → repdet rewrite. Order matters and is documented
  in `internal/proxy/handler.go` for the HTTP path; wsmitm must match.

## Deviations

(none yet)
