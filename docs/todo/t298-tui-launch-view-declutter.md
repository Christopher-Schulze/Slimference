# TASK T298: TUI launch view declutter

## Why

The Launch view currently mixes product launch actions with live traffic,
provider maps, checkpoints, tool archives, cache internals, and debug-flavored
route wording. That makes the daily surface noisy and hard to read. Launch must
stay a simple product entry point: pick Codex CLI/App, understand that normal
Codex remains direct, see compact readiness/safety/session status, and move
details to Savings, Status, or Setup.

## Acceptance

- Launch view removes internal dashboard blocks from the daily surface:
  `TRAFFIC`, `PROVIDER MAP`, `LIVE`, `CHECKPOINTS`, `TOOL ARCHIVE`, and
  optimization internals are not rendered there.
- Launch action states use product wording instead of WSS/transport/internal
  wording.
- Right panel shows only Slimference mode, current session summary, health, and
  diagnostics handoff.
- Savings, Status, and Setup remain the detail locations for metrics, logs,
  daemon/service state, and advanced controls.
- Focused TUI tests and relevant package tests pass.
- Current binary is installed/restarted after the fix.

## Sub-Tasks

- [x] Simplify Launch view rendering and labels.
- [x] Update TUI tests for the new information architecture.
- [x] Update docs if product wording changed.
- [x] Run focused tests and install/restart.

## Notes

- User screenshot showed overlapping/noisy Launch UI. Treat that as the product
  bug: too many internal surfaces rendered in the first screen.
- Removed Launch-view rendering for `TRAFFIC`, `PROVIDER MAP`, `LIVE`,
  `CHECKPOINTS`, `TOOL ARCHIVE`, and optimization internals. Savings/Status
  retain detail views.
- Launch action states now use product wording such as `savings active`,
  `ready`, `safe fallback`, `repairing`, and `repair needed`.
- Added a Launch width regression test so rendered lines cannot exceed the
  configured TUI width and recreate the screenshot overlap.
- Verification: `go test ./internal/tui`; `go test ./cmd/slimference ./internal/tui ./docs`; `go run ./scripts/ci` PASS, 8/8 steps, aggregate coverage 96.2%.
- Installed via `go run ./scripts/build --restart`; daemon restarted on port
  8990 as PID 29258.
- Installed checks: `slimference status --preflight` green; normal Codex direct;
  advanced shared route off; `slimference codex status` daemon reachable.

## Deviations

- None.
