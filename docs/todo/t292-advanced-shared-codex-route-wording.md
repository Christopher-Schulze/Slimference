# TASK T292: Advanced shared Codex route wording

## Why

The default product contract is simple: normal Codex CLI/App launches stay
direct unless the user starts them through Slimference. The old user-facing
wording made the healthy default look like a failure by saying "Codex route
disabled" or "Codex Mode disabled".

The persistent provider-block path still exists for advanced/dev/compat use,
but product surfaces must label it as the advanced shared route, not as the
normal Codex state.

## Acceptance

- CLI and TUI disabled/default status says normal Codex is direct.
- Persistent provider-block actions are labelled as the advanced shared Codex
  route.
- Docs and script reports use the same distinction.
- Tests cover the new wording.
- Routing behavior remains unchanged: no persistent route is enabled by this
  task.

## Sub-Tasks

- [x] Update CLI/TUI user-facing wording.
- [x] Update docs and script report wording.
- [x] Update tests.
- [x] Run targeted tests and full CI.

## Notes

- Internal package names, JSON field names, marker names, and historical task
  records may keep `codexroute` / `codex route` where that is technical state,
  API compatibility, or immutable history.
- Verified wording scan excludes the old user-facing `Codex Mode`, `Route is
  disabled`, `route_enabled=`, and `optional shared` surfaces outside technical
  or historical contexts.
- `slimference status` now treats normal direct Codex, product listener `:8990`,
  and inactive hosts redirects as OK states instead of marking the default
  product setup unhealthy just because global lab / advanced shared route is
  off.
- Targeted tests passed: `go test ./cmd/slimference ./internal/tui
  ./scripts/benchmarks ./scripts/utils`.
- Full tests passed: `go test ./...`.
- Final gate passed: `go run ./scripts/ci` with all 8 steps green and 96.5%
  aggregate coverage.

## Deviations

- None.
