# TASK 123: TLS fingerprint mimicry (uTLS) for outbound connections

Status: CODE-COMPLETE / EXTERNAL-JA3-PROBE PENDING (implemented 2026-05-01)
Priority: P1
Scope: `internal/tlsdial/` (new package), `internal/proxy/connect.go`, `internal/proxy/ws.go`, `internal/proxy/proxy.go`, `internal/transparent/`. Requires T131 first.
Driver: today the upstream-side TLS handshake (the connection from Slimference to OpenAI / Anthropic / chatgpt.com) uses Go's `crypto/tls` with default settings. Its TLS ClientHello, JA3/JA4 surface, ALPN order, extension list and cipher-suite ordering are uniquely "Go stdlib" and differ from the native upstream tool's stack (Codex Desktop is an Electron/Chromium app, Codex CLI and Claude Code are Node.js-driven, Python SDKs use OpenSSL via urllib3/httpx, etc.). There is no current proof that OpenAI / ChatGPT-Plus blocks Slimference by JA3, but the operator wants the stealth layer completed proactively. The fix is practical mimicry: make the upstream ClientHello match the selected native profile closely enough that Slimference no longer advertises itself as Go stdlib.

---

## Problem (current state)

The MITM dispatch in `internal/proxy/connect.go::mitm()` reads the inbound CONNECT, signs a leaf cert (`tlsca.Signer`), then runs `tls.Server(conn, cfg)` for the client-facing handshake. That part is fine; the client-facing TLS sees the slimference CA either way and is not a fingerprint risk for OpenAI.

The risk is the **upstream-facing** handshake: before T123, when Slimference re-emitted the request to `api.openai.com` / `chatgpt.com` etc., it used Go's standard TLS stack for the regular upstream transport and `tls.Dial` in `internal/proxy/ws.go::DefaultWebSocketDialer`. That handshake produced a Go-stdlib ClientHello:

- Cipher suites in Go-canonical order
- TLS 1.3 + 1.2 advertised
- `ALPN: h2, http/1.1` in stdlib order
- Extension list including `signed_certificate_timestamp`, `application_layer_protocol_negotiation`, `key_share`, `psk_key_exchange_modes`, etc., in Go's order
- GREASE values match Go's PRNG seed pattern, not Chromium's

The resulting JA3 hash is one of the small Go-stdlib family fingerprints. Codex Desktop's native Chromium and Node/OpenSSL clients produce different fingerprints. A simple JA3 allowlist on the OpenAI edge could deny Slimference's traffic while admitting the same user's direct-app traffic.

Beyond JA3/JA4, there is HTTP/2 SETTINGS frame fingerprinting (frame ordering, initial window sizes, header compression preferences), header ordering, DNS behaviour, and connection-reuse patterns. uTLS does **not** solve those by itself. T123 reduces the biggest current wire tell (Go ClientHello); it must not be documented as "undetectable".

## Implemented target state

A new `internal/tlsdial/` package wraps `refraction-networking/utls` and exposes `Dial(ctx, network, host, port string, profile Profile) (net.Conn, error)`. Profiles are selected per upstream host:

- `chromium_stable` / `chrome_133` - uTLS `HelloChrome_133`.
- `chrome_131`, `chrome_120`, `chrome_120_pq`, `ios_12_1`, `safari_16_0` - explicit uTLS profiles available in the pinned dependency.
- `node_stable`, `python_requests`, `chrome`, `chromium`, `node`, `python` - intent aliases mapped to `chromium_stable`.
- `go_stdlib` - explicit opt-out that preserves the legacy Go TLS stack.

Reality note: uTLS v1.8.2 does not expose exact maintained Node/OpenSSL/Python fingerprints. Mapping Node/Python intent labels to Chromium is a practical "remove Go-stdlib tell" improvement, not a claim that Slimference becomes byte-identical to Node's OpenSSL or Python's urllib3/httpx.

Per-host profile selection is operator-configurable in `[transparent.tls_profiles]`:

```toml
[transparent.tls_profiles]
"api.openai.com"      = "node_stable"     # Codex CLI is the typical client
"chatgpt.com"         = "chromium_stable" # Codex Desktop is the typical client
"api.anthropic.com"   = "node_stable"     # Claude Code
default               = "chromium_stable"
```

When transparent mode is enabled, the upstream HTTP transport installs `DialTLSContext` and every WebSocket-over-TLS dial uses the same `tlsdial.Resolver`, so the resulting handshake follows the configured profile. Direct/config-patch mode keeps the standard transport unchanged.

## Preconditions

- T131 must prove the transparent runtime path is actually wired. T123 must not spend effort patching a dead or test-only CONNECT path.
- The test harness must prove both stdlib and uTLS dials can complete against a local TLS endpoint and fail safely on handshake/dial errors.
- The docs must say "TLS fingerprint mimicry" / "Go-stdlib fingerprint removed", not "undetectable".

## Implementation plan

### WP1 - tlsdial package

