# T312 Activity view and Desktop indicator cleanup

Status: Done.

## Why

The Desktop menu-bar indicator was the wrong product surface. The current
Desktop app stays without an external indicator, while route visibility moves
to existing Slimference-owned surfaces. The TUI also needed a simple Activity
view so users can see which Slimference sessions and recent routed traffic exist
without opening raw logs.

## Acceptance

- Desktop app-server shim does not start a macOS menu-bar/status indicator.
- No Desktop overlay, Dock badge, model metadata change, or service-tier
  display workaround remains in the product path.
- CLI sessions still use the terminal tab/window title signal.
- TUI main menu contains `Activity`.
- Activity shows real Hook turn-state and Flight Recorder data when present.
- Activity does not invent missing cwd/session/title fields.
- Documentation states that current Desktop builds have no external indicator
  and that Activity/Status/logs are the route visibility surfaces.
- Focused tests cover CLI title behavior, shim routing without indicator
  ownership, Activity rendering, and menu expectations.

## Sub-Tasks

- [x] Remove the Desktop menu-bar helper from code and tests.
- [x] Remove shim ownership of any external Desktop indicator.
- [x] Add `Activity` as a first-class TUI view.
- [x] Render Hook turn-state session activity.
- [x] Render recent Flight Recorder traffic activity.
- [x] Update TUI menu/persistence tests for the new view.
- [x] Update `docs/documentation.md`, `docs/install.md`, and TODO records.

## Notes

- Activity uses `~/.slimference/turn-state/*.json` for Hook session/turn/file
  state and the proxy Flight Recorder for recent routed traffic.
- Hook state can provide session ID, turn ID, open/closed state, file read/edit
  counts, and git path-list CWD. It does not guarantee a human-readable Codex
  conversation title, so the TUI does not fake one.
- Flight records can provide provider/client/route/path/savings data. They do
  not guarantee cwd.

## Deviations

None.
