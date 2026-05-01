# TASK 122: Transparent mode (system-wide HTTPS interception)

Status: COMPONENT-COMPLETE 2026-05-01; LIVE-CERTIFICATION REQUIRED (see T131)
Priority: P1
Scope: `internal/tlsca/` (new), `internal/transparent/` (new), `internal/proxy/proxy.go`, `internal/proxy/connect.go` (new), `cmd/slimference/proxy_cmd.go` (new), `cmd/slimference/main.go`, `internal/config/`, `docs/transparent-mode.md` (new). Follow-up runtime proof: `docs/todo/t131-transparent-runtime-closure.md`.

Driver: today every LLM client that wants to route through Slimference needs a per-tool config patch (Codex CLI's `~/.codex/config.toml`, Claude Code's settings, future tools). This breaks two real use-cases: (a) the Codex Desktop App, which speaks WebSocket-over-HTTPS to `chatgpt.com/backend-api/dev` and is non-trivial to redirect via app-config; (b) any tool the operator hasn't pre-configured. Plus: every per-tool patch is a hard dependency on Slimference daemon being up; if the daemon dies, the tool dies. Transparent mode replaces the per-tool patches with a single system-level intercept so any HTTPS-based LLM client routes through Slimference automatically, and a clean off-switch (`slimference proxy disable`) lets the operator drop back to direct OpenAI/Anthropic with no app-side change.

WebRTC (used by Codex Desktop for microphone transcription) bypasses System-HTTPS-Proxy by design (UDP/SRTP, ignores HTTP/HTTPS settings). That is the property that makes transparent mode safe for audio-streaming features: Slimference never sees them, latency stays native, no proxy code path touches them.

## Reality correction (2026-05-01 audit)

The T122 component commits landed useful building blocks, but the repository audit found this task was marked stronger than the code currently proves. Treat the state as **component-complete, not live-certified** until T131 closes these gaps:

- The running proxy startup path must visibly attach the CONNECT/MITM handler when transparent mode is enabled.
- The transparent config surface must be present and used by proxy startup, not only described in this plan.
- The WebSocket tunnel helper must be wired into the actual CONNECT/MITM request path, or explicitly documented as deferred.
- Streaming/SSE/WebSocket paths must be proven not to buffer model output incorrectly.
- `proxy status` must detect "system proxy points at Slimference but daemon/CONNECT path is not actually usable".
- A manual macOS E2E proof must cover `proxy install`, `proxy enable`, Codex Desktop traffic, Browser-Use passthrough, microphone bypass, `proxy disable`, and `proxy uninstall`.

Do not use T122 as evidence that transparent mode is production-ready until T131 is done.

---

## Problem (current state)

- `internal/proxy/proxy.go::NewProxy` builds an `http.Server` listening on `127.0.0.1:8990` that accepts plain HTTP (or HTTPS via TLS termination, when configured). Routes today: `/v1/messages` (Anthropic), `/v1/chat/completions` and `/v1/responses` (OpenAI), `/backend-api/codex/*` (ChatGPT-Plus), `/admin/*` (status surface).
- The `ServeHTTP` handler dispatches to `handler.go::Handle` which compresses and forwards to the upstream provider derived from `provider.go::DetectProvider`.
- There is no support for the HTTP `CONNECT` method, no on-the-fly TLS interception, no WebSocket-frame proxying, no notion of "system intercept mode".
- `cmd/slimference/integrate_cmd.go` patches per-tool configs (`~/.codex/config.toml`, `~/.claude/settings.json`) so the tool's HTTP client points at `127.0.0.1:8990`. Operator-visible setup that has to be redone for every new tool.
- Codex Desktop App at `/Applications/Codex.app` uses `wss://chatgpt.com/backend-api/dev` (transport `responses_websocket`), bypassing any HTTP-proxy config completely. WebSocket-over-HTTPS terminates at the `CONNECT` host before the WebSocket upgrade fires; today Slimference cannot terminate it, intercept the upgrade, or re-emit the upstream stream.

## Target state

A new `slimference proxy` subcommand tree provides the transparent-mode lifecycle:

