# TASK T294: Mass-market scoped launch polish

## Why

Slimference's release UX should make the safe default obvious: installing and
running the daemon must not imply that normal Codex CLI/App launches are routed
through Slimference. Users should see one-shot/TUI-launched Slimference sessions
as the normal product path, while the marker-owned advanced shared route remains
available but clearly secondary.

## Acceptance

- Top-level help does not promote `slimference enable` as a first step.
- `slimference status` explains that normal Codex remains direct and that
  Slimference mode is launched explicitly.
- Install docs TL;DR and surface governance separate default scoped launch from
  advanced shared route.
- Tests cover the public help wording.
- No routing behavior changes.

## Sub-Tasks

- [x] Update public help/status wording.
- [x] Update install docs.
- [x] Update tests.
- [x] Run focused tests and CI.

## Notes

- This is UX polish only. The current route contract remains: daemon can run in
  the background, but Codex traffic is direct unless launched through
  Slimference or the optional advanced shared route is enabled.
- Top-level help now keeps `slimference enable` out of the first-run path and
  labels it as optional/dev advanced shared route.
- `slimference status` now states that normal Codex CLI/App stays direct unless
  launched through Slimference.
- Verification:
  - `go test ./cmd/slimference ./docs`
  - `go run ./scripts/ci`
  - `go run ./scripts/build --install`
  - `/Users/christopher/.local/bin/slimference status`
  - `/Users/christopher/.local/bin/slimference help`

## Deviations

- None.
