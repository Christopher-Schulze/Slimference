# TASK 238: Codex Desktop process-local proxy proof

Status: CLOSED NEGATIVE - process-local CONNECT works, Desktop TLS root-store
blocks bytes; superseded by T246 app-server shim proof
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
launch mode. The spawned Codex.app gets Electron proxy arguments plus proxy env
only for that process tree. Normal Codex.app, Browser ChatGPT, ChatGPT.app, and
Claude Code remain direct.

Non-disruptive inspection on 2026-05-18 found the current bundled Codex Desktop
binary contains proxy and WSS proxy surfaces (`HTTP_PROXY`, `HTTPS_PROXY`,
`WSS_PROXY`, `ALL_PROXY`, lowercase variants, `NO_PROXY`, `CODEX_NETWORK_PROXY_ACTIVE`,
`network-proxy/*`, and `responses_websocket`). This makes the route plausible
enough to engineer and live-prove, but not enough to claim success.

## Target State

One of two final states is acceptable:

1. **Desktop Slimference mode proven:** `slimference codex launch-desktop
   --transport=proxy --with-ca-env` starts Codex.app with process-local
   Electron proxy arguments, proxy env, and Codex custom-CA env, the
   conversation WSS stream reaches Slimference, WSS Phase-F remains clean, and
   direct Finder/Spotlight launches remain native.
2. **Desktop Slimference blocked truth proven:** Codex.app ignores or bypasses
   every scoped process-local proxy route, or closes before bytes flow.
   Slimference reports the TUI Desktop launch as blocked and keeps all Desktop
   savings claims off until upstream exposes a usable route. Direct Desktop
   remains Finder/Spotlight outside Slimference.

No third state is allowed. "Probably working", sideband-only, cosmetic badge,
or inferred savings is not a product outcome.

Current live state on 2026-05-22 is a precise blocked branch, not a mystery:
proxy env and Electron proxy args reach Codex.app, CONNECT reaches Slimference,
Chromium NetworkService no longer opens direct ChatGPT sockets for the launched
process tree, and upstream dial succeeds. Codex.app still closes the tunnel
before application bytes flow. Treat this as `tls_trust_rejected` /
root-store mismatch unless a future T242 custom-CA or upstream route hook proves
otherwise.

Follow-up source inspection found a better route: Codex.app honors
`CODEX_CLI_PATH` for its app-server child, and `codex app-server` accepts
process-local provider overrides. T246 tracks that no-CA app-server shim and
its final current-build blocker (`desktop_connect_only_no_app_server_bytes`).
This proxy branch remains diagnostic only.

## Acceptance

- Codex.app launched through Slimference can be tested without modifying
  `/etc/hosts`, pfctl, macOS system proxy settings, persistent shell env, or
  `~/.codex/config.toml`.
- Direct Codex.app launches from Finder/Spotlight stay direct to `chatgpt.com`.
- Browser ChatGPT and ChatGPT.app stay direct while a Slimference-launched
  Codex.app is running.
- If proxy env and Electron proxy args are honored by Codex.app conversation
  WSS, `lsof` shows the launched app-server and Chromium NetworkService
  connected to the Slimference loopback proxy and no direct `chatgpt.com:443`
  conversation connection.
- `/_slimference/admin/state` under `.wss` shows WSS bridge activity with
  `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`, and compressed messages
  inspected; mutation counters must advance before any savings claim.
- If proxy env is ignored, WSS bypasses it, or the client rejects the
  Slimference CA before bytes flow, the product reports Desktop Slimference as
  blocked instead of faking success.
- Any CA trust requirement is explicit, reversible, and visible in Status.
- Any parse drift or unsupported frame shape fails open to byte-equal tunnel.

## Decision Tree

1. Build scoped proxy launch support without enabling it by default.
2. Run `--probe` first and verify only the spawned Codex.app process tree would
   receive Electron proxy arguments and proxy env.
3. Prefer a real launch with process-local custom CA env:
   `CODEX_CA_CERTIFICATE` plus generic CA hints from T242. Keychain trust is a
   fallback/lab branch, not the first product proof.
4. Launch Codex.app from a quiet external terminal, not from the active Codex
   Desktop session.
