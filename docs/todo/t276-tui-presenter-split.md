# T276 - TUI presenter split

## Status

Open.

## Source

External model-review follow-up after validating `internal/tui/model.go` and
`internal/tui/views.go` at commit `f0f96ed`.

## Evidence

The TUI is already tested and much smaller than some review claims implied, but
state mutation, polling, and string rendering still live close together in the
Bubble Tea model and view files. Product-surface work in T271 added more
savings and safety fields, so the next UI growth should be guarded by a clearer
presenter boundary.

## Why

The default TUI should show product signals only: active route/fallback,
billable input saved, output-wire saved, provider-cache read/create,
tool-prune/output-reduce status, read/search/repeated/chunk hit families,
safety attention, and host attention. Debug counters and parser internals should
stay in debug/reporting surfaces. A presenter split makes this enforceable and
testable.

## Scope

- Introduce pure presenter structs/functions for product status panels.
- Keep Bubble Tea update logic focused on input, polling, and model state.
- Keep render functions side-effect free.
- Add tests for presenter output from representative product state snapshots.
- Ensure debug-only counters do not leak into the default product view.

## Non-goals

- No visual redesign.
- No new TUI dependency.
- No new debug pane unless required for product clarity.
- Do not change `/admin/state` semantics unless the presenter exposes a missing
  already-existing product field.

## Acceptance

- Product presenter functions are unit-tested without starting Bubble Tea.
- Existing TUI tests stay green.
- Tests assert that core product signals are shown and debug-only internals are
  not shown in the default product panel.
- `go test ./internal/tui ./internal/control -count=1` passes.
- `go run ./scripts/ci` passes.

## Verification

- Unit tests over presenter structs.
- Snapshot-like string assertions for the product panel.
- Full CI.

## Notes

- This is UX maintainability and product clarity. It is not a token-savings
  mechanism by itself.
