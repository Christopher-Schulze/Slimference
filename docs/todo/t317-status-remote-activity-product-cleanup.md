# T317 - Status and remote Activity product cleanup

Status: Done.

## Why

The TUI Status page mixed daily product state with advanced route vocabulary:
Normal Codex, advanced shared route, global lab, transport details, and safety
internals. That made the scoped launch model harder to understand. Activity
also stayed stuck in a launch/waiting state when the TUI used the remote daemon
adapter because `GetRecentFlights` returned no daemon flight telemetry.

## Acceptance

- Status renders only product operator checks: daemon runtime, install
  readiness, and health.
- Status normal operation does not render `CODEX MODE`, `NORMAL CODEX`,
  `GLOBAL LAB`, or `SAFETY`.
- Activity reads real daemon flight telemetry through the admin status API.
- Direct Codex windows and stale hook-turn state stay out of Activity.
- Tests verify both the UI wording guard and the remote flight transport.
- Latest local binary is rebuilt and installed after the change.

## Sub-Tasks

- [x] Add recent flight telemetry to `AdminStatus`.
- [x] Teach `remoteProxyAdapter.GetRecentFlights` to read the admin snapshot.
- [x] Simplify Status into Daemon, Install, and Health cards.
- [x] Keep advanced route/lab vocabulary out of normal Status rendering.
- [x] Update TUI, admin, and remote-adapter regression tests.
- [x] Update documentation and TODO index.

## Notes

- The Activity bug was not a Desktop typing-state issue. The proxy already had
  normalized flight records; the remote TUI adapter simply dropped them.
- Lab/global-route state is still surfaced only when it becomes a real warning.
  A disarmed lab installation is no longer daily Status noise.

## Deviations

None.
