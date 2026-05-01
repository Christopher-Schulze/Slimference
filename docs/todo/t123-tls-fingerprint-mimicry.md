# TASK 123: TLS fingerprint mimicry (uTLS) for outbound connections

Status: PENDING (planned 2026-05-01)
Priority: P1
Scope: `internal/tlsdial/` (new package), `internal/proxy/connect.go`, `internal/proxy/ws.go`, `internal/proxy/proxy.go`, `internal/transparent/`.
Driver: today the upstream-side TLS handshake (the connection from Slimference to OpenAI / Anthropic / chatgpt.com) uses Go's `crypto/tls` with default settings. Its TLS ClientHello, JA3 hash, ALPN order, extension list and cipher-suite ordering are uniquely "Go stdlib" and differ from the native upstream tool's stack (Codex Desktop is an Electron/Chromium app, Codex CLI is a Node.js binary, Claude Code is a Node.js binary, the Anthropic Python SDK is OpenSSL via urllib3, etc.). OpenAI / ChatGPT-Plus does not currently fingerprint-detect proxy traffic, but a single anti-abuse policy flip on their side would lock Slimference users out by JA3. The fix is to mimic the upstream tool's native ClientHello byte-for-byte so wire-level traffic is indistinguishable from the un-proxied case.

---

## Problem (current state)

The MITM dispatch in `internal/proxy/connect.go::mitm()` reads the inbound CONNECT, signs a leaf cert (`tlsca.Signer`), then runs `tls.Server(conn, cfg)` for the client-facing handshake. That part is fine; the client-facing TLS sees the slimference CA either way and is not a fingerprint risk for OpenAI.

The risk is the **upstream-facing** handshake: when Slimference re-emits the request to `api.openai.com` / `chatgpt.com` etc., it uses Go's standard `tls.Dial` (in `internal/proxy/ws.go::DefaultWebSocketDialer` and the standard `http.Client` transport built by `proxy.go::NewProxy`). That handshake produces a Go-stdlib ClientHello:

- Cipher suites in Go-canonical order
- TLS 1.3 + 1.2 advertised
- `ALPN: h2, http/1.1` in stdlib order
- Extension list including `signed_certificate_timestamp`, `application_layer_protocol_negotiation`, `key_share`, `psk_key_exchange_modes`, etc., in Go's order
- GREASE values match Go's PRNG seed pattern, not Chromium's

The resulting JA3 hash is `cd08e31494f9531f560d64c695473da9` (or similar - one of a small set of Go fingerprints). Codex Desktop's native Chromium produces JA3 `b32309a26951912be7dba376398abc3b`. Claude Code's Node fingerprint is different again. A simple JA3 allowlist on the OpenAI edge would deny Slimference's traffic while admitting the same user's direct-app traffic.

Beyond JA3, there is JA4, HTTP/2 SETTINGS frame fingerprinting (frame ordering, initial window sizes, header compression preferences), and connection-reuse patterns. A real anti-bot stack (Cloudflare-style) would catch all of these.

## Target state

A new `internal/tlsdial/` package wraps `refraction-networking/utls` and exposes `Dial(host, port string, profile Profile) (net.Conn, error)`. Profiles are per upstream-tool:

- `ProfileChromiumStable` - matches the latest Chromium stable release used by Electron-based apps (Codex Desktop, ChatGPT Desktop, slim browser-based clients).
- `ProfileFirefoxLatest` - for any app embedding Firefox / Servo (rare in the LLM space but supported for symmetry).
- `ProfileNodeStable` - matches Node.js's BoringSSL build used by Codex CLI, Claude Code, and most JS/TS LLM SDKs.
- `ProfilePythonRequests` - matches OpenSSL via Python's `requests` / `httpx` (Anthropic Python SDK, OpenAI Python SDK).
- `ProfileGoStdlib` - the current default; kept for explicitness so an operator who does not care about stealth can opt out.

Per-host profile selection is operator-configurable in `[transparent.tls_profiles]`:

```toml
[transparent.tls_profiles]
"api.openai.com"      = "node_stable"     # Codex CLI is the typical client
"chatgpt.com"         = "chromium_stable" # Codex Desktop is the typical client
"api.anthropic.com"   = "node_stable"     # Claude Code
default               = "chromium_stable"
```

