# RTK Parity Summary

Date: 2026-06-05

This is the short closure companion to `docs/rtk-audit.md`.

## Imported into Slimference

- Trust-model port: `internal/filter/trust.go`
- Terraform filter coverage: `internal/filter/builtin_terraform.go`
- Python traceback coverage: `internal/filter/builtin_python.go`
- Current RTK TOML catalog parity: 59 RTK filter files, 59 Slimference bundled
  TOML files, filename diff empty.
- Safe RTK-inspired `wc` compaction: deterministic count/unit/path formatting
  with shorter-than-original and fail-open guards.
- Safe RTK-inspired `find`/`fd` path-list grouping: no command replacement,
  no result cap, preserves every path component and order, fail-open on
  ambiguous lines.
- Search output-shape hardening: `rg -0`, GNU `grep -Z`, `--null`,
  `--null-data`, and `--path-separator` full-pass before grouped search
  compaction.

## Already Covered Better in Slimference

- Layer-0 pipeline, hook install/verify, analytics gain tracking, and ANSI
  stripping all have first-class Go equivalents.
- Slimference also adds the proxy, active Layer 0/1/3/4 stack, TUI, daemon service,
  prompt-cache visibility, and operating modes, which RTK never had.
- Codex support is materially stronger in Slimference: current RTK Codex
  support is prompt-level awareness, while Slimference owns Codex hooks plus
  HTTP/WSS proxy mutation and Phase-F reducers.

## Explicitly Not Ported

- `discover/`
- `learn/`
- niche long-tail TOML filters with little hot-path value
- `openclaw/` as a separate companion tool
- RTK aggressive code-signature summaries as default product behavior; they
  remove implementation details and therefore violate Slimference's default
  drawdown bar unless exact recovery and live quality proof exist.
- RTK transparent rewrite prefixes and Claude built-in tool hooks as Codex
  product work; those are command-mutation or Claude-specific surfaces, while
  Slimference's Codex savings happen through Codex hooks, HTTP/WSS proxy
  mutation, and tool-output reducers.

## Outcome

- RTK has been reduced to an audit/reference exercise, not a live dependency.
- The valuable in-scope Codex ideas are already landed in Slimference.
- Non-ported RTK surfaces are closed product decisions, not pending hidden work.
- `research/rtk-ai/rtk/` remains an embedded read-only foreign reference per
  `AGENTS.md`; it is not a live dependency and must not be edited as part of
  Slimference work.
