# TASK 242: Codex Desktop custom-CA env and proxy compatibility matrix

Status: READY FOR LIVE PROBE
Priority: P0 after T238 zero-byte CONNECT classification
Scope: Codex Desktop App conversation routing only; no global lab product path

## Why

T238 proved the clean scoped Desktop architecture up to CONNECT:
process-local proxy env reaches the Codex.app Rust app-server, CONNECT reaches
Slimference, and upstream dial succeeds. The failure is after Slimference
presents its local CA-signed leaf: Codex.app closes before any application
bytes flow.

The current best hypothesis is a Rust TLS root-store mismatch: Codex.app may use
rustls/webpki roots or another root store that does not see the macOS Keychain
trust entry. This is functionally as blocking as pinning, but it is more precise
and may have a solvable hook.

The newest high-value hook is Codex's own custom CA environment support:
current upstream Codex has `CODEX_CA_CERTIFICATE`, falls back to
`SSL_CERT_FILE`, and its secure WebSocket path calls the same custom-CA rustls
builder before opening `wss://.../responses`. The installed Codex.app binary
also contains `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`, rustls/webpki, proxy
env, and managed-proxy strings. That does not prove Desktop conversation routing
works, but it changes the next proof: try process-local custom CA env before
asking the user to trust a CA in Keychain.

This task must not confuse Desktop TLS work with the scoped Codex CLI WSS
product path. CLI WSS Phase-F, WSS bridge, auto-recert, and the T243 ladder do
not require macOS Keychain trust. CA/root-store work here is only for Desktop
process-local proxy or global lab TLS-MITM branches. It is still CA/proxy/MITM:
without an official Desktop endpoint hook or local-provider route, Slimference
cannot see encrypted Desktop WSS frames and therefore cannot produce Desktop
Phase-F savings.

## Acceptance

- Run the explicit `--with-ca-env` Desktop launcher probe with
  `CODEX_CA_CERTIFICATE` as the primary Codex-specific CA hook and
  `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and
  `NODE_EXTRA_CA_CERTS` as compatibility hints. Every variable points at the
  Slimference root only for the spawned Codex.app process.
- Do not require macOS Keychain trust for this first Desktop proof. Keychain
  trust is a fallback/lab branch from T245, not the default Desktop UX.
- Verify app-server env via `ps eww` and route via `lsof`.
- Send one Desktop prompt and capture `/admin/state.wss` before/after.
- If bytes and WSS frames flow, continue to mutation proof before claiming
  Desktop savings.
- If bytes remain zero even with `CODEX_CA_CERTIFICATE`, classify Desktop as
  `desktop_ca_env_rejected` / direct-only until upstream changes its root-store
  behavior or exposes a supported route hook.
- If Desktop bytes flow but Phase-F mutation is not yet proven, classify
  Desktop as WSS byte-equal bridge, not as a savings path. The T243 ladder
  applies to Desktop only after T242 proves process-local Desktop routing can
  carry real conversation bytes through Slimference.
- If CA env or Keychain trust is absent, do not mark CLI WSS degraded. Report
  it only as a Desktop/Lab readiness detail. The TUI wording must say
  "not needed for CLI WSS".
- Investigate Codex's own managed `network.proxy_url` / permission-profile
  proxy path from the OpenAI Codex source tree and prove whether it applies only
  to command sandbox traffic or can ever affect Desktop conversation routing.
- Investigate provider/base-URL route hooks one last time, but treat them as
  positive only if Desktop conversation WSS bytes, not sideband traffic, enter
  Slimference.
- No `/etc/hosts`, pfctl, macOS system proxy, persistent shell env, or
  `~/.codex/config.toml` mutation is allowed in the product proof.

## Sub-Tasks

- [x] Update or verify the Desktop launcher so `--with-ca-env` injects
  `CODEX_CA_CERTIFICATE=<slimference-root.crt>` before generic CA env hints.
- [~] Run `slimference codex launch-desktop --transport=proxy --with-ca-env
  --probe` and verify all process-local proxy and CA env hints are present.
- [ ] Launch with `--with-ca-env`, send a prompt, collect lsof, WSS counters,
  daemon log tail, config hash, and browser/ChatGPT.app direct controls.
- [ ] Test a direct Finder/Spotlight relaunch after cleanup and prove it is
  direct again.
- [ ] Read current installed Codex.app strings and upstream Codex source for
  `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`, `native-certs`, `webpki-roots`,
  websocket TLS config, and managed-proxy behavior.
- [ ] Prove or reject the no-CA/no-proxy alternatives explicitly:
  official endpoint/base-URL hook, managed `network.proxy_url`, remote-control
  or app-server launch path. Any positive claim requires Desktop conversation
  WSS bytes through Slimference, not merely process env, provider badge, or
  sideband requests.
- [ ] If managed `network.proxy_url` is plausible for conversation traffic,
  design a non-persistent probe; otherwise document why it is command-sandbox
  only.
- [x] Cross-check T245 CA state wording: missing CA must block only Desktop
  proxy/lab probes, never scoped CLI WSS.
- [ ] Update T239 Launch Codex App menu-state vocabulary with the final result:
  `desktop-proxy-proven`, `desktop-ca-env-probe`, or `desktop-direct-only`.

## Notes

The menu item is not pointless. It is the user-facing steering surface. What is
forbidden is presenting a blocked route as active savings. The item should stay
visible and capability-gated.

T243's WSS-first ladder is CLI-first. Desktop adopts it only after this task
proves the TLS/root-store barrier is gone. Until then, normal Finder/Spotlight
Codex.app remains the correct direct path and the TUI Desktop launch remains
blocked or diagnostic.

The Desktop menu item remains useful as the steering surface, but success means
real bytes and WSS frames through Slimference. CA env present, Keychain trusted,
provider badge, or CONNECT accepted are only precondition signals, not proof of
savings.

2026-05-20 non-live update:

- TUI Launch Codex App now calls
  `slimference codex launch-desktop --transport=proxy --with-ca-env`.
- Missing CA material still blocks Desktop diagnostics, but missing Keychain
  trust does not block the preferred Desktop probe and never affects CLI WSS.
- Historical `tls_trust_rejected` counters no longer permanently block the TUI
  launch path; they are shown as a process-local CA-env retry/proof state. A
  future live run must still prove bytes, frames, and zero errors before Desktop
  savings can be claimed.

The preferred branch is now:

1. TUI launches Codex.app with process-local proxy env and
   `CODEX_CA_CERTIFICATE`.
2. Slimference terminates only this Codex.app process tree's `chatgpt.com`
   WSS tunnel.
3. Browser ChatGPT, ChatGPT.app, normal Finder-launched Codex.app, Claude Code,
   system proxy, hosts, and `~/.codex/config.toml` remain untouched.
4. Desktop is called green only after bytes, WSS frames, Phase-F mutation, and
   clean error counters are live-proven.

If this branch works, Desktop can reach the same Phase-F savings class as the
CLI. If it fails, Desktop remains direct and honest until OpenAI exposes a
supported endpoint/root-store hook.

## Deviations

None yet.
