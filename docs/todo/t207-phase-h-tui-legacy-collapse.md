# TASK 207: Phase H TUI visible-surface collapse

Status: DONE 2026-05-17
Priority: P1
Scope: `internal/tui/`, `cmd/slimference/main.go`, TUI tests

## Why

The TUI must show the real Phase H surface: install, enable, disable, uninstall, status, app toggles. It must not tell the user to run legacy `proxy install` / `proxy enable`, and it must not present Claude Code as part of the active default target.

## Acceptance

- TUI setup and quick-start copy points to `slimference install`, `enable`, `disable`, `uninstall`, `status`.
- The service adapter used by the TUI calls the top-level Phase H lifecycle functions, not the old `proxy` command implementation.
- Apps view remains visible and Codex-focused.
- Claude Code is rendered as off / opt-in later, and pressing the old Claude toggle no longer mutates proxy routing.
- Existing method names such as `InstallTransparent` may remain as internal compatibility shims, but they must delegate to Phase H commands.

## Sub-Tasks

- [x] Route `serviceControlAdapter` lifecycle methods through `runInstallCmd`, `runEnableCmd`, `runDisableCmd`, `runUninstallCmd`.
- [x] Add injection points for tests around those command functions.
- [x] Update setup steps to Codex-only Phase H commands.
- [x] Update dashboard, setup, quick-start, and footer copy.
- [x] Disable the Claude provider shortcut in the visible default flow.
- [x] Update TUI and adapter tests.

## Verification

- `go test ./internal/tui ./cmd/slimference -count=1 -timeout 180s`

## Notes

This is not a full TUI rewrite. The deeper API cleanup from old method names to `Install/Enable/Disable/Uninstall` remains optional refactor work. The user-visible and command-executing surface is now Phase H.
