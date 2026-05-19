# TASK 229: Codex hook hotpath socket

Status: PLANNED
Priority: P1 after T243 fallback matrix and T244 lifecycle hardening; can run
before Desktop proof if hook latency is the next visible UX cost
Scope: Codex hooks only; Claude Code remains parked/off

## Why

Hooks are useful as a signal layer: Read/Bash/PostTool/session events can feed
read cache, tool-output compression, metrics, and safety decisions. They are
not a replacement for scoped provider routing because Codex does not currently
honor `PreToolUse.updatedInput` as a transparent rewrite surface.

The current hook path can fork the full Slimference binary. That is functional
but not elegant: cold-start latency accumulates across many hook events. The
daemon already exists; hooks should talk to it over a local Unix socket and fall
back to the binary path only when the daemon is unavailable.

This is a UX/latency/stability task, not a transport replacement. It should make
Slimference feel invisible by removing hook process churn, but model
conversation traffic still belongs to the scoped provider/WSS route.

## Target State

- Codex hooks remain installed only by Slimference Codex install/product mode.
- Hook scripts are tiny stable shims.
- Shim -> sidecar/client -> daemon Unix socket.
- Daemon performs the real readhook/posttool/codexhook work.
- If daemon socket is unavailable, hook fails open or uses the existing
  subprocess fallback depending on event safety.
- No Claude Code hooks are installed by default.

## Acceptance

- Hook hot path p95 is below 5 ms for daemon-reachable local events excluding
  actual file IO and compression work.
- Hook cold-start/fork overhead is measured before and after implementation so
  the claimed win is evidence-backed, not assumed.
- `SessionStart`, `PreToolUse Read`, `PostToolUse`, `PermissionRequest`, and
  `Stop` all have socket request/response contracts.
- Hook response format remains exactly what current Codex expects.
- Unknown hook payloads fail open and are logged.
- Socket unavailability does not block Codex CLI/App usage.
- Tests cover daemon reachable, daemon missing, malformed payload, timeout,
  and subprocess fallback.

## Sub-Tasks

- [ ] Inventory existing Codex hook event payloads and response contracts.
- [ ] Define a compact daemon hook RPC schema under `internal/daemon/hookproto`
  or extend the existing one if suitable.
- [ ] Add socket server handlers in the daemon for Codex hook operations.
- [ ] Add a hook client path used by the installed hook shims.
- [ ] Keep current CLI subcommands working for manual/debug use.
- [ ] Add timeouts and event-specific fail-open behavior.
- [ ] Add status telemetry:
  hook_socket_available, hook_socket_latency_p95, fallback_count,
  malformed_count.
- [ ] Add tests for every event branch and fallback path.
- [ ] Benchmark hook p50/p95 before/after.
- [ ] Add release-gate smoke proving socket failure cannot prevent Codex CLI
  from continuing normally.

## Benefits

- Faster hook events and less process churn.
- Better RTK-like feel for Codex without pretending Codex supports transparent
  command rewrite.
- More reliable metrics because the daemon owns session state.

## Drawdowns and Guards

- Adds a local RPC path. Guard: small schema, strict timeouts, fail-open.
- Hook layer must not become a third traffic surface. Guard: hooks feed signal
  and local output compression only; model traffic still goes through scoped
  provider route.
- Do not start this before T243/T244 are stable unless profiling shows hooks
  are the active bottleneck. A faster hook path is valuable, but a broken
  transport ladder or daemon lifecycle would hurt UX more.
