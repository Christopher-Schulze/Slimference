# RTK Parity Summary

Date: 2026-04-19

This is the short closure companion to `docs/rtk-audit.md`.

## Imported into Slimference

- Trust-model port: `internal/filter/trust.go`
- Terraform filter coverage: `internal/filter/builtin_terraform.go`
- Python traceback coverage: `internal/filter/builtin_python.go`

## Already Covered Better in Slimference

- Layer-0 pipeline, hook install/verify, analytics gain tracking, and ANSI
  stripping all have first-class Go equivalents.
- Slimference also adds the proxy, active Layer 0/1/3/4 stack, TUI, daemon service,
  prompt-cache visibility, and operating modes, which RTK never had.

## Explicitly Not Ported

- `discover/`
- `learn/`
- niche long-tail TOML filters with little hot-path value
- `openclaw/` as a separate companion tool

## Outcome

- RTK has been reduced to an audit/reference exercise, not a live dependency.
- The valuable in-scope ideas are already landed in Slimference.
- The repo no longer needs the vendored RTK tree in HEAD.
