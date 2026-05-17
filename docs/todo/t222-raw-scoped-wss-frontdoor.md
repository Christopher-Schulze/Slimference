# TASK 222: Raw scoped WSS frontdoor

Status: PLANNED
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
- The raw frontdoor is used by WSS `auto` once it is proven; the older
  `net/http` WebSocket tunnel stays only as fallback/legacy.

## Sub-Tasks

- [ ] Factor the transparent dispatcher's `readHTTPHeader` and parser into a
  reusable raw Upgrade ingress helper.
- [ ] Build a local listener/handler path that can bypass `net/http` only for
  Codex WSS upgrades.
- [ ] Decide whether this is integrated into the existing port `8990` listener
  or a dedicated local loopback frontdoor. Preferred: one user-facing port
  unless code evidence proves impossible.
- [ ] Preserve raw header bytes with minimal path/host rewriting.
- [ ] Prove Go `http.Request.Write` is not used for the raw WSS path.
- [ ] Add header-order, header-casing, duplicate-header, and unknown-header
  tests.
- [ ] Add failure-mode tests: malformed header, oversized header, upstream 4xx,
  upstream partial 101, client close.
- [ ] Document why this exists: not for speed, for provider-side
  indistinguishability.
- [ ] Add transport-selection docs: raw scoped WSS is the intended final WSS
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
