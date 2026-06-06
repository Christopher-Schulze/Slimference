# T308 Desktop patch-free route indicator

Status: Done.

## Why

The old in-composer provider chip is no longer a stable public surface in the
current Codex Desktop app-server contract. Mutating model names, model IDs,
selected model values, default service tiers, or service-tier metadata would
make the indicator fragile and would put the signal in the wrong product
contract. Users still need a visible confirmation that a Codex.app window was
started through Slimference instead of the normal direct path.

## Acceptance

- Scoped Desktop launches start a visible Slimference route indicator without
  patching the Codex Desktop bundle.
- The indicator is owned by Slimference, not by `model/list`, model metadata, or
  service-tier metadata.
- Normal Finder/Spotlight Codex.app launches and normal terminal Codex starts do
  not start the indicator.
- The indicator is tied to the launched Codex.app PID and exits automatically
  when that process exits.
- The indicator is click-through and available across Spaces/full-screen
  desktops on macOS.
- Operators can suppress it with `SLIMFERENCE_CODEX_DESKTOP_INDICATOR=0`.
- The scoped launcher also accepts
  `--env=SLIMFERENCE_CODEX_DESKTOP_INDICATOR=0` as the only safe
  `SLIMFERENCE_CODEX_DESKTOP_*` extra override.
- Focused tests cover route gating, env sanitisation, detached process launch,
  flag parsing, and scoped launcher integration.
- Install and technical docs describe the new signal and no longer promise a
  current renderer-native model/provider chip.

## Sub-Tasks

- [x] Re-check the current Codex Desktop app-server and renderer contract for a
  stable arbitrary badge/chip field.
- [x] Reject model metadata, model list, selected model, and service-tier
  mutation as route-indicator mechanisms.
- [x] Add a hidden `slimference desktop-indicator` helper.
- [x] Implement the macOS Cocoa overlay with click-through all-spaces behavior
  and PID-watch auto-exit.
- [x] Gate indicator startup to scoped Slimference Desktop launches only.
- [x] Add regression tests for startup gating, env scrubbing, flag parsing, and
  launcher wiring.
- [x] Update install, technical docs, and TODO index.
- [x] Run focused verification.

## Notes

- This is intentionally not an in-Codex renderer patch. Patching the Electron
  bundle would be visually closer to the old chip, but too fragile for the
  product default and likely to drift on every Codex Desktop update.
- This is intentionally not a model-list fallback. `model/list` remains
  byte-identical in the product path.
- The Darwin/Cocoa helper locks the original process main thread before main so
  AppKit can create the overlay window without crashing.
- The indicator proves scoped launch state visually. Token savings proof still
  comes from the app-server shim flight log, daemon decisions log, and WSS
  proof gates.

## Deviations

None.