```
slimference proxy install         # one-time: generate CA, trust in keychain, prep launchd
slimference proxy enable          # set system HTTPS proxy to 127.0.0.1:8990
slimference proxy disable         # clear system HTTPS proxy; daemon stays up
slimference proxy status          # show CA fingerprint, trust state, proxy state, intercepted hosts
slimference proxy uninstall       # remove from keychain, clear proxy, stop launchd
```

Behaviours:

- **CA generation**: `internal/tlsca/`. ECDSA P-256 root key + 10-year self-signed cert under `~/.slimference/ca/{root.key,root.crt}`. Per-domain leaf certs signed on-the-fly with 24-hour TTL, cached in-process LRU keyed by SNI.
- **Trust installation**: `cmd/slimference/proxy_cmd.go` shells out to `security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain` (with explicit `sudo` prompt, never silent) for system-wide trust, or `security add-trusted-cert -d -r trustRoot -k ~/Library/Keychains/login.keychain-db` for user-only. The CLI explains what is happening before invoking and prints the CA fingerprint so the operator can verify.
- **MITM HTTPS proxy with CONNECT**: `internal/proxy/connect.go`. `http.Server` is extended to handle `CONNECT host:port` requests by hijacking the TCP connection, completing a TLS handshake using a per-domain leaf cert signed by the slimference CA, then re-entering the existing `ServeHTTP` dispatch with the decoded request. WebSocket-upgrade requests inside the CONNECT tunnel are detected (Connection: Upgrade + Upgrade: websocket) and switched to a frame-level pass-through with optional compression on `responses` streams.
- **WebSocket proxying**: `internal/proxy/ws.go`. Once Upgrade is accepted, Slimference dials the upstream WebSocket (using the same TLS) and pumps frames in both directions. Layer 1/2 compression hooks into the message-boundary on `responses`-shaped streams; other streams (Realtime audio, anything else) pass through frame-perfect with no inspection beyond logging.
- **System-proxy control**: `internal/transparent/networksetup.go`. Wraps `networksetup -setsecurewebproxy "<service>" 127.0.0.1 8990`, `-setsecurewebproxystate "<service>" off`, `-getsecurewebproxy`. Iterates all configured network services so wired Ethernet + WiFi + Thunderbolt all flip together. Also writes a "previous state" record so `disable` can confirm-no-op when state was already off.
- **WebRTC bypass guarantee**: documented and tested. Slimference never sets a SOCKS proxy or any setting that affects UDP traffic; only `setsecurewebproxy` and `setwebproxy` (HTTP/HTTPS) are touched. Audio streams continue native.
- **launchd auto-start (optional)**: `internal/transparent/launchd.go`. Generates `~/Library/LaunchAgents/com.slimference.daemon.plist` so the daemon starts on login and on crash. `proxy install` offers this as an opt-in step. `proxy uninstall` removes it.
- **Crash safety**: signal handler (SIGINT/SIGTERM) clears the system-proxy setting before exit, so a Slimference crash does not strand all HTTPS connections. A second-line defence: `proxy status` notices "proxy points to 127.0.0.1:8990 but daemon not listening" and offers `proxy disable` as the fix.
- **Off-switch behaviour**: `proxy disable` clears the system setting via `networksetup`. Apps that were already connected through Slimference finish their current WebSocket / SSE stream and reconnect direct to OpenAI/Anthropic on next request. Cert stays trusted but is inert without the proxy setting.
- **Compression integration**: existing Layer 0/1/2 logic is unchanged. The transparent listener is just a different ingress; once a request lands in `handler.go::Handle`, the rest of the pipeline does not know whether it arrived via direct config-patch or via CONNECT-MITM. The provider-detection in `provider.go` works off `Host` header / path, both available after CONNECT is decoded.

## Implementation Plan

Eight WPs, each landing as its own commit. T122 is a multi-commit task by design so each layer's tests stay isolated.

### WP1 - TLS CA + per-domain signer (`internal/tlsca/`)

