# TASK 243: WSS-first auto transport ladder

Status: PARTIAL - CLI auto ladder, WSS bridge path, bridge proof state, TUI
state, tests, certified-tuple live proof, and successful recert restore proof
landed; fallback-branch live proof and non-CLI passthrough audits remain
Priority: P0 after T241 recert core design, before T240 release seal
Scope: Codex CLI transport selection only; Desktop adopts the same ladder only
after T246 proves Desktop can route bytes through Slimference safely through the
process-local app-server shim

## Why

The user requirement is WSS as the standard because the goal is MAXXED
Slimference savings and maximum compatibility with Codex's native agentic
transport. HTTP is a valid stable fallback, but it must not be the first
fallback when WSS mutation is not certified.

The correct ladder is:

1. `wss_phasef` - native WSS plus Phase-F mutation and `permessage-deflate`
   re-encoding. This is the target and the max-savings path.
2. `wss_bridge` - native WSS byte-equal bridge. This keeps Codex on its native
   WebSocket route when mutation is temporarily unproven. It has no Phase-F
   savings, but it avoids an unnecessary transport shift to HTTP.
3. `http` - stable scoped Responses HTTP fallback. Use only when WSS itself is
   not healthy enough for bridge mode.
4. `direct` - final fail-open when Slimference cannot safely serve the request.

This task prevents false either/or behavior. A stale WSS Phase-F certificate
should not automatically mean HTTP. It should mean: keep WSS native if the WSS
tunnel is safe, run T241 auto-recert, and return to `wss_phasef` as soon as the
capability proof is green.

## Acceptance

- `transport=auto` resolves through an explicit ladder:
  `wss_phasef -> wss_bridge -> http -> direct`.
- `wss_phasef` is selected only when the current Codex/Slimference tuple has a
  green WSS Phase-F certification:
  - schema/profile/transport match;
  - current Codex version matches cert;
  - current Slimference version matches cert;
  - `passed=true`;
  - no parse failures;
  - no degraded sessions;
  - no compression errors;
  - live Phase-F mutation proof was recorded by the cert command.
- `wss_bridge` is selected when WSS bridge capability is safe but Phase-F
  mutation is not certified, such as:
  - Codex version drift with no new Phase-F cert yet;
  - Slimference version drift with no new Phase-F cert yet;
  - T241 auto-recert currently running;
  - T241 auto-recert recently failed due missing mutation but WSS handshake and
    byte-equal bridge remained clean;
  - operator explicitly disables mutation for debug.
- `wss_bridge` must not mutate, rewrite, compact, prune, streamcut, or re-encode
  application payloads. It preserves the WebSocket path and forwards frames
  byte-equal except for unavoidable tunnel mechanics.
- `wss_bridge` has its own lower-risk proof/state separate from Phase-F cert:
  a successful exact-reply WSS smoke or bridge observation must prove handshake,
  upstream dial, sent/received bytes, no parse failures, no degraded sessions,
  and no compression errors.
- If no current bridge proof exists, `auto` may perform a cheap bridge health
  probe or use the most recent valid bridge state, but it must not block the
  user's interactive launch for a long proof run.
- Bridge probing should be opportunistic and cheap: use bounded exact-reply or
  minimal WSS handshake/byte observations, persist the result with a short TTL,
  and let T241 run the heavier mutation proof in the background. The goal is to
  make `http` a rare fallback, not the normal post-update state.
- `http` is selected only when WSS bridge is unavailable or unsafe:
  - daemon reachable but WSS handshake fails;
  - upstream WSS dial fails;
  - repeated WSS bridge exact-reply smoke fails;
  - WSS produces parse/degrade/compression errors even in byte-equal mode;
  - WSS policy explicitly disabled.
- `direct` is selected only when Slimference cannot provide a safe scoped route,
  such as daemon unreachable and no local fallback route can be served.
- T241 auto-recert integrates with this ladder:
  - drift sets `needs_recert=true`;
  - auto-recert starts in background;
  - active user sessions get `wss_bridge` when available;
  - after successful recert, subsequent sessions get `wss_phasef`;
  - after failed recert, sessions remain `wss_bridge` if bridge proof is clean,
    not HTTP by default.
- Status JSON exposes both layers separately:
  - selected auto mode: `wss_phasef`, `wss_bridge`, `http`, or `direct`;
  - Phase-F cert state;
  - WSS bridge state;
  - active recert state;
  - exact fallback reason;
  - recommended repair action.
