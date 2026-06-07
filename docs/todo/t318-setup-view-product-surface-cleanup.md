# T318 - Setup view product-surface cleanup

Status: Done.

## Why

Setup still exposed old operator/debug controls on the daily product surface:
advanced shared route, global lab, uninstall assets, and command-list noise.
That conflicted with the intended scoped UX: normal Codex stays direct, and only
sessions launched from Slimference use Slimference.

## Acceptance

- Setup renders only install/repair state, daemon controls, autostart, and app
  routing access.
- Setup has four visible setup steps and no advanced shared-route step.
- Setup does not advertise advanced shared route, global lab, or uninstall
  controls.
- `r`, `g`, and `u` in Setup no longer mutate route/lab/uninstall state.
- Generated keybinding docs no longer list removed Setup controls.
- Tests enforce the simplified Setup surface.
- Latest local binary is rebuilt and installed after the change.

## Sub-Tasks

- [x] Remove the advanced shared-route setup step.
- [x] Convert Setup `r`, `g`, and `u` keys to CLI-only safety messages.
- [x] Reduce Setup rendering to install/repair and daemon cards.
- [x] Remove advanced route/lab/uninstall controls from generated key docs.
- [x] Update documentation and TODO index.
- [x] Update tests that previously enforced the old Setup surface.

## Notes

- CLI lab and advanced route commands remain available outside the TUI for
  explicit operator/dev use.
- A machine-wide route warning is still shown if such a route is already armed,
  because hiding an active global route would be unsafe.

## Deviations

None.
