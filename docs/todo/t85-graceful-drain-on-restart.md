# TASK 85: Graceful drain on launchd restart

Status: todo
Priority: P1
Scope: `internal/daemon/`, `internal/proxy/handler.go`, `cmd/slimference/`
Driver: launchd KeepAlive (T68) restarts the daemon on crash, but in-flight streaming connections are killed mid-flight. Restarting the proxy while the model is mid-stream cuts the user's response. T60 added a shutdown-timeout guard but there is no drain-then-exit pattern.

---

## Problem

Today a clean exit closes the listener and signals goroutines, but a streaming response that has buffered upstream tokens does not get a chance to flush before the process dies. From the user's perspective: "Slimference rebooted -> Claude session looks frozen -> next message restarts the conversation".

## Target State

On SIGTERM / SIGHUP / `slimference service restart`:

1. Stop accepting *new* connections.
2. Allow up to `[daemon] drain_timeout` (default 30s, capped at 120s) for in-flight requests to finish.
3. Cancel any remaining requests with a graceful upstream cancel and a clean SSE event-stream close.
4. Exit.

When launchd restarts the daemon, old in-flight requests have a chance to flush their stream before the process dies; new traffic flows to the new instance after socket-rebind.

## Implementation Plan

### WP1 - Drain controller
- `internal/daemon/drain.go` with an in-flight WaitGroup and a context that fires on SIGTERM.
- Listener wraps `Accept` in a check on the drain context.

### WP2 - Streaming handler integration
- `streamingRelay` (T02) accepts the drain context as cancel parent so it can flush a "stream interrupted" SSE event before closing.

### WP3 - launchd plist update
- ThrottleInterval already in T68; add SIGTERM grace window so launchd waits for our drain.

### WP4 - Tests
- Integration test: start daemon, open streaming request, send SIGTERM, assert client gets a clean stream-end event within drain budget.
- Race test: simultaneous shutdown + new accepts must reject new and finish old.

## Acceptance Criteria

- [ ] In-flight streaming requests finish or close cleanly within `drain_timeout` on SIGTERM.
- [ ] No orphan goroutines after exit.
- [ ] `slimference service restart` exits 0 within drain budget under load.
- [ ] Race tests green.
- [ ] Counter `drain_canceled_requests_total` exposed in `/admin/status`.

## Out of Scope

- Hot-reload of binary without exit (separate, much harder).
- Connection migration to a sibling process.

## Validation

```
go test ./internal/daemon/... ./internal/proxy/...
go test -tags=integration ./tests/integration/drain_test.go
```