- Human status and TUI use the same color semantics:
  - green: WSS Phase-F savings active;
  - yellow: WSS native bridge active, savings repair running or needed;
  - orange: HTTP fallback active because WSS bridge is unsafe;
  - red: direct fallback / daemon repair needed.
- The launch-center UX does not change. `Launch Codex CLI` still starts
  `transport=auto`; the auto resolver chooses the best safe mode.
- Normal direct Codex launches remain direct. This ladder applies only to
  Slimference-launched scoped Codex CLI sessions.
- Desktop gets this same priority order only after T246 proves real Desktop
  conversation bytes through Slimference. CA env present, Keychain trust,
  provider badge, or CONNECT acceptance alone do not qualify Desktop for this
  ladder.
- Browser ChatGPT, ChatGPT.app, Claude Code, `/etc/hosts`, pfctl, macOS system
  proxy, and persistent shell env remain untouched.
- Audio, Realtime, Voice, and non-conversation WSS paths are passthrough only.
  They are not optimization targets and must not be included in savings claims.
- WSS terminal streamcut remains excluded until T236 proves a protocol-correct
  terminal sequence. This ladder must not re-enable unsafe delta blanking.
- Logs are bounded and useful:
  - route decision events go to the existing bounded Slimference log surface or
    a new `~/.slimference/logs/transport.log`;
  - max active file size: 2 MiB;
  - one backup rotation;
  - no secrets or prompt dumps;
  - include selected mode, rejected modes, reason, tuple, cert id, bridge proof
    id, and recert attempt id.

## Sub-Tasks

- [x] Introduce explicit transport mode vocabulary:
  `wss_phasef`, `wss_bridge`, `http`, `direct`.
- [x] Extend `codexroute.AutoDecision` or its successor with selected mode,
  rejected modes, bridge proof state, recert state, and exact fallback reason.
- [ ] Keep existing `transport=wss` power mode available, but define whether it
  means "force WSS Phase-F when certified" or "force WSS transport with bridge
  fallback"; document the operator-facing behavior before implementation.
- [x] Add WSS bridge proof storage under `~/.slimference/` with tuple, timestamp,
  bridge counters, exact-reply sentinel, and failure reason.
- [ ] Add a cheap WSS bridge smoke/probe that does not mutate payloads and can
  prove handshake/upstream/bytes without running the full Phase-F recert.
- [ ] Make the bridge smoke/probe non-blocking where possible: launch-center
  refresh and first CLI launch may start it in the background, but interactive
  Codex work should proceed through the best known safe state.
- [x] Update `transport=auto` resolution order to prefer `wss_bridge` before
  `http` whenever Phase-F cert is stale but bridge proof is green.
- [x] Integrate T241 recert state so active/failed recert attempts influence
  auto mode without duplicating recert logic.
- [x] Update proxy run flags and route profile labels so telemetry can
  distinguish `websocket_phasef` from `websocket_bridge`.
- [x] Ensure `wss_bridge` bypasses Phase-F mutation, output streamcut, response
  pruning, and any re-encoding that is not required by the tunnel.
- [x] Add status JSON and human output for green/yellow/orange/red ladder
  states.
- [x] Update TUI launch-center state display:
  "WSS Savings active", "WSS native bridge - repairing savings", "HTTP fallback
  - WSS unsafe", and "Direct fallback - repair daemon".
- [x] Add tests for every ladder branch:
  certified Phase-F; stale cert plus bridge proof; recert running plus bridge
  proof; recert failed plus bridge proof; bridge unsafe -> HTTP; daemon down ->
  direct.
- [x] Add tests proving `wss_bridge` does not call Phase-F mutators and does
  not increment mutation/re-encoding counters.
- [ ] Add tests proving Audio/Realtime/Voice routes remain passthrough and do
  not affect WSS savings state.
- [ ] Add bounded transport-decision logging and log-rotation tests.
- [ ] Add live negative-branch runbook and evidence capture for:
  simulated Codex drift -> WSS bridge, bridge proof expiry -> cheap probe,
  forced WSS bridge failure -> HTTP, daemon down -> direct.
- [x] Update `docs/install.md`, `docs/documentation.md`, and T240 release
  evidence requirements with the final ladder.
