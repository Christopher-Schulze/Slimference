# TASK 81: Bypass granularity

Status: partial - duration-bounded landed; per-tool / per-route / next-request deferred
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

## Closure Notes (2026-04-30)

Landed:

- `Proxy.SetBypassFor(d time.Duration)` enables bypass with an
  auto-revert deadline. Non-positive duration is treated as "until
  cleared explicitly".
- `Proxy.BypassExpiresAt()` returns the deadline for inspection.
- `Proxy.BypassAutoRevertCount()` exposes the cumulative auto-revert
  counter for observability.
- `Proxy.Bypass()` lazily reverts when the deadline has passed; the
  CompareAndSwap path ensures concurrent observers see a consistent
  on/off state without an extra goroutine. `isProviderEnabled` and
  `isLayerEnabled` route through `Bypass()` so the auto-revert is
  visible everywhere bypass matters.
- `SetBypass(false)` also clears any pending deadline.
- Race-clean unit tests pin happy-path, infinite-duration, and clear
  semantics. `internal/proxy` 100% coverage.

Deferred:

- `--next-request` scope: needs a per-request "matched bypass" decrement
  with concurrency guards. Not landed.
- Per-tool / per-route bypass scopes: pattern matching on request tool
  name / URL path. Not landed.
- CLI / admin endpoint surface for the new duration parameter. The
  underlying API exists; the wrappers in `cmd/slimference/bypass_cmd.go`
  and `internal/proxy/admin.go` still take a bool only.
- TUI bypass overlay update.
