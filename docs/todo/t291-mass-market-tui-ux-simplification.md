# T291: Mass-market TUI UX simplification

## Why

The TUI works for power users, but the default surface still exposes internal
terms and lab controls too prominently. The product should be easy to operate:
launch Codex through the proven paths, see savings, see status, and repair or
install from Setup without confusing global lab controls with normal daily use.

## Acceptance

- The top-level TUI navigation is limited to Launch, Activity, Savings, Status,
  Logs, and Setup.
- Normal users see Launch Codex CLI, Launch Codex App, Activity, Savings,
  Status, Logs, and Setup as the primary product actions.
- Advanced lab / MITM controls remain available but are hidden under Setup and
  never advertised as the normal path.
- Daemon start/restart/repair and install/enable flows are visible from Setup.
- User-facing labels avoid stale "Layer 2" summary ambiguity; cache and
  deterministic compression are named plainly.
- Keybinding documentation matches the real TUI surface.
- TUI and CLI tests pass; final repo gate passes before Done.

## Sub-Tasks

- [x] Verify current TUI navigation, Setup flow, keybindings, and tests.
- [x] Simplify the main TUI navigation and dashboard language.
- [x] Move apps and global lab controls behind Setup wording.
- [x] Update keybinding docs and product documentation.
- [x] Run focused TUI/CLI tests and full CI.

## Notes

- Existing backend functions must remain available; this task changes the
  product surface and recovery guidance, not the safe savings mechanisms.
- Direct Codex remains the native launch outside Slimference. Slimference mode
  remains the TUI/CLI scoped launch path.
- `go test ./internal/tui` passed after the navigation/keybinding rewrite.
- `go test ./internal/tui ./cmd/slimference` passed after Setup/Status action
  updates.
- `go test ./...` passed after the docs and behavior updates.
- `go run ./scripts/ci` passed all 8 steps with 96.5% aggregate statement
  coverage and a passing live-corpus gate.

## Deviations

- None.
