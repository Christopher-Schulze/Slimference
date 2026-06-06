# TASK T301: TUI status and logs separation

## Why

The simplified home menu still leaked daemon PID/port/liveness into the global
header, and the old Status screen mixed runtime state with logs, flight records,
hook diagnostics, and export actions. The mass-market product surface needs one
plain home menu plus separate destinations: Status for state, Logs for evidence
and export.

## Acceptance

- Home view exposes Launch Codex CLI, Launch Codex App, Savings, Status, Logs,
  and Setup.
- Global header does not render daemon live/idle, PID, port, or session timer.
- Status view owns daemon running/PID/port, Codex mode, route state, Desktop
  state, CA/global-lab state, and safety state.
- Logs view owns flight recorder, hook-turn state, session log stream, and debug
  log export.
- Status does not render log/flight/hook blocks.
- Home remains menu-only and does not render status/log details.
- Focused TUI tests, repo CI, build, install/restart, and status checks pass.

## Sub-Tasks

- [x] Move daemon header state into Status.
- [x] Split Logs out of the old Status/Debug view.
- [x] Add Logs to the home menu.
- [x] Update docs/tests.
- [x] Run gates, rebuild, install, restart, and commit.

## Notes

- `ViewDebug` remains the persisted internal tag for Status compatibility.
- Logs is a separate view and can be opened from the home menu; no extra hotkey
  was added because `l` conflicts with right-navigation and the product surface
  should stay menu-led.

## Deviations

- None.