- [~] Run live Codex CLI proof:
  - [x] certified tuple -> `wss_phasef`;
  - simulated cert drift -> `wss_bridge` while auto-recert runs;
  - [x] successful recert -> `wss_phasef` restored;
  - forced WSS bridge failure -> `http`;
  - daemon down -> direct fail-open.

## Notes

This task is not permission to reduce savings ambition. The steady-state goal
is always `wss_phasef`. `wss_bridge` exists only so incompatible or unproven
mutation does not throw Codex off its native WSS route unnecessarily.

The ladder should be implemented as a deterministic state machine, not scattered
if/else branches:

1. Gather facts: daemon, current tuple, Phase-F cert, bridge proof, recert
   state, operator flags.
2. Evaluate `wss_phasef`.
3. Evaluate `wss_bridge`.
4. Evaluate `http`.
5. Fall open to `direct`.
6. Emit one bounded decision event with all rejected candidates and reasons.

Open engineering questions to settle before code:

- Whether `--transport=wss` should force Phase-F only or force WSS transport
  with bridge fallback. Recommended: explicit flags keep operator clarity:
  `--transport=wss` means WSS transport, `--require-phasef` means fail if
  Phase-F cannot be used.
- Whether bridge proof is per Codex version, per Slimference version, or both.
  Recommended: both, but with shorter proof than Phase-F recert.
- Whether bridge proof can be inferred from a recent successful byte-equal
  live session. Recommended: yes, if counters show bytes in/out and zero error
  counters inside a bounded recent window.
- Whether auto-recognition of "WSS unsafe" should be sticky. Recommended:
  sticky with cooldown, but repairable by TUI force repair.

Engineering bias for the remaining work:

- Treat `wss_phasef` as the steady state and spend extra effort to keep it
  there: reliable T241 mutation trigger, tuple-scoped bridge proof, cooldown
  instead of panic fallback, and exact reasons in status.
- Treat `wss_bridge` as the safety net, not a product victory. It preserves the
  native WSS route but does not claim Phase-F savings.
- Treat `http` as emergency compatibility. It is valid, but it should only
  appear when WSS itself is unsafe, not when mutation merely needs repair.
- Treat `direct` as fail-open. It protects Codex UX; it is not a Slimference
  savings mode.

2026-05-19 implementation pass:

- `codexroute.AutoMode` now exposes `wss_phasef`, `wss_bridge`, `http`, and
  `direct`; `AutoDecision` includes rejected modes, bridge proof path, recert
  path/status, and repair command.
- `transport=auto` selects certified WSS Phase-F first. If the Phase-F cert is
  stale but `codex-wss-bridge.json` is green for the current tuple, it selects
  `wss_bridge` before HTTP.
- `slimference codex run --transport=auto` maps `wss_bridge` to
  `proxy run codex --proxied-wss-bridge`, which routes through the
  `/backend-api/codex-bridge/responses` alias and canonicalizes upstream to the
  native Codex WSS path.
- The bridge dispatcher calls `wsmitm.Session` with no Phase-F handlers. It is
  intentionally byte-equal compatibility mode: no mutation, no output streamcut,
  no response pruning, no savings claim.
- TUI Launch Center shows WSS savings, WSS bridge/repairing, HTTP fallback, and
  daemon repair states through the existing five-item surface.

2026-05-19 live proof update:

- The current certified tuple is `codex-cli 0.131.0` plus Slimference 2.0.2.
- `slimference codex recertify wss --force --no-write --json` produced a real
  Phase-F mutation with `frames_reencoded=1`,
  `compressed_messages_mutated=1`, `parse_failures=0`,
  `degraded_sessions=0`, and `compression_errors=0`.
- `~/.codex/config.toml` stayed bit-identical before and after the recert
  trigger, proving the strengthened trigger does not add temporary project
  trust entries.
- `slimference codex status --json` resolved `auto.mode=wss_phasef`,
  `auto.transport=wss`, `auto.wss_certified=true`, and `needs_recert=false`.
- A live `slimference codex run --transport=auto -- exec ...` returned its
  sentinel through provider `slimference-codex`, confirming that the normal
  launch path selects the WSS Phase-F lane. Remaining T243 live work is the
  negative branch matrix: simulated drift to bridge, forced bridge failure to
  HTTP, daemon-down direct fail-open, and Audio/Realtime/Voice passthrough.

## Deviations

None yet.
