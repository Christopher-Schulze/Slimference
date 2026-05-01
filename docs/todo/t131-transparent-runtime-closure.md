# TASK 131: Transparent-mode runtime closure + live proof

Status: CODE-COMPLETE / LIVE-PROOF PENDING (opened 2026-05-01)
Priority: P0
Scope: `internal/proxy/proxy.go`, `internal/proxy/connect.go`, `internal/proxy/ws.go`, `internal/proxy/mitm_response.go`, `internal/config/`, `cmd/slimference/proxy_cmd.go`, `docs/todo/t122-transparent-mode.md`, `docs/transparent-mode.md`.
Driver: T122 landed useful transparent-mode components, but the repository audit found the task was marked stronger than the code proves. The project needs the actual daemon/proxy startup path to run CONNECT/MITM/WebSocket interception, streaming-safe inner dispatch, and a live macOS proof before T123 stealth work or any "transparent mode works" claim.

---

## Problem

T122 currently has component coverage:

- CA generation and signer package exist.
- CONNECT/MITM handler exists and is unit-tested.
- WebSocket tunnel helper exists and is unit-tested.
- macOS `networksetup`, keychain, launchd, and `slimference proxy` CLI exist.

The missing proof was the runtime seam:

- `proxy.New` / daemon startup now attaches the CONNECT interceptor when transparent mode is enabled and CA init succeeds.
- Transparent config now drives signer directory, allowlist, audio-bypass paths, and TLS profile decisions.
- WebSocket upgrade handling is reachable from the CONNECT/MITM request path.
- MITM inner response writing now supports `http.Flusher`; non-streaming responses still get deterministic `Content-Length`, while streaming responses are explicitly connection-close framed.
- WebSocket upgrades preserve bytes already buffered after the HTTP upgrade headers, so early client frames are not lost when the request parser has read ahead.
- `proxy status` now detects when macOS points at `127.0.0.1:8990` but the daemon is down and prints `slimference proxy disable`.
- Manual live proof must cover Codex Desktop, Browser-Use passthrough, microphone bypass, disable, and uninstall.

## Target state

Transparent mode is certified only when a fresh macOS install can run:

```
slimference proxy install
slimference proxy enable
slimference proxy status
# Codex Desktop normal text session
# Browser-Use opens a non-LLM HTTPS website
# microphone transcription still works
slimference proxy disable
slimference proxy uninstall
```

and the evidence shows:

- allowlisted LLM hosts go through CONNECT/MITM and reach the existing compression path;
- non-allowlisted hosts are raw TCP passthrough;
- WebSocket requests are routed correctly;
- streaming output is not buffered into a single response;
- WebRTC/UDP audio remains untouched because SOCKS is not set;
- the machine returns to direct mode after disable/uninstall.

## Implementation plan

### WP1 - Config truth

- [x] Add `[transparent]` config section with `enabled`, `intercept_hosts`, `cert_cache_size`, `ca_dir`, `audio_bypass_paths`, `default_tls_profile`, and `[transparent.tls_profiles]`.
- [x] Defaults keep transparent mode off unless explicitly enabled.
- [x] `slimference proxy status` prints effective transparent runtime state and per-host TLS profile mapping.

### WP2 - Proxy startup wiring

- [x] In `internal/proxy/proxy.go`, wrap the root handler with `NewConnectInterceptor` when transparent mode is enabled and a signer is available.
- [x] Ensure direct-mode semantics stay unchanged when transparent mode is disabled (`DialTLSContext` remains nil).
- [x] Add in-process tests proving CONNECT reaches the live server handler when transparent mode is enabled.

### WP3 - Streaming-safe MITM dispatch

- [x] Audit `internal/proxy/mitm_response.go` for buffering that would break SSE / streaming responses.
- [x] Replace the buffered-only writer with a hybrid writer: buffered until `Flush`, streaming after `Flush`.
- [x] For streaming responses without `Content-Length`, force connection-close framing so clients have an unambiguous response boundary.
- [x] Add tests for implicit status, repeated flush, header/body write failures, streaming writes, and deterministic non-streaming `Content-Length`.

### WP4 - WebSocket reachability

- [x] Route `Upgrade: websocket` inside the MITM request path into `WebSocketTunnel`.
- [x] Add a test proving WebSocket upgrades bypass the inner HTTP handler and enter the tunnel path.
- [x] Preserve buffered post-header bytes when dispatching to the WebSocket tunnel.
- [x] Keep message-boundary WebSocket compression out of scope; tunnel correctness only.

### WP5 - Status and crash-safety

- [x] `proxy status` checks both macOS proxy settings and actual daemon reachability.
- [x] If system proxy points at Slimference but the daemon is unreachable, status prints `slimference proxy disable`.
- [x] Daemon reachability probe bypasses ambient HTTP proxy environment variables to avoid recursive false positives/negatives.
- [x] Auto-disable-on-shutdown remains best effort; crashes are handled by status detection and operator repair.

### WP6 - Live evidence

- Add a manual evidence checklist under this task file or `docs/transparent-mode.md`.
- Capture commands, timestamps, and observed results for:
  - `proxy install`
  - `proxy enable`
  - Codex Desktop text request
  - Browser-Use passthrough to non-allowlisted HTTPS
  - microphone transcription
  - `proxy disable`
  - `proxy uninstall`

## Acceptance criteria

- [x] Transparent config exists and is used by runtime proxy startup.
- [x] CONNECT/MITM handler is attached in the live proxy path when transparent mode is enabled.
- [x] Non-transparent/direct mode keeps existing behaviour byte-compatible.
- [x] WebSocket upgrade path is reachable and covered by an integration test.
- [x] WebSocket upgrade path preserves early buffered client bytes after the HTTP headers.
- [x] SSE / streaming responses are not buffered until completion.
- [x] Streaming MITM responses are connection-close framed when no length is known.
- [x] Non-allowlisted HTTPS hosts raw-relay successfully through existing CONNECT passthrough tests.
- [x] `proxy status` detects daemon-down / system-proxy-drift state.
- [ ] Manual macOS E2E checklist is completed and recorded.
- [x] `go test ./...` passes.
- [x] `go run ./scripts/ci` passes (8/8, total statement coverage 100.0%).
- [x] Focused race check passes for touched packages: `go test -race ./cmd/slimference ./internal/config ./internal/proxy ./internal/tlsdial`.
- [ ] Manual macOS E2E, optional TS tests, optional integration tags, and full `go test -race ./...` are pending; full race remains blocked by T132 until the known Layer 2 race is fixed.

## Out of scope

- T123 TLS fingerprint mimicry.
- WebSocket message-boundary compression beyond pass-through correctness.
- Windows/Linux transparent proxy support.
- Replacing config-patch integration mode; both modes continue to coexist.

## Validation

```
go test -race ./internal/proxy/... ./internal/transparent/... ./internal/tlsca/... ./cmd/slimference/...
go run ./scripts/ci
bun test tests/ts
go test -tags=integration ./tests/integration
```