5. Send one minimal Desktop prompt.
6. Check `lsof` and `/_slimference/admin/state` under `.wss`.
7. If conversation WSS is proxied and clean, run a mutation-triggering Desktop
   prompt before claiming savings.
8. Quit the Slimference-launched app and relaunch Codex.app from Finder.
9. Prove Finder launch is direct again.
10. While Slimference-launched Codex.app is running, prove Browser ChatGPT and
    ChatGPT.app remain direct.
11. If any step fails, Desktop Slimference remains blocked with a precise
    reason; normal direct Desktop remains outside the TUI.

## Sub-Tasks

- [x] Reconcile stale Desktop docs: record that base-URL env injection is
  process-local but insufficient for current Codex.app conversation routing.
- [x] Decouple CONNECT/MITM ingress from global `transparent.enabled` so a
  scoped Desktop proxy mode can run without arming global lab routing.
- [x] Add a product-scoped proxy profile for Codex Desktop:
  intercept `chatgpt.com` only, use existing WSS Phase-F bridge, and bypass
  audio/realtime paths.
- [x] Extend `codex launch-desktop` with a proxy transport mode that injects
  Electron `--proxy-server` / `--proxy-bypass-list` arguments plus
  `HTTP_PROXY`, `HTTPS_PROXY`, `WSS_PROXY`, `ALL_PROXY`, lowercase variants,
  and `NO_PROXY=127.0.0.1,localhost,::1`.
- [x] Keep the existing base-URL launcher mode as diagnostic/future-proof, but
  do not present it as the active Desktop conversation route.
- [x] Add CA trust preflight for the original Keychain branch: refuse proxy
  launch with a clear instruction when the Slimference CA is not trusted.
  T242 supersedes this as the preferred proof branch by trying
  `CODEX_CA_CERTIFICATE` first.
- [x] Add `--probe` output that shows exactly which env keys would be injected
  without spawning Codex.app.
- [x] Add status observation fields for Desktop launch mode: direct,
  slimference-launched, proxy-env-present, proxy-connected, last WSS route,
  last telemetry timestamp.
- [x] Add tests for env construction, CA preflight, CONNECT routing activation,
  WSS upgrade handoff, bypass paths, and fail-open behavior.
- [x] Run controlled live proof from outside the active Codex Desktop session:
  launch via Slimference, send one prompt, collect `lsof`,
  `/_slimference/admin/state` under `.wss`,
  config hash, and direct Browser/ChatGPT.app control evidence.
- [x] Close the proxy mutation follow-up as not applicable: prompt proof never
  produced application bytes, so there was no safe Desktop proxy mutation prompt
  to run on this branch.
- [x] Add explicit failure-class reporting for the observed zero-byte CONNECT
  branch as `tls_trust_rejected`.
- [x] Extend explicit failure-class reporting:
  `proxy_env_not_inherited`, `connect_not_attempted`, `tls_untrusted`,
  `embedded_root_store`, `wss_bypassed_proxy`, `wss_parse_drift`,
  `proxied_but_no_mutation_candidate`, `passed`.
- [x] Add a `codex desktop status` or equivalent probe output so T239 can render
  Desktop truth without scraping logs.