- `internal/tlsca/ca.go`: `LoadOrGenerateCA(dir string) (*tls.Certificate, error)`. Generates ECDSA P-256 root cert with `IsCA=true`, `KeyUsage=DigitalSignature|CertSign`, 10-year validity, common name `Slimference Local CA <fingerprint8>`. Stores `root.key` (mode 0600) + `root.crt` (mode 0644) under `<dir>/ca/`. Idempotent: existing files reused if valid; corrupted files rotated.
- `internal/tlsca/signer.go`: `Signer` struct with `Cert(host string) (*tls.Certificate, error)`. Generates a per-host ECDSA P-256 leaf key on first lookup (or cache miss), signs a 24-hour leaf cert with SAN matching the requested host (DNS-only, no IP at this stage), caches in an LRU bounded by `[transparent] cert_cache_size` (default 256). Cache is RW-mutex protected.
- `internal/tlsca/verify.go`: `Fingerprint(cert *tls.Certificate) string` returns the SHA-256 fingerprint of the root cert in the canonical hex-colon format that `security` CLI emits, so doctor / status output can show what the operator should expect to see in Keychain Access.
- Tests: 18 covering CA generation, idempotent reload, corrupted-key recovery, signer cache hits/misses, leaf-cert SAN matching, fingerprint format, concurrent signer access (race), TTL refresh on expiry.

### WP2 - CONNECT method + MITM dispatch (`internal/proxy/connect.go`)

- `internal/proxy/connect.go`: handler that fires when `r.Method == "CONNECT"`. Hijacks the underlying TCP connection via `http.Hijacker`, writes `HTTP/1.1 200 Connection Established\r\n\r\n`, wraps the connection in a `tls.Server` with a `GetCertificate` callback that consults the WP1 signer using the SNI from the ClientHello. After handshake, reads HTTP requests off the TLS connection and re-injects them into the existing dispatch (`p.ServeHTTP`) so Layer 0/1/2 compression runs unchanged.
- `internal/proxy/proxy.go`: `NewProxy` plumbs an optional `*tlsca.Signer` parameter; when nil, CONNECT returns 405 (preserves current direct-mode semantics). When set, CONNECT is honoured.
- Allowlist gate: `[transparent] intercept_hosts` (default `["api.openai.com", "api.anthropic.com", "chatgpt.com"]`). CONNECT requests for hosts not in the list pass through with a raw TCP relay (no MITM), so Slimference can be the system proxy without breaking iCloud, Github, sentry, etc.
- Tests: 14 covering CONNECT-allowed-host (MITM), CONNECT-other-host (raw relay), TLS handshake against the signer, HTTP/1.1 + HTTP/2 negotiation in the inner dispatch, hijack-not-supported error path, allowlist parsing.

### WP3 - WebSocket frame proxy (`internal/proxy/ws.go`)

- `internal/proxy/ws.go`: detects `Upgrade: websocket` inside an MITM dispatch, dials the upstream via `tls.Dial` to the original host on 443, performs the upgrade handshake against upstream with the original headers (modulo Provider-Invisibility: drop `Connection`, `Host`, `Sec-WebSocket-Key`-rewrite), then pumps frames bidirectionally.
- For streams matching the `responses` shape (path or first-frame heuristic), the downstream-to-upstream side runs through Layer 1/2 compression on message boundaries. Upstream-to-downstream is byte-for-byte (model output stays untouched).
- Realtime / audio / WebRTC-signaling streams are recognised by URL pattern (`/v1/realtime`, `/realtime`, paths containing `webrtc`) and bypass compression entirely.
- Tests: 18 covering: upgrade-passthrough on non-responses streams, compression on responses streams, header sanitisation, half-close (one side closes), upstream-rejected upgrade, connection error during pump, frame-too-large guard, audio path bypass.

### WP4 - System-proxy management (`internal/transparent/networksetup.go`)

