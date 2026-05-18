# TASK 222: Raw scoped WSS frontdoor

Status: IMPLEMENTED PRE-LIVE
Priority: P0 after T221 prototype
Scope: Local Codex provider ingress; no global hosts/pfctl

## Why

The old transparent dispatcher read the first HTTP/1.1 Upgrade request as raw
bytes and wrote that header to upstream before entering `wsmitm.Session`. That
preserved header order, casing, subprotocol text, and other wire details much
better than reconstructing a request through `net/http`.

The current direct local WebSocket path uses `net/http`, then writes an upstream
request with `http.Request.Write`. That is stable, but it can change header
order/casing and default fields. For "as invisible as engineering allows",
scoped WSS needs the old raw-header property without reviving global
`/etc/hosts`.

This is the key task that turns scoped WSS from "functional" into
"old-transparent-path-grade". T221 makes WSS work; T222 makes WSS preserve the
wire shape tightly enough that it can become the preferred `auto` transport
after T224 proof.

## Acceptance

- Scoped local WSS ingress can read the raw HTTP/1.1 Upgrade header bytes from
  Codex before `net/http` normalizes them.
- The raw header is forwarded upstream with only required authority/path fixes,
  preserving header order, casing, `Sec-WebSocket-Key`, subprotocol list, and
  unknown headers.
- The raw frontdoor supports the local provider path used by
  `slimference codex run --transport=wss`.
- The raw frontdoor still rejects or downgrades safely when the request is not a
  valid Codex conversation WSS upgrade.
- After upstream returns `101 Switching Protocols`, bytes enter the same
  `wsmitm.Session` Phase-F path from T221.
- Fallback path exists: if raw parsing fails, route through the existing stable
  tunnel or direct HTTP mode; never block Codex by default.
- Tests compare raw input header bytes to upstream header bytes for order and
  field preservation.
- The raw frontdoor is implemented and used for scoped WSS. WSS `auto`
  promotion is now gated on the version-bound T226 cert; T224 live capture
  remains the separate indistinguishability audit. The older `net/http`
  WebSocket tunnel stays as fallback/legacy.

## Sub-Tasks

- [x] Factor the transparent dispatcher's `readHTTPHeader` and parser into a
  reusable raw Upgrade ingress helper.
- [x] Build a local listener/handler path that can bypass `net/http` only for
  Codex WSS upgrades.
- [x] Decide whether this is integrated into the existing port `8990` listener
  or a dedicated local loopback frontdoor. Preferred: one user-facing port
  unless code evidence proves impossible.
- [x] Preserve raw header bytes with minimal path/host rewriting.
- [x] Prove Go `http.Request.Write` is not used for the raw WSS path.
- [x] Add header-order, header-casing, duplicate-header, and unknown-header
  tests.
- [x] Add failure-mode tests for malformed/non-Codex fallback, oversized
  header handling via shared parser tests, upstream 4xx/non-101 forwarding,
  upstream read failure, buffered post-101 bytes, and shutdown/client close.
- [x] Document why this exists: not for speed, for provider-side
  indistinguishability.
- [x] Add transport-selection docs: raw scoped WSS is the intended final WSS
  engine, `net/http` WSS is a fallback.

## Notes

Benefit compared with T221 alone:

- Reduces provider-visible HTTP Upgrade drift.
- Brings scoped WSS closer to the old transparent raw dispatcher.
- Makes WSS viable as a future default instead of only a lab transport.

Benefit compared with old global transparent path:

- Keeps the raw-header property but avoids machine-wide `chatgpt.com` routing.

Known limit:

- Local provider mode still means Codex intentionally connects to
  `127.0.0.1`; OpenAI does not see that local hop, but the upstream TLS/HTTP
  leg still comes from Slimference. T223 handles that leg.
- T226 certification is required before `transport=auto` can prefer WSS. T224
  remains the capture/diff evidence gate for provider-visible drift claims.

Implementation notes:

- `Proxy.Start` wraps the existing loopback listener with
  `rawScopedWSSListener`; no second public port was added.
- Only `GET /backend-api/codex/responses` WebSocket upgrades whose offered
  subprotocol list includes `responses_websockets` are intercepted. Every
  other request is replayed byte-for-byte to the normal `net/http` server via
  `prefetchedConn`.
- The raw path calls `WebSocketTunnel.ServeRawUpgrade`, writes the captured
  request header directly to the upstream connection after Host/request-target
  normalization, then enters the same `wsmitm.Session` Phase-F bridge after
  upstream `101 Switching Protocols`.
- `Sec-WebSocket-Protocol` parsing now preserves the full offered list, so the
  raw gate does not depend on `responses_websockets` being listed first.

Verification:

- `go test ./internal/proxy ./scripts/utils/indist_probe -run 'TestRawScoped|TestRewriteRaw|TestReadAndParseHTTPHeaderEdges|TestParseTSharkJSONSyntheticWSSCapture|TestProxyStartUsesRawScopedWSSListener' -count=1 -timeout 120s`
