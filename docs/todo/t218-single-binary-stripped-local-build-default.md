# TASK 218: Single-binary stripped local build default

Status: DONE (2026-05-17)
Priority: P1 release hygiene
Scope: local build tooling and documentation only; no product split

## Why

Slimference should stay one self-contained binary. The default local build
command should nevertheless match release hygiene by stripping debug sections
and local paths. The current unstripped developer build is about 26 MiB; the
same binary built with `-trimpath -ldflags "-s -w"` is about 18 MiB on
darwin/arm64, with no runtime feature loss.

## Acceptance

- One product binary remains the target: `cmd/slimference`.
- No daemon/TUI/proxy split is introduced.
- Local build documentation points to a canonical helper.
- The helper always uses `-trimpath -ldflags "-s -w"`.
- Optional `--install` copies the optimized binary to
  `~/.local/bin/slimference`.
- Release script remains compatible with the same stripped-build policy.

## Sub-Tasks

- [x] Add `scripts/build` Go helper for local stripped builds.
- [x] Add tests covering default flags, version injection, dry-run, and
  argument rejection.
- [x] Update `scripts/README.md`, `README.md`, `docs/documentation.md`, and
  `spec+.md` to point at the optimized single-binary build path.
- [x] Build and sync the optimized binary locally.

## Verification

- `go test ./scripts/build -count=1`
- `go run ./scripts/build --install`
- `ls -lh ./slimference ~/.local/bin/slimference` shows the stripped
  darwin/arm64 binary around 18 MiB.

## Notes

Do not split Slimference just to reduce file size. The large unstripped build
is mostly Go runtime metadata, DWARF/debug sections, SQLite/TLS/TUI/compression
dependencies, and symbol tables. The stripped build is the correct default.
