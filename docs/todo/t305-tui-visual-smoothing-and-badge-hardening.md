# T305 TUI visual smoothing and Desktop badge hardening

Status: Done.

## Why

The simplified TUI menu was functionally correct, but the cyan/turquoise accent
looked too harsh and the visible keybinding legend consumed space without
adding value. The Desktop Slimference provider chip also needed to be harder to
lose after Codex Desktop changes its config read request details.

## Acceptance

- TUI menu structure remains unchanged.
- Visible footer keybinding legends are removed from the main, savings, status,
  logs, and setup screens.
- The visual accent moves away from turquoise to a warmer, smoother palette.
- The scoped Desktop app-server shim injects the Slimference provider chip for
  any app-server `result.config` response, not only a previously tracked
  `config/read` response.
- Normal Finder/Spotlight/terminal Codex starts remain direct; the badge remains
  scoped to Slimference-launched Codex.app.
- Go tests and final CI pass.

## Sub-Tasks

- [x] Soften the TUI accent and semantic colors without changing menu layout.
- [x] Remove visible footer keybinding legends from rendered TUI screens.
- [x] Harden Desktop badge config injection against config/read method/id drift.
- [x] Update focused tests for hidden legends and untracked config response
  rewriting.
- [x] Rebuild and install the newest binary.

## Notes

- The key bindings themselves still exist; only the always-visible legend was
  removed.
- The app-server shim still only mutates the scoped Slimference Desktop launch.
  It does not write persistent Codex config and does not affect direct app
  starts.
