# Transparent mode

Transparent mode is the system-level intercept path. Once installed it routes every HTTPS-based LLM client (Codex Desktop, ChatGPT Desktop, Claude Code, Codex CLI, Anthropic SDK, anything that respects the macOS System-HTTPS-Proxy) through the local Slimference daemon without any per-app configuration. The clean off-switch (`slimference proxy disable`) drops the system back to direct connections; apps reconnect automatically on the next request.

This document covers what transparent mode does, what it deliberately does NOT do, the trust model, and the lifecycle commands.

## Architecture in one paragraph

`slimference proxy install` generates a local ECDSA P-256 root CA under `~/.slimference/ca/`, prompts the operating system to trust it in your keychain (User scope by default, no sudo), and registers the Slimference daemon for autostart. `slimference proxy enable` is the separate arming step: it sets the macOS System-HTTPS-Proxy on every active network service so HTTPS connections first reach `127.0.0.1:8990`. Slimference signs a per-domain leaf certificate on the fly using the trusted root, terminates TLS, runs allowlisted LLM requests through the compression pipeline, then re-emits them to the upstream provider. WebSockets (Codex Desktop's `responses_websocket` transport) tunnel through the same path. WebRTC (Codex Desktop microphone transcription, video) is unaffected because UDP traffic ignores the System-HTTPS-Proxy setting by design. Upstream TLS in transparent mode uses the configured `internal/tlsdial` uTLS profile instead of always emitting Go's stdlib ClientHello.

## What it does

- **Catches all HTTPS** for the configured allowlist of LLM hosts (`api.openai.com`, `api.anthropic.com`, `chatgpt.com` etc.). Compression layers run as if the request had arrived through a config-patched direct port.
- **Tunnels everything else** via raw TCP relay so iCloud, GitHub, package mirrors, and everything else on your machine keep working unaffected.
- **Honours WebSocket upgrades** so Codex Desktop's `responses_websocket` traffic completes end-to-end. The tunnel is byte-for-byte by default; T142 adds an inspect-only frame parser that can record opcode/direction/JSON-shape metadata without mutating frames. Compression on WS message boundaries remains blocked until live Codex frame-shape evidence proves the internal protocol is stable enough.
- **Bypasses WebRTC** for audio. The macOS System-HTTPS-Proxy setting only affects HTTP/HTTPS; UDP / SRTP audio streams continue native. This is the property that makes transparent mode safe for Codex Desktop's microphone transcription feature.
- **Uses per-host TLS profiles upstream** in transparent mode. Defaults map `chatgpt.com` to `chromium_stable` and the API hosts to `node_stable` intent aliases, currently backed by maintained uTLS Chromium profiles.

## What it does NOT do

- **Does not touch SOCKS proxy.** WebRTC bypass is the property of NOT setting a SOCKS hook; we want audio to skip Slimference entirely.
- **Does not modify per-app configuration.** No `~/.codex/config.toml` patch, no `~/.claude/settings.json` edit. The config-patch path remains available through `slimference integrate install --client codex|claude` for operators who do not want a CA in their keychain.
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
slimference proxy env codex <--direct|--proxied|--transparent-proxied> [-- <codex-args>...]
```

The same lifecycle is available from the TUI. Open `slimference`, switch to
Setup, then use:

- `[enter]` on **Install transparent proxy (CA + daemon)** to trust the local CA
  and install the launch agent.
- `[enter]` on **Arm system HTTPS proxy** or shortcut `[a]` to route system HTTPS
  through Slimference.
- `[a]` again to disarm and return apps to direct upstream connections.
- `[u]` to run transparent uninstall from the TUI.
- `[p]`, `[o]`, `[e]`, `[w]` for daemon start/stop, restart, autostart install,
  and autostart removal. Start/restart waits for the daemon status file before
  reporting success, so immediate status checks do not race process startup.

The TUI status line is a cached operator snapshot: CA exists, CA trusted,
autostart installed, system proxy armed, number of armed services, daemon
reachability, and networksetup availability. It force-refreshes after actions
and avoids calling `networksetup`, `security`, or `launchctl` from render loops.

### `install`

One-time setup. Generates the CA if it does not exist, installs it as a trusted root, registers the daemon as a launch agent (skip with `--no-launchd`), and prints the CA fingerprint plus next-step instructions. Pass `--yes` to flip the System-HTTPS-Proxy on as the final step (otherwise install + enable are separate).

### `enable`

Sets `127.0.0.1:8990` as the HTTPS-proxy and HTTP-proxy on every active network service via `networksetup -setsecurewebproxy / -setwebproxy`. SOCKS is intentionally never touched. Per-service results are printed.

### `disable`

Clears the HTTPS / HTTP proxy via `networksetup -setsecurewebproxystate / -setwebproxystate ... off` on every active service. Apps that were already mid-stream finish their current request through Slimference; the next request reconnects directly to upstream. Cert remains trusted but inert.

### `status`

Prints the CA fingerprint, transparent runtime state, per-host TLS profile mapping, launch-agent state, per-service current proxy configuration, and daemon reachability when a service points at `127.0.0.1:8990`. If the system proxy points at Slimference but the daemon is unreachable, it prints the repair command `slimference proxy disable`.

### `env codex`

Prints exact Codex CLI launch commands for split live testing without mutating
`~/.codex`:

```bash
slimference proxy env codex --direct
slimference proxy env codex --proxied
slimference proxy env codex --transparent-proxied
```

`--direct` clears `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` and lowercase
variants, then sets `NO_PROXY=*`. Use it when the macOS System-HTTPS-Proxy is
armed and Codex App should flow through Slimference while Codex CLI should try
to stay direct.

`--proxied` is the preferred CLI-only split-test path. It unsets proxy
environment variables, keeps the macOS System-HTTPS-Proxy untouched, and
launches Codex with per-process config overrides:

```toml
model_provider = "slimference-codex"

[model_providers.slimference-codex]
name = "Slimference Codex"
base_url = "http://127.0.0.1:8990/backend-api/codex"
requires_openai_auth = true
supports_websockets = false
wire_api = "responses"
```

Modern Codex appends `/responses` to the provider base URL, so the Codex
backend prefix is required. The command is not written to `~/.codex/config.toml`;
it only affects that one CLI process. This keeps Codex App direct while Codex
CLI flows through Slimference. Setting `supports_websockets=false` on the custom
provider is the clean path for compression: Codex uses HTTP directly, Slimference
decodes Codex's `Content-Encoding: zstd` request body, runs the normal pipeline,
re-encodes zstd for upstream, and logs the processed request as
`route_mode=upstream`. Default direct mode can still tunnel Codex's WebSocket
transport byte-for-byte (`route_mode=websocket_tunnel`); that is the
smoothest/invisibility-first path but does not inspect message frames. The
legacy `[proxy] direct_codex_websocket_policy = "force_https_fallback"` remains
available only as a fallback proof mode for older launch commands.

`--transparent-proxied` is the CONNECT/MITM variant. It sets HTTP/HTTPS/ALL
proxy environment variables to `http://127.0.0.1:8990` and clears `NO_PROXY`.
Use it only when explicitly testing the CA path. It requires a running daemon
with `[transparent].enabled=true` and a trusted local CA, but still does not
require `slimference proxy enable`.

Flight log commands such as `slimference debug flight tail 50 --json` only have
disk evidence if the running daemon has `[debug].decisions_log` configured or
was started with `SLIMFERENCE_DEBUG_DECISIONS_LOG`, for example:

```bash
export SLIMFERENCE_DEBUG_DECISIONS_LOG="$HOME/.slimference/debug/decisions.jsonl"
```

For launchd-managed daemon runs, prefer the config file setting because a
current shell export does not retroactively change an already-running daemon.
The recorder expands leading `~/` paths and creates the parent directory on
first write, so `~/.slimference/debug/decisions.jsonl` works without manual
directory setup.

For real proxied Codex accounting, use the flight-backed gain view:

```bash
slimference gain --proxy today --json
slimference savings today --json
```

This reads the same decision log and separates provider-reported input tokens,
provider cached tokens, output tokens, estimated input savings, output-reduce
input overhead, and a billing-equivalent cache-read discount estimate.
`gain --proxy` and `savings` count provider proxy flights only: local hook,
readhook, raw passthrough, and unknown-provider records are ignored so the
report cannot be inflated by local bookkeeping. Plain `slimference gain`
remains the Layer-0 `filter.db` view; `gain --cache` and `gain --output` read
analytics-event logs, while `gain --proxy` is the focused proxy evidence view
and `savings` is the operator's unified roll-up for daemon-proxied Codex
CLI/App traffic.

## T140 live split test

Mode 1: Codex App through Slimference, Codex CLI direct.

```bash
slimference proxy status
slimference proxy install
slimference proxy enable
slimference proxy status
slimference proxy env codex --direct
slimference debug flight tail 50 --json
```

Run the printed Codex CLI command in a separate terminal. A Codex App text turn
should create transparent flight records; the CLI turn should not. If the CLI
still creates flight records, the current CLI build is not proven direct under
that environment.

Mode 2: Codex CLI through Slimference, Codex App direct.

```bash
slimference proxy disable
slimference proxy status
slimference proxy env codex --proxied
slimference debug flight tail 50 --json
```

Run the printed Codex CLI command. A CLI text turn should create flight records
with `provider=codex_chatgpt`, `path=/backend-api/codex/responses`, and
`route_mode=upstream`. Codex App should remain direct because the macOS
System-HTTPS-Proxy is disabled and no persistent `~/.codex/config.toml` block is
written.

Browser-Use passthrough is proven by a non-LLM HTTPS host being raw-relayed
without compression. Microphone/WebRTC bypass is a negative proof: the App voice
path still works and no audio payload inspection appears in flight records.

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

`slimference doctor` prints the shipped TLS profile catalogue version/date and
warns when the catalogue is older than the review threshold. That warning means
"refresh the pinned uTLS/browser profile mapping"; it is not a runtime failure
and it is not a JA3 match proof.

Local profile wiring can be checked without contacting an external probe:

```bash
go run ./scripts/utils tls-probe --profile=chromium_stable --json
```

The probe starts a loopback TCP listener, captures Slimference's outbound
ClientHello, parses TLS/JA3 fields, and compares the selected profile with
`go_stdlib`. This proves the local Go-stdlib tell is removed. It is still not
an external JA3/JA4 proof because provider-edge observation includes network
path, TLS termination, and JA4 features outside the local ClientHello.

T139 adds an opt-in reflected proof path:

```bash
go run ./scripts/utils tls-probe \
  --profile=chromium_stable \
  --reflector=https://<reflector-host>/<json-endpoint> \
  --save \
  --json
```

The reflected probe still uses `internal/tlsdial`, not Go's default
`net/http`, so it measures the same uTLS ClientHello path used by transparent
mode. Proof records are appended under `~/.slimference/tls-proofs/` and
`slimference doctor` reports the latest status per profile. If the reflector
negotiates HTTP/2, Slimference records the attempt as unproven instead of
pretending HTTP/1.1 probing proves HTTP/2 SETTINGS or JA4 parity.

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