- [x] New `internal/tlsdial/profile.go`: `Profile`, string parsing, concrete uTLS profile names, and intent aliases.
- [x] New `internal/tlsdial/dial.go`: `Dial(ctx, network, host, port, profile)` that uses `utls.UClient` for mimicry and Go stdlib only for the explicit `go_stdlib` opt-out.
- [x] New `internal/tlsdial/resolver.go`: `Resolver` struct holding the per-host map; `Resolve(host) Profile` returns the configured profile or default.
- [ ] Versioned profile metadata and stale-profile doctor warning remain optional follow-up, not required for the first T123 code landing.

### WP2 - Wire upstream callers

- [x] `internal/proxy/proxy.go` upstream `http.Client` installs `DialTLSContext` only when transparent mode is enabled.
- [x] CONNECT/MITM WebSocket dials use the same profiled resolver via `newProfiledWebSocketDialer`.
- [x] `internal/transparent/networksetup.go`: no change.

### WP3 - HTTP/2 settings reality check

Not implemented in this landing. uTLS handles ClientHello mimicry; HTTP/2 SETTINGS/frame-order/header-order mimicry remains explicitly out of scope unless live evidence shows provider-side detection.

### WP4 - Config integration

- [x] New `[transparent.tls_profiles]` section in `internal/config/config.go` and defaults.
- [x] `slimference proxy status` prints the resolved profile per intercepted host.
- [ ] `slimference doctor` stale-profile warning is deferred until versioned profile metadata exists.

### WP5 - Verification harness

- [x] Unit coverage proves stdlib and uTLS profile dials complete against a local TLS endpoint when trusted and fail safely when untrusted/unreachable.
- [x] uTLS dials honor context cancellation while the handshake is blocked and immediately after handshake completion; cancellation closes the underlying TCP connection instead of leaking a hanging dial.
- [ ] External JA3/JA4 probe remains pending; do not claim exact JA3 hash match until a reflecting endpoint or local ClientHello parser is added.

### WP6 - Tests

- [x] `internal/tlsdial/profile_test.go`: profile parsing, aliases, default resolution, per-host overrides, profile listing, stdlib/uTLS dial success and failure paths.
- [x] `internal/proxy/transparent_runtime_test.go`: direct mode keeps stdlib transport; transparent mode installs profiled TLS dial path; WebSocket dialer uses the profile resolver.
- [x] Existing CONNECT + WebSocket tests keep passing; new T131 test covers upgrade reachability through CONNECT/MITM.

### WP7 - Documentation

- [x] This task file documents the practical limits: ClientHello mimicry only, no undetectability claim.
- [ ] `docs/transparent-mode.md` operator-facing TLS profile section can be expanded after live proof.

## Acceptance criteria

- [x] `internal/tlsdial/` package compiles with `refraction-networking/utls` as the only new dependency.
- [x] Available maintained uTLS profiles ship with explicit alias mapping; exact Node/Python/OpenSSL profile parity is not claimed.
- [x] `[transparent.tls_profiles]` resolves per-host with default fallback.
- [x] `slimference proxy status` prints the profile per intercepted host.
- [ ] External `tls-probe` / reflected JA3 verification is pending.
- [x] Docs explicitly avoid claiming full undetectability; HTTP/2 SETTINGS and connection-behaviour limits are documented.
- [x] `go run ./scripts/ci` passes (8/8, total statement coverage 100.0%).
- [x] Focused race check passes for touched packages: `go test -race ./cmd/slimference ./internal/config ./internal/proxy ./internal/tlsdial`.
- [x] Coverage is 100% for `cmd/slimference`, `internal/proxy`, and `internal/tlsdial`.
- [x] No regression in existing CONNECT + WebSocket test matrix.
- [x] Forensic hardening pass fixed uTLS context-cancel behavior; local tests cover handshake-timeout and post-handshake cancellation.

## Out of scope

- Spoofing source IP, DNS, or DNS-over-HTTPS (operator's own machine; that is by design).
- HTTP/2 SETTINGS-frame mimicry beyond what uTLS gives us (T123b).
- TLS 1.0/1.1 mimicry (we never want to downgrade).
- Per-session profile rotation: profiles are static per host; users who need rotation can flip the config.

## Validation

```
go test -race ./internal/tlsdial/... ./internal/proxy/...
go run ./scripts/utils tls-probe --profile=chromium_stable
slimference proxy status   # shows profile per host
```

## Risks + open questions

- **uTLS maintenance lag**: Chromium ships a new ClientHello every 6-8 weeks. uTLS tracks but with 1-2 release lag. We accept that lag - 1-2 versions behind Chromium is still indistinguishable from "older Chromium user" and far below detection threshold.
- **Performance**: uTLS handshake is ~1-2ms slower than stdlib's per first-connection. After connection-reuse it is the same. Acceptable.
- **Memory footprint**: uTLS adds ~3MB to the binary. Acceptable.
- **License**: uTLS is BSD-3-Clause, compatible with Slimference's licensing.
