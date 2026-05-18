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

## Target State

One of two final states is acceptable:

1. **Desktop Slimference mode proven:** `slimference codex launch-desktop
   --transport=proxy` starts Codex.app with process-local proxy env, the
   conversation WSS stream reaches Slimference, WSS Phase-F remains clean, and
   direct Finder/Spotlight launches remain native.
2. **Desktop direct-only truth proven:** Codex.app ignores or bypasses every
   scoped process-local proxy route. Slimference reports Desktop as direct-only
   and keeps all Desktop savings claims off until upstream exposes a usable
   route.

No third state is allowed. "Probably working", sideband-only, cosmetic badge,
or inferred savings is not a product outcome.

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

## Decision Tree

1. Build scoped proxy launch support without enabling it by default.
2. Run `--probe` first and verify only the spawned Codex.app process tree would
   receive proxy env.
3. Refuse real launch if the CA is not trusted, unless the operator passes an
   explicit debug override that does not become the product path.
4. Launch Codex.app from a quiet external terminal, not from the active Codex
   Desktop session.
5. Send one minimal Desktop prompt.
6. Check `lsof` and `/admin/state.wss`.
7. If conversation WSS is proxied and clean, run a mutation-triggering Desktop
   prompt before claiming savings.
8. Quit the Slimference-launched app and relaunch Codex.app from Finder.
9. Prove Finder launch is direct again.
10. While Slimference-launched Codex.app is running, prove Browser ChatGPT and
    ChatGPT.app remain direct.
11. If any step fails, Desktop remains direct-only with a precise reason.

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
- [ ] If minimal prompt proves routing but not mutation, run a Desktop prompt
  with enough repeated context/tool output to trigger Phase-F mutation.
- [ ] Add explicit failure-class reporting:
  `proxy_env_not_inherited`, `connect_not_attempted`, `tls_untrusted`,
  `cert_pinned`, `wss_bypassed_proxy`, `wss_parse_drift`,
  `proxied_but_no_mutation_candidate`, `passed`.
- [ ] Add a `codex desktop status` or equivalent probe output so T239 can render
  Desktop truth without scraping logs.
- [ ] Document the final branch decision in `docs/install.md` and the operation
  log: proven proxy mode, direct-only limitation, or upstream-required blocker.

## Engineering Plan

1. **Scoped ingress:** keep the existing global lab CONNECT/MITM code intact,
   but add a product-scoped Desktop proxy profile that can be enabled without
   `transparent.enabled`, `sni_peek_mode`, hosts, pfctl, or macOS system proxy.
2. **Host policy:** intercept only `chatgpt.com` for Desktop proof. Keep all
   other CONNECT hosts byte-equal passthrough or blocked by explicit allowlist,
   depending on the existing proxy policy.
3. **WSS reuse:** reuse the existing `WebSocketTunnel` and Phase-F dispatcher.
   Do not fork a second WSS parser or a Desktop-only mutation path.
4. **Env set:** inject `HTTP_PROXY`, `HTTPS_PROXY`, `WSS_PROXY`, `ALL_PROXY`,
   lowercase variants, `NO_PROXY`, and any Codex-specific proxy guard env found
   in the binary inspection only into the spawned process.
5. **Trust gate:** product path requires Slimference CA trusted in Keychain.
   The launcher must fail before spawn if trust is missing, with one clear
   repair command.
6. **Observation:** persist a Desktop launch observation record containing app
   version, app-server PID, env mode, first loopback connect timestamp, WSS
   route mode, parse/degrade/compression counters, and direct-control checks.
7. **Fallback:** if the proxy path fails, leave the machine in the exact direct
   state and record the failure class. Do not retry by arming global lab mode.
8. **Docs:** `docs/install.md`, `docs/todo.md`, and operation log must say the
   same thing: Desktop proven, or Desktop direct-only.

## Live Proof Matrix

| Proof | Required Evidence |
|---|---|
| Spawn scope | app-server env contains proxy keys; normal Finder launch env does not |
| Loopback route | `lsof` shows app-server connected to `127.0.0.1:<port>` |
| No direct conversation | no app-server direct `chatgpt.com:443` conversation socket during Slimference launch |
| WSS parser health | `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0` |
| Savings eligibility | `compressed_messages_inspected>0` and mutation counters advance on a mutation prompt |
| Browser untouched | browser process remains direct to `chatgpt.com:443` |
| ChatGPT.app untouched | ChatGPT.app process remains direct if running |
| Direct fallback | Finder/Spotlight Codex.app relaunch returns direct |
| Config unchanged | `~/.codex/config.toml` hash unchanged |
| Global untouched | hosts inactive, pf unchanged, system proxy unchanged |

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

T238 intentionally does not add a new normal user surface. It only proves the
Desktop branch and exposes enough status for T239 to render the right button
behavior.

## Deviations

None yet.
