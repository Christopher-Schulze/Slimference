# T315: Activity View Live-Signal Cleanup

## Why

Activity was mixing old hook turn-state JSON with real Slimference route
signals. That made direct Codex sessions, stale historical hook files, and
actual Slimference-routed traffic look like the same thing. For daily UX this is
wrong: Activity must answer one question only: what is currently running through
Slimference?

## Acceptance

- Activity does not read or render old hook turn-state files.
- Activity shows a small current-state card:
  - routed when a recent Flight record exists,
  - launched/waiting after a TUI Slimference launch before first traffic,
  - Desktop app-server active when that scoped process signal exists,
  - idle when no Slimference route signal exists.
- Recent traffic shows only routed Slimference request records.
- Direct Codex windows are not listed as active Slimference sessions.
- Hook turn-state remains available under Logs for diagnostics only.
- Tests prove stale hook data cannot leak into Activity.
- Updated binary is built, installed, and daemon-restarted after verification.

## Sub-Tasks

- [x] Remove hook-state Activity rendering.
- [x] Add model-local launch-pending state for TUI-launched Codex CLI/App.
- [x] Keep Activity focused on NOW + recent routed requests.
- [x] Add regression tests for stale hook-state suppression.
- [x] Update product documentation.
- [x] Run targeted TUI/launch tests and full CI.
- [x] Build and install the latest binary.

## Notes

- This is intentionally not a process-list view. Process inspection would be
  noisy and fragile. The reliable product signal is: TUI launch happened,
  app-server scoped status is active, or the daemon recorded a Flight.
- Old hook-turn files are still useful for diagnosing hooks and parser/recovery
  state, so they remain visible in Logs.
