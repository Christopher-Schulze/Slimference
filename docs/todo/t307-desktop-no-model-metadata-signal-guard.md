# T307 Desktop no-model-metadata signal guard

Status: Done.

## Why

The current local Codex Desktop bundle (`26.602.40724`, inspected on
2026-06-07) no longer exposes the old text provider chip in the composer model
selector from process-local provider config. The previous T306 fallback
prefixed model display names with `Slimference `, which made the route visible
but put the signal in the wrong UI element. A follow-up UI-slot attempt also
touched model metadata. The product rule is now hard: route badges must never
be faked through `model/list`, model IDs, model labels, selected model values,
default service tiers, or service-tier metadata.

## Acceptance

- Current Codex Desktop bundle inspection identifies that the visible slot left
  of the model label is not an arbitrary provider text chip in `26.602.40724`.
- Scoped `model/list` responses pass through byte-identically.
- Scoped `thread/start` may rewrite only default/null `modelProvider` to
  `slimference-codex`; real service tiers such as `priority` remain untouched.
- Malformed or unrelated JSON-RPC frames still pass through fail-open.
- Focused Go tests cover `model/list` byte-identical passthrough and real-tier
  preservation.
- Documentation no longer claims the current Desktop signal is a model-name
  prefix or model-metadata replacement.

## Sub-Tasks

- [x] Extract and inspect the current Codex Desktop app bundle under `/tmp`.
- [x] Verify the composer model trigger in `26.602.40724` does not expose
  arbitrary provider text through app-server response data.
- [x] Remove model display-name prefixing and all model-metadata badge fallback
  logic.
- [x] Add regression coverage that model-list responses remain byte-identical.
- [x] Update focused tests.
- [x] Update install and technical docs.
- [x] Run formatting and verification gates.

## Notes

- The current Codex Desktop bundle does not expose a process-local app-server
  API for an arbitrary `Slimference` text chip in the old location. A literal
  text chip would require patching Codex Desktop's frontend bundle, which is not
  a product default for Slimference.
- `model_list_seen` is diagnostic only. There is intentionally no model-list
  rewrite event.
- T308 replaces the removed model-metadata fallback with a Slimference-owned
  patch-free macOS route indicator tied to the scoped Codex.app PID.

## Deviations

None.
