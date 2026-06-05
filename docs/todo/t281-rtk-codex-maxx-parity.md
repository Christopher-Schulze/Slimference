# TASK 281: RTK Codex maxx parity and safe deltas

## Why

The current product target is Codex-only max savings without product drawdown.
RTK remains a useful reference for command-output filtering, hook ergonomics,
and edge-case parsers, but Claude-style `updatedInput` and aggressive
signature summaries cannot be assumed safe for Codex. The project needs a
current RTK/Codex reality check plus any deterministic, fail-open deltas that
increase savings without deleting model-needed context.

## Acceptance

- Current RTK upstream and Codex hook capability docs are reflected in
  Slimference docs.
- Every accepted RTK-inspired delta is deterministic, shorter-than-original
  guarded, and fail-open on unknown input.
- Aggressive code-signature summaries remain rejected as a default product path
  unless exact recovery and live model-quality proof exist.
- Focused tests cover every new reducer/hook-parser branch.
- `go test ./...` and `go run ./scripts/ci` pass.

## Sub-Tasks

- [x] Refresh RTK/Codex reality-check docs against RTK `0a630fe` and Codex
  CLI `0.137.0`.
- [x] Add safe `wc` Layer-0 compaction and rewrite coverage.
- [x] Add safe `find`/`fd` path-list output grouping without command-semantic
  replacement.
- [x] Harden Codex hook migration for current lifecycle event names.
- [x] Re-run focused package tests.
- [x] Re-run full local CI gate.

## Notes

RTK's Claude hook rewrites Bash `PreToolUse` with `updatedInput`; Codex support
in RTK is prompt awareness only. Slimference's Codex path stays stronger:
hook signals plus proxy/WSS mutation, with `updatedInput` treated as unproven
for transparent rewrite.

RTK `rtk read -l aggressive` keeps imports/signatures and drops implementation
bodies. That can save many tokens, but as a default/first-read path it is a
model-quality drawdown risk for Codex because GPT-5.x may need the omitted body
later. Keep it rejected as default; only a future archive-backed, explicit
scan/repeated-read mode could reconsider a signature capsule.

RTK's `find` command executes a new gitignore-aware filesystem walk, which is
not safe to transparently substitute for a user command. Slimference takes only
the safe half: when actual `find`/`fd` output is already a large newline path
list, group repeated directory prefixes while preserving every path component
and original order.

Tests: `go test ./internal/filter`, `go test ./internal/hooks`,
`go test ./...`, and `go run ./scripts/ci` all passed on 2026-06-05.

## Deviations

- None.
