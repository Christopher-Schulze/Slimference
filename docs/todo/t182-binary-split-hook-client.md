# TASK 182: Binary split — tiny slimference-hook client

Status: TODO (planning 2026-05-16; depends on t156)
Priority: P2 (latency + binary-size win)
Scope: new `cmd/slimference-hook/` (already planned by t156), build infra in `scripts/release/`

## Why

The full `slimference` binary is 17.7 MB stripped. Every hook event subprocess-launches this fat binary just to do <10 ms of work. A tiny `slimference-hook` binary (no TUI, no proxy, no sqlite, no tiktoken-go) would be ~3-4 MB and start in ~5 ms instead of ~30-80 ms.

Already part of t156 plan (hot-path daemon socket). This task is the realisation of the binary-split piece.

**Why:** Halves hook latency. User-facing snappiness. Plus smaller binary footprint for hook callers (Codex spawns it per tool call).
**How to apply:** Build a separate `cmd/slimference-hook/main.go` that only imports the hook-handling code, links a minimal command set, uses the Unix-socket daemon when available, falls back to subprocess invocation of the full binary when the socket is down.

## Target State

1. New `cmd/slimference-hook/main.go`, ≤200 LOC, minimal flag parsing.
2. Build releases both binaries: `slimference` (full) + `slimference-hook` (mini).
3. `internal/hooks/codex.go` and `internal/hooks/claude.go` generate hook scripts that prefer `slimference-hook` when it exists in PATH, fall back to `slimference <subcommand>`.
4. Size target: stripped `slimference-hook` ≤4 MB.
5. Latency target: cold-start ≤10 ms on macOS-arm64.

## Acceptance

- `du -h slimference-hook` shows ≤4 MB.
- `time slimference-hook --version` ≤15 ms.
- Hook scripts use `slimference-hook` when present.
- Full integration test: hooks run via the mini binary, end-to-end Codex flow unchanged.

## Sub-Tasks

- [ ] Audit which packages the hook entry points actually need (filter, hookproto, daemon-socket client, debug, config).
- [ ] Minimal main with subcommands: `rewrite`, `posttool`, `readhook`, `codexhook`.
- [ ] No imports of: `internal/tui`, `internal/proxy`, `internal/summarization`, `internal/tlsca`, `internal/caching`.
- [ ] Build scripts in `scripts/release/`.
- [ ] Hook-script template update.
- [ ] Tests: hook script picks the mini binary when present.

## Notes

- Depends on t156 (Unix-socket daemon protocol). The mini binary's hot path is a socket call; subprocess fallback handles socket-down cases.
- This is the realistic-floor for binary size given current Go runtime + sqlite needs.

## Deviations

(none yet)
