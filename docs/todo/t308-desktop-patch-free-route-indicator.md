# T308 Desktop patch-free route indicator

Status: Rejected / removed from product path.

## Why

The old in-composer provider chip is no longer a stable public surface in the
current Codex Desktop app-server contract. Mutating model names, model IDs,
selected model values, default service tiers, or service-tier metadata would
make the indicator fragile and would put the signal in the wrong product
contract. Users still need a visible confirmation that a Codex.app window was
started through Slimference instead of the normal direct path. The patch-free
overlay attempt did not meet that UX bar and was removed.

## Acceptance

- No patch-free overlay helper remains in the binary.
- Scoped Desktop launches do not start an overlay process.
- The Desktop app-server shim still keeps the historical `config/read`
  provider-chip injection for older Codex Desktop builds.
- Current Codex Desktop builds are documented honestly: the old chip may not
  render, and Slimference does not fake it through model metadata.
- Route truth remains `codex desktop status`, the app-server shim flight log,
  and daemon decision events.

## Sub-Tasks

- [x] Re-check the current Codex Desktop app-server and renderer contract for a
  stable arbitrary badge/chip field.
- [x] Reject model metadata, model list, selected model, and service-tier
  mutation as route-indicator mechanisms.
- [x] Add and test the patch-free overlay experiment.
- [x] Reject it after live UX review.
- [x] Remove the hidden helper, launcher wiring, env override, and tests.
- [x] Update install, technical docs, and TODO index to prevent re-promotion.
- [x] Run final verification after removal.

## Notes

- This is intentionally not an in-Codex renderer patch. Patching the Electron
  bundle would be visually closer to the old chip, but too fragile for the
  product default and likely to drift on every Codex Desktop update.
- This is intentionally not a model-list fallback. `model/list` remains
  byte-identical in the product path.
- The overlay was technically possible but poor product UX. It created a second
  surface instead of restoring the native in-composer chip and was removed.
- Token savings proof still comes from the app-server shim flight log, daemon
  decisions log, and WSS proof gates.

## Deviations

None.