- `internal/transparent/networksetup.go`: `Manager` struct with `EnableHTTPS(host, port)`, `Disable()`, `Status() Snapshot`, `ListServices()`. Wraps `networksetup -listallnetworkservices`, `-setsecurewebproxy`, `-setwebproxy`, `-setsecurewebproxystate`, `-getsecurewebproxy`. Operates on every active service, skips disabled ones. SOCKS proxy is explicitly NOT touched (WebRTC bypass guarantee).
- Snapshot includes per-service current state so `status` can render which services are actively routed through Slimference.
- All shell-outs are mockable via `execCommandFn` indirection so tests run without privileges.
- Tests: 12 covering: enable on multiple services, disable on multiple services, partial-failure aggregation, ListServices parsing of `networksetup` output, SOCKS-untouched assertion (test reads the issued command-list).

### WP5 - Cert install / uninstall (`internal/transparent/keychain.go`)

- `internal/transparent/keychain.go`: `Install(certPath string, scope Scope)` and `Uninstall(certSHA1 string, scope Scope)`. Scope = User (login.keychain-db, no sudo) or System (System.keychain, sudo prompt). Wraps `security add-trusted-cert -d -r trustRoot -k <keychain> <cert>` and `security delete-certificate -Z <sha1> <keychain>`.
- `IsTrusted(certSHA1 string)` runs `security verify-cert -c <cert>` and parses the result.
- Tests: 10 with mocked `security` command, covering install-success, uninstall-success, idempotent re-install, sudo-required-but-not-tty error path, SHA1 parsing.

### WP6 - launchd integration (`internal/transparent/launchd.go`)

- `internal/transparent/launchd.go`: `Install(plistPath, daemonBinary string)` writes a launch-agent plist with `RunAtLoad=true` + `KeepAlive=true`. `Uninstall(plistPath string)` removes it and runs `launchctl unload`.
- Plist content includes `StandardOutPath` / `StandardErrorPath` under `~/.slimference/log/`, environment variables for SLIMFERENCE_HOME, and a `WorkingDirectory` that is operator's home.
- Crash-safety hook: the daemon SIGTERM handler calls `transparent.Manager.Disable()` before exit so a daemon kill via launchctl does not strand the system-proxy.
- Tests: 8 covering plist generation, idempotent install, uninstall preserves user mods, launchctl invocation order.

### WP7 - `slimference proxy` subcommand tree (`cmd/slimference/proxy_cmd.go`)

- `cmd/slimference/proxy_cmd.go`: dispatcher for `install / enable / disable / status / uninstall`. Each subcommand prints a clear preamble explaining what it is about to do (CA generation, trust prompt, proxy flip), then prompts for confirmation unless `--yes` is passed.
- `proxy install` flow: generate CA -> show fingerprint -> ask user-vs-system trust -> install cert -> offer launchd auto-start -> exit. Emits a copy-paste verification command (`security verify-cert -c <path>`).
- `proxy status` output includes: CA fingerprint, trust scope (User/System/Untrusted), per-service proxy state, daemon listening state, intercepted-host allowlist, last-disable timestamp.
- Doctor integration: `slimference doctor` runs `transparent.Manager.Status()` when transparent mode is installed and surfaces any drift (proxy points at us but we are not listening, cert no longer trusted, etc.).
- Tests: 24 covering each subcommand's happy path, --yes bypass, status when nothing installed, status when cert untrusted, status when daemon down, etc.

### WP8 - Wiring + docs + CI

- `internal/config/config.go`: `[transparent]` section with `enabled`, `intercept_hosts`, `cert_cache_size`, `auto_disable_on_shutdown`. Defaults preserve current behaviour (transparent off).
- `internal/proxy/proxy.go::NewProxy` reads the config, instantiates `tlsca.Signer` when `enabled=true`, attaches CONNECT handler.
- Daemon main loop installs SIGTERM/SIGINT handler that calls `transparent.Manager.Disable()` if transparent mode is active.
- `docs/transparent-mode.md`: operator-facing documentation. What it does, what it does not do (WebRTC bypass), trust model, off-switch behaviour, troubleshooting.
- `scripts/ci/main.go`: existing 8-step gate stays; transparent-mode tests live in their packages and run under the existing `go test` step. No new CI step needed (the leaf-audit gate covers Layer 0; transparent mode is its own subsystem).

