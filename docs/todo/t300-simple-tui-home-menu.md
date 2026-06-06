# TASK T300: Simple TUI home menu

## Why

The TUI home screen is still too text-heavy. A mass-market launch surface should
not look like a dashboard or tabbed control panel. The first screen should be a
plain menu: Launch Codex CLI, Launch Codex App, Savings, Status, Setup.

## Acceptance

- Home view has no top tabs/reiter.
- Home view shows only five menu entries: Launch Codex CLI, Launch Codex App,
  Savings, Status, Setup.
- Home view does not render explanatory right-panel copy, selected-action copy,
  setup details, savings details, status details, diagnostics, or mechanism
  counters.
- Savings, Status, and Setup remain separate views.
- Subviews offer an obvious Back path via `b`/`esc`; non-action detail views may
  also return with Enter.
- Focused TUI tests and repo CI pass.
- Newest binary is built, installed, daemon-restarted, and preflight-checked.

## Sub-Tasks

- [x] Replace launch dashboard with five-item home menu.
- [x] Simplify navigation/back keys and footer hints.
- [x] Update docs/tests.
- [x] Run gates, rebuild, install, restart, and commit.

## Notes

- Keep launch behavior scoped: normal Codex direct unless opened from the TUI or
  `slimference codex run`.
- TUI `b`/`esc` is now Back. Master bypass remains available through CLI/admin
  controls, and the TUI header still displays `BYPASS` if that state is active.
- Removed the unused tab renderer so top tabs/reiter cannot silently return.
- Verification passed:
  - `go test ./cmd/slimference ./internal/tui ./docs`
  - `go run ./scripts/ci`
  - `go run ./scripts/build --restart`
  - `/Users/christopher/.local/bin/slimference status --preflight`
  - `/Users/christopher/.local/bin/slimference codex status`

## Deviations

- None.
