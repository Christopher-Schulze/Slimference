# TASK 238: Codex Desktop process-local proxy proof

Status: PLANNED
Priority: P0 before any Desktop Slimference product claim
Scope: Codex Desktop App only; Browser ChatGPT, ChatGPT.app, Claude Code, and
direct Finder/Spotlight Codex.app launches must stay untouched

## Why

The user wants maximum Slimference savings with no intelligence, context,
memory, reliability, UX, or collateral-network drawback. Codex CLI already has
the clean product path: `transport=auto` promotes to certified WSS for the
current Codex/Slimference tuple and fails open.

Codex Desktop is different. The current base-URL launcher proves process-local
environment injection reaches the app-server, but Codex.app 0.131.0-alpha.9
still keeps conversation traffic on hardcoded `chatgpt.com` URLs. The next
credible Desktop route is not another config patch; it is a process-local proxy
launch mode. The spawned Codex.app gets proxy env only for that process tree.
Normal Codex.app, Browser ChatGPT, ChatGPT.app, and Claude Code remain direct.

Non-disruptive inspection on 2026-05-18 found the current bundled Codex Desktop
binary contains proxy and WSS proxy surfaces (`HTTP_PROXY`, `HTTPS_PROXY`,
`WSS_PROXY`, `ALL_PROXY`, lowercase variants, `NO_PROXY`, `CODEX_NETWORK_PROXY_ACTIVE`,
`network-proxy/*`, and `responses_websocket`). This makes the route plausible
enough to engineer and live-prove, but not enough to claim success.

## Acceptance

- Codex.app launched through Slimference can be tested without modifying
  `/etc/hosts`, pfctl, macOS system proxy settings, persistent shell env, or
  `~/.codex/config.toml`.
- Direct Codex.app launches from Finder/Spotlight stay direct to `chatgpt.com`.
- Browser ChatGPT and ChatGPT.app stay direct while a Slimference-launched
  Codex.app is running.
- If proxy env is honored by Codex.app conversation WSS, `lsof` shows the
  launched app-server connected to the Slimference loopback proxy and no direct
  `chatgpt.com:443` conversation connection.
- `/admin/state.wss` shows WSS bridge activity with `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`, and compressed messages
  inspected; mutation counters must advance before any savings claim.
- If proxy env is ignored or WSS bypasses it, the product reports Desktop as
  direct-only instead of faking success.
- Any CA trust requirement is explicit, reversible, and visible in Status.
- Any parse drift or unsupported frame shape fails open to byte-equal tunnel.

## Sub-Tasks

- [x] Reconcile stale Desktop docs: record that base-URL env injection is
  process-local but insufficient for current Codex.app conversation routing.
- [ ] Decouple CONNECT/MITM ingress from global `transparent.enabled` so a
  scoped Desktop proxy mode can run without arming global lab routing.
- [ ] Add a product-scoped proxy profile for Codex Desktop:
  intercept `chatgpt.com` only, use existing WSS Phase-F bridge, and bypass
  audio/realtime paths.
- [ ] Extend `codex launch-desktop` with a proxy transport mode that injects
  `HTTP_PROXY`, `HTTPS_PROXY`, `WSS_PROXY`, `ALL_PROXY`, lowercase variants,
  and `NO_PROXY=127.0.0.1,localhost,::1`.
- [ ] Keep the existing base-URL launcher mode as diagnostic/future-proof, but
  do not present it as the active Desktop conversation route.
- [ ] Add CA trust preflight: refuse proxy launch with a clear instruction when
  the Slimference CA is not trusted.
- [ ] Add `--probe` output that shows exactly which env keys would be injected
  without spawning Codex.app.
- [ ] Add status observation fields for Desktop launch mode: direct,
  slimference-launched, proxy-env-present, proxy-connected, last WSS route,
  last telemetry timestamp.
- [ ] Add tests for env construction, CA preflight, CONNECT routing activation,
  WSS upgrade handoff, bypass paths, and fail-open behavior.
- [ ] Run controlled live proof from outside the active Codex Desktop session:
  launch via Slimference, send one prompt, collect `lsof`, `/admin/state.wss`,
  config hash, and direct Browser/ChatGPT.app control evidence.
- [ ] Document the final branch decision in `docs/install.md` and the operation
  log: proven proxy mode, direct-only limitation, or upstream-required blocker.

## Notes

Known current state:

- Codex CLI: working, certified, auto-WSS, no global collateral.
- Codex.app direct launch: works normally and stays direct.
- Current launcher: process-local base-URL env injection, useful as a probe,
  not sufficient for conversation routing on Codex.app 0.131.0-alpha.9.
- Existing Slimference CONNECT/MITM code can already terminate CONNECT and feed
  WebSocket upgrades into the WSS Phase-F bridge, but it is wired only when
  `transparent.enabled=true`. T238 must make a scoped Desktop product path
  instead of re-promoting global lab mode.

Live proof must not be run from this active Codex Desktop session unless the
operator explicitly accepts the risk of focus/process disruption. A separate
terminal or a quiet window is required for the proof ceremony.

## Deviations

None yet.
