# TASK 239: Slimference launch center TUI

Status: PLANNED
Priority: P0 after T238 proof branch is known
Scope: User-facing Slimference launch and management UX for Codex CLI and
Codex Desktop on macOS arm64

## Why

The user does not want a matrix of modes, lab commands, route patches, proxy
switches, and app-specific toggles. The normal product should feel like one
place to start and manage Slimference:

- Launch Codex CLI
- Launch Codex App
- Savings
- Status
- Manage Slimference

Direct mode does not need its own menu item. Direct mode is launching Codex
normally outside Slimference: `codex` in a normal shell, or Codex.app from
Finder/Spotlight. The Slimference TUI is for the Slimference-launched path and
for health, savings, install, repair, and uninstall.

This reduces complexity: instead of maintaining persistent per-app on/off
switches as the primary UX, the user chooses the launch path. Normal launch is
direct; Slimference launch is optimized. Persistent enable/disable commands
remain available under Manage for supported scoped config routes and recovery,
but they are not the main mental model.

## Target State

Running `slimference` opens a compact launch center. The user can start the
optimized CLI or optimized Desktop path, inspect real savings, check health,
and repair or remove Slimference from one place. Nothing in the launch center
arms global lab mode by accident.

The launch center is not a settings maze. It is a cockpit:

1. **Launch Codex CLI** starts the proven CLI path.
2. **Launch Codex App** starts the proven Desktop path if T238 passes; otherwise
   it says Desktop is currently direct-only.
3. **Savings** shows actual measured savings and separates estimates.
4. **Status** shows whether the machine is safe, healthy, and scoped.
5. **Manage Slimference** handles install, repair, uninstall, enable/disable,
   logs, and advanced lab controls behind explicit wording.

## Acceptance

- Top-level TUI has exactly these primary entries:
  - Launch Codex CLI
  - Launch Codex App
  - Savings
  - Status
  - Manage Slimference
- There is no top-level "direct open" action.
- Launch Codex CLI starts the existing safe Codex CLI product path with
  `transport=auto` and shows WSS certification/fallback state.
- Launch Codex App uses the T238 branch decision:
  - if process-local proxy proof passes, launch Codex.app in Slimference mode;
  - if proof fails, show Desktop as direct-only with a concise reason and do not
    pretend savings are active.
- Savings shows total, today, session, route, and mechanism attribution where
  the data exists; no fake Desktop savings.
- Status shows daemon, CA trust, WSS cert, Codex CLI version, Codex Desktop
  version, route mode, config drift, listener state, and last Desktop/CLI
  observation.
- Manage Slimference contains Install, Repair, Uninstall, enable/disable
  recovery actions, logs, and lab/advanced controls fenced away from normal use.
- Browser ChatGPT, ChatGPT.app, and Claude Code are explicitly shown as
  untouched/direct unless the user enters a lab path.
- All actions are reversible and fail open.
- The TUI never offers a top-level persistent Desktop toggle before T238 proves
  Desktop routing.
- The TUI can be used as the normal entry point without remembering CLI flags.

## Information Architecture

### Launch Codex CLI

- Shows detected Codex CLI version.
- Shows Slimference WSS certification tuple and fallback reason.
- Defaults to `transport=auto`.
- Lets the user enter or paste a prompt.
- Starts `slimference codex run --transport=auto -- exec ...`.
- If Slimference daemon is unhealthy, explains fail-open and offers direct run.

### Launch Codex App

- If T238 passed: launches Codex.app with the proven process-local proxy mode.
- If T238 failed or is unproven: displays direct-only state and why.
- Shows whether CA trust is required and present.
- Shows whether the currently running Codex.app was Slimference-launched or
  direct-launched.
- Does not restart or kill Codex.app without explicit confirmation.

### Savings

- Separates proxy input savings, WSS mutation savings, prompt-cache savings,
  output-reduce savings, hook/readhook savings, and estimates.
- Desktop savings are hidden or marked unavailable until T238/T240 prove them.
- Shows today/week/month/all plus last session when session attribution exists.
- Never mixes local hook savings into proxied Codex traffic totals unless the
  source is clearly labelled.

### Status

- Shows daemon health, binary path/SHA, config path, CA disk/trust state,
  listener state, Codex CLI route, Desktop route, WSS cert, drift fallback,
  and global lab disarmed/armed state.
- Shows Browser ChatGPT, ChatGPT.app, and Claude Code as untouched/direct in
  the normal product path.
- Surfaces exact repair actions instead of vague warnings.

### Manage Slimference

- Product actions: Install, Repair, Uninstall, Enable scoped route, Disable
  scoped route, Restart daemon, View logs.
- Advanced actions: CA trust, global lab enable/disable/root-arm/root-disarm,
  clearly labelled as lab/global.
- Every destructive or global action has a confirmation and shows the blast
  radius before execution.

## Sub-Tasks

- [ ] Design the final launch-center state model using existing
  `/admin/state`, `/admin/status`, `codex status`, and savings surfaces.
- [ ] Add a route-mode vocabulary shared by CLI, TUI, and docs:
  direct, slimference-cli-wss, slimference-cli-http, desktop-direct,
  desktop-proxy-proven, desktop-proxy-unproven, lab-global.
- [ ] Implement top-level menu entries exactly as accepted; do not add a
  direct-open item.
- [ ] Implement Launch Codex CLI as a guided wrapper around
  `slimference codex run --transport=auto --`.
- [ ] Implement Launch Codex App after T238 decides the Desktop branch.
- [ ] Fold current install/enable/disable/repair/uninstall controls into Manage
  Slimference with clear product vs lab separation.
- [ ] Show savings truth without mixing hook estimates, proxy savings, cache
  savings, and Desktop-unproven traffic.
- [ ] Add tests for menu structure, action routing, status wording, and no
  accidental lab/global activation from product actions.
- [ ] Add focused UX tests for the T238 branches: Desktop proven, Desktop
  unproven, Desktop failed by cert trust, Desktop failed by WSS bypass.
- [ ] Add golden text tests for user-facing wording so the app never claims
  Desktop savings before proof.
- [ ] Update `docs/install.md` with the human flow: normal launch is direct,
  Slimference launch goes through the launch center.

## Implementation Order

1. Define state structs and route-mode vocabulary.
2. Wire read-only Status and Savings first.
3. Wire Launch Codex CLI using the already-proven command path.
4. Wire Manage Slimference product actions.
5. Wire advanced/lab actions behind explicit lab labels.
6. Wire Launch Codex App only after T238 has a final branch decision.
7. Update install docs and operation log.
8. Hand to T240 for the final zero-drawdown release certification.

## Notes

The user's preferred UX is launch-based, not switch-based:

- Normal app/CLI launch means direct mode.
- Slimference TUI launch means optimized mode.
- Enable/disable remains useful for persistent scoped route and repair, but it
  should not be the primary story if the launch center can own the clean path.

This is lower cognitive complexity than independent always-on toggles for CLI
and Desktop. It also gives a safe fallback when Codex Desktop changes: normal
launch still works while Slimference launch can refuse or downgrade.

The existing `enable` / `disable` commands remain useful, but they move under
Manage. The normal daily decision is simply: launch through Slimference or
launch normally.

## Deviations

None yet.