- [x] Add `--with-ca-env` diagnostic launch mode to inject process-local
  `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and
  `NODE_EXTRA_CA_CERTS` for a final root-store compatibility probe.
- [x] Update `--with-ca-env` to inject Codex's own
  `CODEX_CA_CERTIFICATE=<slimference-root.crt>` first; this is now the
  preferred T242 branch before Keychain trust.
- [x] Document the final branch decision in `docs/install.md` and the operation
  log: proven proxy mode, blocked Slimference mode, or upstream-required
  blocker.

## Engineering Plan

1. **Scoped ingress:** keep the existing global lab CONNECT/MITM code intact,
   but add a product-scoped Desktop proxy profile that can be enabled without
   `transparent.enabled`, `sni_peek_mode`, hosts, pfctl, or macOS system proxy.
2. **Host policy:** intercept only `chatgpt.com` for Desktop proof. Keep all
   other CONNECT hosts byte-equal passthrough or blocked by explicit allowlist,
   depending on the existing proxy policy.
3. **WSS reuse:** reuse the existing `WebSocketTunnel` and Phase-F dispatcher.
   Do not fork a second WSS parser or a Desktop-only mutation path.
4. **Process-local route set:** pass Electron `--proxy-server` /
   `--proxy-bypass-list` arguments and inject `HTTP_PROXY`, `HTTPS_PROXY`,
   `WSS_PROXY`, `ALL_PROXY`, lowercase variants, `NO_PROXY`, and any
   Codex-specific proxy guard env found in the binary inspection only into the
   spawned process.
5. **Trust gate:** product path first tries process-local custom CA env. If
   that fails and a Keychain branch remains useful, Keychain trust must be an
   explicit Desktop/Lab fallback with one clear repair command. CLI WSS never
   depends on this gate.
6. **Observation:** persist a Desktop launch observation record containing app
   version, app-server PID, env mode, first loopback connect timestamp, WSS
   route mode, parse/degrade/compression counters, and direct-control checks.
7. **Fallback:** if the proxy path fails, leave the machine in the exact direct
   state and record the failure class. Do not retry by arming global lab mode.
8. **Docs:** `docs/install.md`, `docs/todo.md`, and operation log must say the
   same thing: Desktop proven, or Desktop Slimference blocked.

## Live Proof Matrix

| Proof | Required Evidence |
|---|---|
| Spawn scope | app-server env contains proxy keys and main process contains Electron proxy args; normal Finder launch env does not |
| Loopback route | `lsof` shows app-server and Chromium NetworkService connected to `127.0.0.1:<port>` |
| No direct conversation | no app-server or Chromium NetworkService direct `chatgpt.com:443` conversation socket during Slimference launch |
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
- Current base-URL launcher: process-local base-URL env injection, useful as a
  probe, not sufficient for conversation routing on Codex.app 0.131.0-alpha.9.
- T238 pre-live implementation added `[transparent].scoped_desktop_proxy=true`.
  When local CA material already exists, the daemon accepts process-local
  CONNECT on the normal loopback port without enabling global transparent mode,
  hosts, pfctl, or system proxy settings. It does not generate CA material just
  because the scoped Desktop ingress is enabled.
- `slimference codex launch-desktop` defaults to `--transport=proxy`, passes
  process-local Electron proxy arguments, injects process-local proxy env only,
  and keeps `--transport=base-url` as diagnostic/future-proof mode.
- `--with-ca-env` is now a legacy Desktop proxy diagnostic branch. It tries
  Codex-specific and generic root-store env hooks in the spawned process without
  touching shell startup files, Keychain, or system proxy settings. It is not a
  product success path unless live bytes and WSS frames prove it.
- `slimference codex desktop status` is the read-only handoff surface for
  Desktop proof: it reports CA gate, daemon reachability, WSS counters, and
  whether a live Desktop conversation has been observed.
- After the 2026-05-19 live proof, `codex desktop status` must not report
  "ready" when historical counters show `mitm_bridged>0` with zero bytes and
  zero upstream dial failures. That is a Desktop TLS/root-store blocked state.
- The WebSocket bridge gate now permits Phase-F only for
  `chatgpt.com/backend-api/codex/responses` with the Codex
  `responses_websockets` subprotocol. Sideband WSS stays byte-equal.
- 2026-05-22 Electron proxy-argument follow-up: the launched Codex.app main
  process carried `--proxy-server=http://127.0.0.1:8990` and
  `--proxy-bypass-list=localhost;127.0.0.1;::1`; Chromium NetworkService
  stopped opening direct ChatGPT sockets, but a real prompt still yielded
  only CONNECT/MITM activity with zero application bytes, zero WSS frames, and
  zero Phase-F mutation. This closes the Chromium-bypass branch and leaves the
  Desktop blocker at TLS/root-store trust.

Live proof must not be run from this active Codex Desktop session unless the
operator explicitly accepts the risk of focus/process disruption. A separate
terminal or a quiet window is required for the proof ceremony.

T238 intentionally does not add a new normal user surface. It only proves the
Desktop branch and exposes enough status for T239 to render the right button
behavior.

## Deviations

None yet.
