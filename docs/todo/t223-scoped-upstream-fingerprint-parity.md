# TASK 223: Scoped upstream fingerprint parity

Status: PARTIAL - scoped profiled TLS wired; live parity proof pending
Priority: P0 after T221/T222
Scope: Upstream TLS/HTTP profile for scoped Codex paths

## Why

T123 introduced uTLS profile support, but the strongest profile wiring is tied
to transparent mode. Scoped Codex HTTP/WSS routes should not fall back to plain
Go stdlib TLS if the goal is maximum provider-side indistinguishability.

The maximum target is not to claim "undetectable". It is to prove and minimize
every provider-visible delta: TLS ClientHello, ALPN, HTTP version, WebSocket
Upgrade shape, header order, compression settings, timing, and connection reuse.

This task is what makes scoped WSS better than the old global transparent path
in operational safety while preserving or improving the upstream fingerprint
story. The old path had profile-aware transparent dials in some places; the
new path must apply the same discipline to every scoped Codex upstream leg.

## Acceptance

- Scoped Codex HTTP and scoped Codex WSS upstream dials both use
  configurable TLS fingerprint profiles, independent of global transparent mode.
- `codex run --transport=auto` reports which upstream profile it will use for
  HTTP and WSS before launching, at least in verbose/preflight output.
- Default profile for `chatgpt.com` is the closest verified Codex/Chromium/Rustls
  match available in the shipped catalogue.
- If a live capture proves Codex CLI uses a different TLS profile than
  `chromium_stable`, add a named `codex_cli_<version>` profile or closest
  documented alias.
- ALPN is chosen by transport reality:
  - WSS/HTTP Upgrade path must negotiate HTTP/1.1.
  - HTTP Responses path must match the live Codex baseline where feasible.
- DoH bypass remains available only where needed to avoid self-loop; scoped mode
  should prefer normal resolution when no hosts poisoning exists.
- `status --preflight` reports the active scoped Codex TLS profile and whether it
  is stale or unverified.
- Tests prove scoped mode uses the profile dialer even when
  `[transparent].enabled=false`.

## Sub-Tasks

- [x] Inventory current scoped HTTP and direct WSS upstream dial sites.
- [x] Remove the accidental coupling between profiled upstream TLS and
  `cfg.Transparent.Enabled`.
- [x] Ensure `newUpstreamTransport` uses profile-aware TLS for scoped Codex
  HTTP even when global transparent mode is disabled.
- [x] Make `WebSocketTunnel` use the same resolver and
  profile selection as normal HTTP upstream.
- [ ] Decide per-route ALPN from route type, not from global transparent state.
- [ ] Add active-profile telemetry to status/admin state.
- [ ] Add full profile-selection tests for HTTP scoped, WSS scoped, global
  transparent, and legacy direct paths.
- [ ] Add a tshark/indist probe checklist for JA3/JA4/ALPN parity.
- [x] Add regression tests proving plain Go stdlib TLS is not silently used for
  scoped Codex unless explicitly configured.

## Notes

Benefit compared with T220:

- Removes the biggest provider-visible "Go proxy" smell from scoped Codex.
- Lets WSS-first become serious rather than just "WebSocket over Go TLS".

Benefit compared with old global transparent path:

- Old transparent mode already used profile-aware dials in some paths. This task
  makes that strength available to scoped Codex without global routing.

Known limit:

- uTLS can approximate common browser/client ClientHello profiles, but exact
  Codex Rustls/native fingerprint requires live capture and may not be perfectly
  reproducible with available profile primitives.

## Implementation Update - 2026-05-17

Landed locally:

- `newUpstreamTransport` now always installs the profile-aware TLS dialer.
  Scoped HTTP no longer silently falls back to Go stdlib TLS just because
  `[transparent].enabled=false`.
- The scoped WSS tunnel was already using `newProfiledWebSocketDialer`; the
  T221 bridge keeps that dialer and adds Phase-F frame mutation after the
  upstream `101`.
- Focused tests cover the always-profiled HTTP transport and WSS dial path.

Still open:

- The active profile is not yet surfaced in `codex status` or
  `/admin/state`.
- ALPN is still profile/dialer driven, not tuned per route from a live Codex
  baseline.
- Exact Codex CLI/App fingerprint parity remains T224 live-capture work.
