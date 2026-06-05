# T282 Search output-shape hardening

## Why

RTK's command catalog treats search output shape as part of the safety boundary.
Slimference already refuses JSON, heading, count, file-list, context, and custom
field-separator search output before grouped `file:line:content` compaction.
The 2026-06-05 RTK/Codex maxx audit found one remaining zero-drawdown guard
gap: NUL-delimited search modes and custom path separators can change the
meaning of separators. Those shapes must full-pass instead of entering the
colon parser.

## Acceptance

- `rg -0`, GNU `grep -Z`, `--null`, and `--null-data` search outputs do not
  enter match-line grouping.
- `rg --path-separator ...` search outputs do not enter match-line grouping.
- The change is a pure fail-open guard: no lossy product behavior is added, and
  normal newline-delimited `file:line:content` grouping remains unchanged.
- Focused filter tests pass, then the repo release gate passes.
- RTK parity docs record the guard as accepted hardening, not a new lossy
  savings feature.

## Sub-Tasks

- [x] Inspect current search grouping and RTK/Codex evidence.
- [x] Add NUL/custom path-separator output-shape guards.
- [x] Add regression tests for the refused shapes.
- [x] Flush RTK docs and TODO status.
- [x] Run focused tests and final CI.

## Notes

- This task intentionally does not change command execution, command rewrite,
  or model-facing context. It only prevents unsafe grouping for output formats
  whose delimiters no longer match the default parser.
- Verification: `go test ./internal/filter`; `go test ./...`;
  `go run ./scripts/ci`.

## Deviations

- None.
