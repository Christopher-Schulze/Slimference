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
- [ ] Update `docs/install.md` with the human flow: normal launch is direct,
  Slimference launch goes through the launch center.

## Notes

The user's preferred UX is launch-based, not switch-based:

- Normal app/CLI launch means direct mode.
- Slimference TUI launch means optimized mode.
- Enable/disable remains useful for persistent scoped route and repair, but it
  should not be the primary story if the launch center can own the clean path.

This is lower cognitive complexity than independent always-on toggles for CLI
and Desktop. It also gives a safe fallback when Codex Desktop changes: normal
launch still works while Slimference launch can refuse or downgrade.

## Deviations

None yet.
