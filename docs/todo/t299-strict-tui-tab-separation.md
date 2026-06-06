# TASK T299: Strict TUI tab separation

## Why

The Launch tab still leaks setup, savings, status, and diagnostics content even
after the first declutter pass. Product UX must be literal: Launch only launch
actions and launch explanation; Savings only metrics; Status only health/logs;
Setup only install/repair/advanced controls.

## Acceptance

- Launch tab renders only Codex CLI/App launch actions and launch-mode
  explanation.
- Launch tab does not render setup warnings, savings state, status action,
  setup action, diagnostics handoff, current-session savings, health, or daemon
  detail blocks.
- Navigation tabs/key hints may still link to Savings/Status/Setup.
- Tests enforce that Launch does not leak `SETUP`, `Savings`, `Status`,
  `Setup missing`, `CURRENT SESSION`, `HEALTH`, or `DIAGNOSTICS` body blocks.
- Focused TUI tests and relevant package tests pass.
- Current binary is installed/restarted after the fix.

## Sub-Tasks

- [x] Restrict Launch view to launch-only content.
- [x] Update tests and docs for strict tab separation.
- [x] Run gates and install/restart.

## Notes

- User screenshot showed Setup state on Launch. Treat this as a UX correctness
  bug, not a cosmetic issue.
- `dashboardActions()` now exposes only `launch_cli` and `launch_app`.
- `buildLeftPanel()` no longer renders setup readiness, global lab, bypass,
  inspect/manage groups, cache/checkpoint/archive shortcuts, or health blocks.
- `buildRightPanel()` now renders only `LAUNCH MODE` and `HOW IT WORKS`.
- TUI tests assert that Launch does not leak session savings, provider maps,
  health, diagnostics, setup warnings, or internal mechanism counters.
- Verification passed:
  - `go test ./cmd/slimference ./internal/tui ./docs`
  - `go run ./scripts/ci`
  - `go run ./scripts/build --restart`
  - `/Users/christopher/.local/bin/slimference status --preflight`
  - `/Users/christopher/.local/bin/slimference codex status`

## Deviations

- None.
