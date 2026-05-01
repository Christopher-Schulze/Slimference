# Transparent mode

Transparent mode is the system-level intercept path. Once installed it routes every HTTPS-based LLM client (Codex Desktop, ChatGPT Desktop, Claude Code, Codex CLI, Anthropic SDK, anything that respects the macOS System-HTTPS-Proxy) through the local Slimference daemon without any per-app configuration. The clean off-switch (`slimference proxy disable`) drops the system back to direct connections; apps reconnect automatically on the next request.

This document covers what transparent mode does, what it deliberately does NOT do, the trust model, and the lifecycle commands.

## Architecture in one paragraph

`slimference proxy install` generates a local ECDSA P-256 root CA under `~/.slimference/ca/`, prompts the operating system to trust it in your keychain (User scope by default, no sudo), and registers the slimference daemon as the macOS System-HTTPS-Proxy on every active network service. From that point on every HTTPS connection an app makes ends up at `127.0.0.1:8990` first; Slimference signs a per-domain leaf certificate on the fly using the trusted root, terminates TLS, runs the request through Layer 0/1/2 compression, then re-emits it to the upstream provider. WebSockets (Codex Desktop's `responses_websocket` transport) tunnel through the same path. WebRTC (Codex Desktop microphone transcription, video) is unaffected because UDP traffic ignores the System-HTTPS-Proxy setting by design. Upstream TLS in transparent mode uses the configured `internal/tlsdial` uTLS profile instead of always emitting Go's stdlib ClientHello.

## What it does

- **Catches all HTTPS** for the configured allowlist of LLM hosts (`api.openai.com`, `api.anthropic.com`, `chatgpt.com` etc.). Compression layers run as if the request had arrived through a config-patched direct port.
- **Tunnels everything else** via raw TCP relay so iCloud, GitHub, package mirrors, and everything else on your machine keep working unaffected.
- **Honours WebSocket upgrades** so Codex Desktop's `responses_websocket` traffic completes end-to-end (compression on WS message boundaries is a follow-up after live-corpus measurement; the tunnel itself is in place).
- **Bypasses WebRTC** for audio. The macOS System-HTTPS-Proxy setting only affects HTTP/HTTPS; UDP / SRTP audio streams continue native. This is the property that makes transparent mode safe for Codex Desktop's microphone transcription feature.
- **Uses per-host TLS profiles upstream** in transparent mode. Defaults map `chatgpt.com` to `chromium_stable` and the API hosts to `node_stable` intent aliases, currently backed by maintained uTLS Chromium profiles.

## What it does NOT do

- **Does not touch SOCKS proxy.** WebRTC bypass is the property of NOT setting a SOCKS hook; we want audio to skip Slimference entirely.
- **Does not modify per-app configuration.** No `~/.codex/config.toml` patch, no `~/.claude/settings.json` edit. The config-patch path remains available through `slimference integrate codex` / `claude` for operators who do not want a CA in their keychain.
- **Does not run as root by default.** User-scope trust (`~/Library/Keychains/login.keychain-db`) is the default. `--system` flag opts into System-keychain trust which requires sudo.
- **Does not export the CA private key.** The ECDSA private key for the root CA stays in `~/.slimference/ca/root.key` (mode 0600), never traverses any network, never appears in any log.
- **Does not make traffic undetectable.** uTLS removes the obvious Go-stdlib ClientHello tell for transparent-mode upstream dials. It does not spoof source IP, DNS behaviour, HTTP/2 SETTINGS, header ordering, or exact Node/Python OpenSSL fingerprints.
- **Does not run on Linux or Windows.** macOS-only in this iteration; Linux (`gsettings`/`/etc/environment`) and Windows (`Internet Settings` registry) ride on a future T122-linux / T122-windows.

## Trust model

The same model proxyman / Charles / mitmproxy / Burp Suite use:

1. The CA is generated locally on first install.
2. You explicitly trust it in your keychain.
3. From then on Slimference can sign per-domain leaf certs that your apps accept as authentic.
4. Without the CA in your keychain, transparent mode does nothing - leaf certs are rejected and apps fail TLS handshake.
5. `slimference proxy uninstall` removes the trust entry and clears the System-HTTPS-Proxy setting; the system returns to its pre-install state.

The CA cert and key never leave your machine. The fingerprint is shown during install so you can cross-check it later via Keychain Access.

## Lifecycle

```
slimference proxy install [--system] [--no-launchd] [--yes]
slimference proxy enable
slimference proxy disable
slimference proxy status
slimference proxy uninstall [--system]
```

### `install`

One-time setup. Generates the CA if it does not exist, installs it as a trusted root, registers the daemon as a launch agent (skip with `--no-launchd`), and prints the CA fingerprint plus next-step instructions. Pass `--yes` to flip the System-HTTPS-Proxy on as the final step (otherwise install + enable are separate).

### `enable`

Sets `127.0.0.1:8990` as the HTTPS-proxy and HTTP-proxy on every active network service via `networksetup -setsecurewebproxy / -setwebproxy`. SOCKS is intentionally never touched. Per-service results are printed.

### `disable`

Clears the HTTPS / HTTP proxy via `networksetup -setsecurewebproxystate / -setwebproxystate ... off` on every active service. Apps that were already mid-stream finish their current request through Slimference; the next request reconnects directly to upstream. Cert remains trusted but inert.

### `status`

Prints the CA fingerprint, transparent runtime state, per-host TLS profile mapping, launch-agent state, per-service current proxy configuration, and daemon reachability when a service points at `127.0.0.1:8990`. If the system proxy points at Slimference but the daemon is unreachable, it prints the repair command `slimference proxy disable`.

## TLS profile configuration

Transparent mode adds these defaults:

```toml
[transparent]
enabled = false
intercept_hosts = ["api.openai.com", "api.anthropic.com", "chatgpt.com"]
default_tls_profile = "chromium_stable"

[transparent.tls_profiles]
"api.openai.com" = "node_stable"
"api.anthropic.com" = "node_stable"
"chatgpt.com" = "chromium_stable"
```

Supported concrete profiles are `chromium_stable`, `chrome_133`, `chrome_131`, `chrome_120`, `chrome_120_pq`, `ios_12_1`, `safari_16_0`, and `go_stdlib`. Intent aliases `node_stable`, `python_requests`, `node`, `python`, `chrome`, and `chromium` resolve to `chromium_stable` because the pinned uTLS dependency does not expose exact maintained Node/OpenSSL/Python profiles. This is a practical stealth improvement, not a byte-identical runtime impersonation guarantee.

### `uninstall`

Disables the proxy, removes the CA from the keychain, removes the launch agent. The CA files in `~/.slimference/ca` remain on disk so a re-install can reuse them; delete the directory manually for a fully clean slate.

## Off-switch behaviour

`slimference proxy disable` is the canonical off-switch. After it returns:

- All future HTTPS connections from apps go straight to the real upstream.
- WebRTC audio (already unaffected) continues exactly as before.
- The slimference daemon keeps running for direct-port use (Codex CLI / Claude Code can still point at `127.0.0.1:8990` via their own config-patch path).
- The CA stays trusted in your keychain. It has no effect without the proxy setting routing traffic through us, but is ready for the next `slimference proxy enable` without a re-trust prompt.

If the daemon crashes while the proxy is set, all HTTPS apps lose connectivity until either the daemon comes back (launch agent auto-restart) or the operator runs `slimference proxy disable`. The launch agent (`KeepAlive=true`) handles the common case automatically; a manual disable is the fallback for cases where the daemon is stopped intentionally.

## Troubleshooting

- **`slimference proxy status` shows "off" on every service after enable**: another tool may be overwriting the proxy setting (corporate VPN, third-party proxy manager). Disable the other tool, re-run `enable`, and verify with `networksetup -getsecurewebproxy "Wi-Fi"`.
- **App fails with "self signed certificate" / SSL_ERROR_BAD_CERT**: the CA is not trusted. Re-run `slimference proxy install` and accept the keychain trust prompt. Verify with `security verify-cert -c ~/.slimference/ca/root.crt`.
- **Codex Desktop microphone fails**: WebRTC is supposed to bypass the proxy. Check that you only ran `slimference proxy install` and did not manually enable a SOCKS proxy. Slimference deliberately never touches SOCKS.
- **Apps that use cert-pinning bypass the trust store**: rare but exist. They will fail TLS handshake under transparent mode regardless of CA trust. Either run them outside transparent mode (disable, run app, re-enable) or use the per-app config-patch path which never touches their TLS path.
- **`networksetup` says permission denied**: macOS sometimes asks for an admin password the first time. Re-run from a terminal owned by your user account.
- **`proxy status` prints "Daemon unreachable"**: the system proxy is still pointing at Slimference but the daemon is down. Run `slimference proxy disable` to restore direct HTTPS immediately, then restart the daemon before enabling transparent mode again.

## Implementation references

- TLS CA + per-domain signer: `internal/tlsca/`
- CONNECT method dispatch + MITM HTTPS proxy: `internal/proxy/connect.go`, `internal/proxy/mitm_response.go`, `internal/proxy/single_listener.go`
- WebSocket tunnel: `internal/proxy/ws.go`
- Upstream TLS profile dialer: `internal/tlsdial/`
- macOS system integration (networksetup / keychain / launchd): `internal/transparent/`
- Subcommand tree: `cmd/slimference/proxy_cmd.go`
- Plan: `docs/todo/t122-transparent-mode.md`
