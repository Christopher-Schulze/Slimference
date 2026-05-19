# TASK 242: Codex Desktop root-store and proxy compatibility matrix

Status: PLANNED
Priority: P0 after T238 zero-byte CONNECT classification
Scope: Codex Desktop App conversation routing only; no global lab product path

## Why

T238 proved the clean scoped Desktop architecture up to CONNECT:
process-local proxy env reaches the Codex.app Rust app-server, CONNECT reaches
Slimference, and upstream dial succeeds. The failure is after Slimference
presents its local CA-signed leaf: Codex.app closes before any application
bytes flow.

The current best hypothesis is a Rust TLS root-store mismatch: Codex.app may use
embedded webpki roots or another root store that does not see the macOS Keychain
trust entry. This is functionally as blocking as pinning, but it is more precise
and may have a solvable hook.

## Acceptance

- Run the explicit `--with-ca-env` Desktop launcher probe:
  `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and
  `NODE_EXTRA_CA_CERTS` point at the Slimference root only for the spawned
  Codex.app process.
- Verify app-server env via `ps eww` and route via `lsof`.
- Send one Desktop prompt and capture `/admin/state.wss` before/after.
- If bytes and WSS frames flow, continue to mutation proof before claiming
  Desktop savings.
- If bytes remain zero, classify Desktop as direct-only until upstream changes
  its root-store or exposes a route hook.
- Investigate Codex's own managed `network.proxy_url` / permission-profile
  proxy path from the OpenAI Codex source tree and prove whether it applies only
  to command sandbox traffic or can ever affect Desktop conversation routing.
- No `/etc/hosts`, pfctl, macOS system proxy, persistent shell env, or
  `~/.codex/config.toml` mutation is allowed in the product proof.

## Sub-Tasks

- [ ] Run `slimference codex launch-desktop --transport=proxy --with-ca-env
  --probe` and verify all CA env hints are present.
- [ ] Launch with `--with-ca-env`, send a prompt, collect lsof, WSS counters,
  daemon log tail, config hash, and browser/ChatGPT.app direct controls.
- [ ] Test a direct Finder/Spotlight relaunch after cleanup and prove it is
  direct again.
- [ ] Read current installed Codex.app strings and upstream Codex source for
  root-store, `SSL_CERT_FILE`, `native-certs`, `webpki-roots`, and
  managed-proxy behavior.
- [ ] If managed `network.proxy_url` is plausible for conversation traffic,
  design a non-persistent probe; otherwise document why it is command-sandbox
  only.
- [ ] Update T239 Launch Codex App menu-state vocabulary with the final result:
  `desktop-proxy-proven`, `desktop-ca-env-probe`, or `desktop-direct-only`.

## Notes

The menu item is not pointless. It is the user-facing steering surface. What is
forbidden is presenting a blocked route as active savings. The item should stay
visible and capability-gated.

## Deviations

None yet.
