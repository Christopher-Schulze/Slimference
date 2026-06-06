# TASK T295: TUI mass-market wording cleanup

## Why

The normal TUI should be the simple product surface. Technical routing terms
such as Phase-F, app-server shim, transport flags, and MITM should not leak into
primary launch/status text. Advanced lab state may remain visible, but it should
use product-safe wording.

## Acceptance

- Launch Center descriptions avoid `Phase-F`, `app-server shim`, and
  `transport=...`.
- Setup command hints do not promote `--transport=wss` as a normal command.
- Left-panel global lab tile does not show `GLOBAL LAB ARMED` when the lab is
  off and does not use `MITM` wording.
- Advanced route status avoids `Phase-F` wording.
- TUI tests and full CI pass.

## Sub-Tasks

- [x] Clean TUI launch/status wording.
- [x] Fix global lab tile wording.
- [x] Update tests.
- [x] Run focused tests and CI.

## Notes

- UX-only change. No route behavior changes.
- Removed normal TUI surface leaks for `Phase-F`, `app-server shim`,
  `transport=wss`, `transport=auto`, `MITM ARMED`, and `GLOBAL LAB ARMED`.
- Fixed the left-panel lab tile so the off state says `GLOBAL LAB OFF` instead
  of incorrectly saying armed.
- Verification:
  - `go test ./internal/tui`
  - `go test ./cmd/slimference ./internal/tui ./docs`
  - `go run ./scripts/ci`
  - `go run ./scripts/build --install`

## Deviations

- None.
