# TASK 81: Bypass granularity

Status: todo
Priority: P1
Scope: `internal/proxy/proxy.go`, `cmd/slimference/`, `internal/tui/`, `internal/admin/` (admin endpoint)
Driver: Today bypass is binary. Real debugging needs "skip compression for the next request" or "for the next 5 minutes" or "for this tool only". Otherwise the operator has to flip global bypass and remember to flip it back.

---

## Problem

`slimference bypass on` and `bypass off` are global and atomic (T67). That is the right primitive but the wrong granularity for daily debugging. When something goes wrong the operator wants to bisect: "is it Layer 1?", "is it this specific tool?", "did the last config change break it?". The current API forces a cold global toggle.

## Target State

Three new bypass modes layered on top of the existing global flag:

1. `slimference bypass on --duration=5m` — auto-reverts after the duration.
2. `slimference bypass on --next-request` — reverts after the very next request through the proxy.
3. `slimference bypass on --tool=<key>` and `--route=<glob>` — per-tool / per-route bypass that leaves the rest of the pipeline running.

Per-layer bypass already exists (`bypass on --layer=L1`) and stays. New modes compose with it.

## Implementation Plan

### WP1 - Bypass state model
- Replace the single atomic bool with a struct `BypassPolicy` that captures global, scoped (route/tool), and time-bounded entries.
- Lock-free read on the hot path; copy-on-write update.

### WP2 - Auto-revert timer
- A single goroutine drains a min-heap of expirations.
- Counter `bypass_auto_revert_total` for telemetry.

### WP3 - Per-request scope
- After serving the next request that matched the scope, decrement the policy.

### WP4 - CLI / admin / TUI parity
- `slimference bypass on --duration=5m`, `--next-request`, `--tool=<glob>`, `--route=<glob>`.
- `/admin/bypass` POST accepts the same fields.
- TUI bypass overlay shows active scopes.

## Acceptance Criteria

- [ ] Duration-bounded bypass reverts at expiry without process restart.
- [ ] `--next-request` reverts after the matching request.
- [ ] Per-tool and per-route bypass leave non-matching traffic compressed.
- [ ] All scopes round-trip via admin endpoint and TUI.
- [ ] Telemetry counters exposed in `/admin/status.bypass`.
- [ ] Coverage 100%; race tests green; integration tests cover the auto-revert.

## Out of Scope

- Persistence across daemon restarts (deliberate: bypass is operator-driven, restart resets).
- User-facing scheduling like "bypass during business hours" (out of scope, would be a routine).

## Validation

```
slimference bypass on --duration=5m
slimference bypass on --next-request
slimference bypass on --tool=Read
slimference bypass status
```