When transparent mode is enabled, every upstream `tls.Dial` and every WebSocket-over-TLS dial routes through `tlsdial.Dial(host, port, resolveProfile(host))` so the resulting handshake matches the configured profile.

## Implementation plan

### WP1 - tlsdial package

- New `internal/tlsdial/profiles.go`: enum `Profile` with constants for the five built-in profiles plus `ProfileAuto` (selects per host based on the `[transparent.tls_profiles]` map). String-based parsing for config consumption.
- New `internal/tlsdial/dial.go`: `Dial(ctx, host, port, profile)` that builds a `utls.UConn` with the right `ClientHelloID` and runs `Handshake()`. Falls back to Go's stdlib `tls.Dial` when `Profile == ProfileGoStdlib` so the dependency is purely additive.
- New `internal/tlsdial/resolver.go`: `Resolver` struct holding the per-host map; `Resolve(host) Profile` returns the configured profile or default.

### WP2 - Wire upstream callers

- `internal/proxy/ws.go::DefaultWebSocketDialer` swaps to `tlsdial.Dial`.
- `internal/proxy/proxy.go` upstream `http.Client` for the regular request path: replace the default `http.Transport.DialTLSContext` with one that calls `tlsdial.Dial`. Keep the rest of the transport intact (HTTP/2 SETTINGS still come from net/http but we re-emit in matching order; full HTTP/2 fingerprint mimicry is T123b if needed).
- `internal/transparent/networksetup.go`: no change.

### WP3 - HTTP/2 settings emulation (T123b, deferred unless needed)

If a real-world OpenAI / Cloudflare deployment ever fingerprints HTTP/2, a follow-up task ports the SETTINGS frame ordering + `INITIAL_WINDOW_SIZE` + `MAX_CONCURRENT_STREAMS` + `HEADER_TABLE_SIZE` to match Chromium's defaults. uTLS handles TLS; HTTP/2 needs a separate layer (likely a fork of `golang.org/x/net/http2` with custom SETTINGS).

### WP4 - Config integration

- New `[transparent.tls_profiles]` section in `internal/config/config.go` and defaults.
- `slimference proxy status` extends to print the resolved profile per intercepted host.
- `slimference doctor` warns if `tls_profiles.default = go_stdlib` and transparent mode is enabled (the explicit "I do not care about stealth" opt-out).

### WP5 - Verification harness

- `scripts/utils/tls-probe`: tool that connects through Slimference to a JA3-reflecting endpoint (e.g. `tls.peet.ws/api/all` or self-hosted), prints the observed JA3, and asserts it matches the expected per-profile fingerprint.
- Operator can run `go run ./scripts/utils tls-probe --profile=chromium_stable --host=api.openai.com` and see the JA3 hash.
- CI integration: a synthetic `tls.peet.ws`-style mock running on a goroutine inside the test binary asserts profile fidelity per round-trip.

### WP6 - Tests

- `internal/tlsdial/profiles_test.go`: enum parsing, default resolution, per-host overrides.
- `internal/tlsdial/dial_test.go`: each profile's ClientHello bytes are byte-for-byte equal to a captured reference (golden-file test). Per-profile cipher-suite ordering, extension ordering, GREASE pattern checked.
- `internal/proxy/ws_test.go` and `internal/proxy/proxy_test.go`: existing tests keep passing; new tests assert that the upstream dialer goes through `tlsdial.Dial` rather than `tls.Dial` directly when transparent mode is on.

### WP7 - Documentation

- `docs/transparent-mode.md`: new "TLS Stealth" section explaining the per-host profile concept, the default settings, and the off-switch for operators who prefer Go stdlib (debugging clarity at the cost of a Go fingerprint).
- `docs/tls-stealth-rationale.md`: longer technical explanation of why JA3 mimicry matters, referenced from the operator-facing doc but separable.

## Acceptance criteria

- [ ] `internal/tlsdial/` package compiles with `refraction-networking/utls` as the only new dependency.
- [ ] Five profiles ship with golden ClientHello bytes; tests assert byte-equal.
- [ ] `[transparent.tls_profiles]` resolves per-host with `default` fallback.
- [ ] `slimference proxy status` prints the profile per intercepted host.
- [ ] `tls-probe` round-trips against the JA3-reflecting service and reports the matched fingerprint.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] No regression in existing CONNECT + WebSocket test matrix.

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
