# TASK 156: Hook hot-path via Unix-socket daemon (sub-millisecond)

Status: TODO (planning 2026-05-15)
Priority: P0
Scope: `internal/daemon/`, `internal/hooks/`, `cmd/slimference/`, new `cmd/slimference-hook/`, `internal/filter/`, `docs/documentation.md`

## Why

Every Codex and Claude hook event today forks and execs the full `slimference` binary (~26 MB). Cold start measured 30-80 ms per call. `spec+.md` mandates <5 ms hot path. The Codex+Claude lifecycle fires dozens of hooks per session (PreToolUse, PostToolUse, Read, UserPromptSubmit, PermissionRequest, Stop). End-to-end latency adds up and is the only structural reason RTK feels snappier than us. We have a running daemon already (`internal/daemon/`) and the proxy keeps a long-lived process; routing hook RPC over a Unix socket to that same process collapses cold start to socket latency. Stay in Go, no Rust.

**Why:** Hook hot path is the user-visible latency budget. Every saved millisecond compounds across a 30-minute coding session. RTK's <10 ms is the bar we must beat with Go-native engineering.
**How to apply:** Every hook event (`rewrite`, `posttool`, `readhook`, `codexhook`, lifecycle) must have a socket fast path. Fallback to subprocess only when socket is unreachable.

## Target State

1. `slimference daemon` opens `~/.slimference/run/hook.sock` (Unix domain socket, 0600) at startup.
2. Tiny client binary `slimference-hook` (single static binary, <1 MB, only flag parsing + socket client + JSON I/O) installed alongside `slimference`. All hook scripts call this binary instead of the fat `slimference` for the hook path.
3. Wire protocol: newline-delimited JSON framing; one request, one response, optional log lines on stderr. Idle connections kept alive briefly (HTTP/1.1-style) to amortize TLS-style handshakes across consecutive hooks within one tool call.
4. Existing subcommands (`rewrite`, `posttool`, `readhook codex`, `codexhook <event>`, `checkpoint`, `expand`) gain a `--via-socket` fast path; manual CLI invocation keeps working as today.
5. Hook bash scripts (`internal/hooks/codex.go` `codex*HookScript()` and `internal/hooks/claude.go` equivalents) emit calls to `slimference-hook` instead of `slimference`. Fallback line: if `slimference-hook` exit is 127 or 124 (timeout), fall back to direct `slimference` invocation to preserve the contract.
6. Zero-alloc hot path: `sync.Pool` for `*json.Decoder`/`*json.Encoder`, `[]byte` buffers; all package-level regexes pre-compiled; no per-call `regexp.MustCompile`. PGO build with `-pgo=auto` using captured hook-trace profile.
7. Hot-path measurement: `slimference bench hooks` reports p50/p95/p99 latency over N synthetic events; CI gate `<2 ms p50, <5 ms p99` on macOS-arm64.
8. Watchdog: if socket connect fails 3x in a row, hook script falls back to subprocess and logs `socket-fallback=1` for analytics.

## Acceptance

- Cold daemon + first hook event p50 < 5 ms on macOS-arm64; steady-state p50 < 1 ms.
- All existing hook tests still pass when forced through socket path.
- `slimference-hook` binary <= 1 MB stripped (`-trimpath -ldflags="-s -w"`); does not import `internal/proxy`, `internal/compression`, `internal/summarization`, `internal/tui`, `internal/tlsca`.
- Subprocess fallback still works when daemon is down; integration test covers the down state.
- PGO build pipeline documented in `docs/documentation.md` and reproducible with `make pgo`.
- 100% Go coverage on the new socket-server and `slimference-hook` packages.

## Sub-Tasks

- [ ] Define wire schema in `internal/daemon/hookproto/` (request/response types, error envelope, version field).
- [ ] Server: `internal/daemon/hookserver.go` exposing the same handlers wrapped today by `cmd/slimference` (`handleRewriteCmd`, `handlePostToolCmd`, `handleReadHookCmd`, `handleCodexHookCmd`).
- [ ] Client: new `cmd/slimference-hook/main.go`, minimal cobra-free flag parsing, single static binary.
- [ ] Build: extend `scripts/build/` (new) to produce `slimference` + `slimference-hook` in one go.
- [ ] Patch hook script templates in `internal/hooks/codex.go` + `internal/hooks/claude.go` to call `slimference-hook` with `--via-socket` and fall back to `slimference` on non-zero socket failure code.
- [ ] PGO infrastructure: `slimference daemon --record-pgo=/tmp/cpu.prof` for capture; `make pgo` rebuilds with the captured profile.
- [ ] Benchmark suite `internal/daemon/hookserver_bench_test.go` (table-driven, all hook event types).
- [ ] CI gate: `scripts/benchmarks/hook_gate.go` rejects regressions > 10% vs. recorded baseline in `scripts/benchmarks/baselines/hooks.json`.
- [ ] Docs: `docs/documentation.md` "Hook hot path" section with architecture diagram + fallback behavior + PGO instructions.

## Notes

- Keep the existing direct-subprocess code paths intact; they remain the public CLI surface and the test substrate.
- Socket auth: `SO_PEERCRED`/`unix.Ucred` to verify the calling UID matches the daemon UID. Reject otherwise.
- Windows: defer; on Windows we can fall back to subprocess until a named-pipe server is added (separate task).
- `slimference-hook` SHOULD NOT depend on cgo to keep static linking trivial.

## Deviations

(none yet)