## Acceptance Criteria

- [ ] `slimference proxy install` runs end-to-end on a fresh Mac: generates CA, trusts in chosen keychain, sets system-proxy on every active service.
- [ ] `slimference proxy disable` cleanly returns the system to direct-mode in <1 second; existing in-flight WebSocket/SSE streams complete; next request reconnects direct.
- [ ] Codex Desktop App routes its `responses_websocket` stream through Slimference when transparent mode is enabled, with Layer 1/2 compression measurable in `slimference gain`.
- [ ] Codex Desktop App's microphone transcription (WebRTC) continues to work with native latency when transparent mode is enabled (UDP path untouched).
- [ ] CONNECT to `github.com:443` (or any non-allowlisted host) passes through as a raw TCP relay so the operator's other HTTPS traffic is unaffected.
- [ ] Daemon crash leaves the system in a working state: SIGTERM handler clears the proxy setting before exit; `proxy status` notices any drift on next run.
- [ ] Coverage 100% across `internal/tlsca/`, `internal/transparent/`, the new `cmd/slimference/proxy_cmd.go`, and the CONNECT/WebSocket additions in `internal/proxy/`. Race-clean. CI eight-step gate green.
- [ ] `docs/transparent-mode.md` documents the trust model, WebRTC bypass guarantee, off-switch, and uninstall path.

## Out of Scope

- Cross-OS support (Windows, Linux). T122 is macOS-only by design; equivalent flows on Linux (`gsettings`/`/etc/environment`) and Windows (registry under `Internet Settings`) ride on a future T122-windows / T122-linux.
- HTTPS-pinned apps (rare among LLM clients but exist; they bypass system trust). If discovered, document, do not break. No workaround attempted in T122.
- Replacing the per-tool config-patch path (`integrate_cmd.go`). Both modes coexist; operator picks. Removing the config-patch path would be T123 if ever justified.
- Cross-process cache sharing of CA-signed certs (T122 keeps the cache in-process; restart regenerates leaf certs from the persistent CA, which is fine for 24-hour TTLs).
- Network-Extension based intercept (would require Apple Developer signing and a different architecture). T122 stays in user-space with `networksetup`.

## Validation

```
go test -race ./internal/tlsca/... ./internal/transparent/... ./internal/proxy/... ./cmd/slimference/...
go run ./scripts/ci

# Manual flow on dev Mac:
slimference proxy install --yes
slimference proxy status
# launch Codex Desktop, run a normal session
slimference gain                         # confirm L1/L2 saving on the responses stream
# launch Codex Desktop, hit microphone, dictate something
# confirm transcription still works (WebRTC bypass)
slimference proxy disable
# Codex Desktop next request: direct to OpenAI, works unchanged
slimference proxy uninstall
# CA out of keychain, system back to pristine
```

## Notes / decisions

- **TLS-MITM is the standard for traffic-modifying proxies.** Charles, Proxyman, mitmproxy, Burp Suite all use the same model. Slimference's CA is locally generated, never leaves the machine, jederzeit revoke-bar via Keychain Access. The operator-facing trust prompt explicitly names "Slimference Local CA" so it is recognisable.
- **WebRTC bypass is the property that makes this safe for Audio.** macOS System-HTTPS-Proxy only affects HTTP/HTTPS; UDP-based WebRTC traffic ignores it by design. Slimference exploits this rather than trying to compete with WebRTC's latency budget.
- **Config-patch path stays.** Operators who do not want to install a CA can keep using `slimference integrate codex` / `claude`. T122 adds a second mode, does not remove the first.
- **Trust-scope default is User**, not System. System-trust requires sudo and affects all users on the machine; User-trust requires no sudo and only affects the current user. Operators upgrading to system-wide can opt in.
- **Realtime / audio path detection** uses URL patterns rather than Content-Type sniffing because the upgrade headers are visible at the dispatch boundary. The pattern list is kept in `internal/proxy/ws.go::audioBypassPatterns` and is operator-tunable via `[transparent] audio_bypass_paths`.
