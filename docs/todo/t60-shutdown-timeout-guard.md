# T60 - Shutdown-Timeout Guard auf `wg.Wait()`

Status: todo
Priority: P2
Scope: `internal/proxy/proxy.go`, `internal/summarization/`, `internal/analytics/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`ProxyServer.Shutdown()` cancels `workerCtx` then calls `wg.Wait()`
with no timeout. If any goroutine ignores context cancellation (e.g.
MiniMax call in a tight retry loop without ctx-aware backoff, fsnotify
watcher blocked on syscall, fsync in analytics collector), the shutdown
hangs indefinitely. systemd / launchd SIGKILL kicks in after their own
timeout, but:

1. In-flight analytics events are lost, not flushed.
2. Users see "service fails to stop cleanly" in journalctl.
3. Tests that exercise shutdown become flaky.

The fix is a bounded `wg.Wait()` with a configurable timeout, followed
by a force-log of which goroutines are still alive, then return.

## Current State

- `Shutdown(ctx context.Context)` exists in `ProxyServer` with partial
  timeout handling on HTTP server but unbounded on worker pools.
- No way to see which goroutines are hanging.

## Target State

- `Shutdown(timeout time.Duration)` with hard cap (default 30 s).
- On timeout: log every known worker group's final state, dump
  goroutine profile via `runtime/pprof` to `$SLIMFERENCE_STATE_DIR/
  shutdown-hang-<ts>.pprof`, then return error.
- Exit code 6 when shutdown exceeds timeout (distinct from exit 0).

## Design

### Signature

```go
// Shutdown attempts a clean shutdown within timeout. Returns nil on
// clean stop, ErrShutdownTimeout on timeout (with goroutine dump written
// to state dir).
func (p *ProxyServer) Shutdown(timeout time.Duration) error
```

### Implementation

```go
func (p *ProxyServer) Shutdown(timeout time.Duration) error {
    p.workerCancel()
    p.httpServer.Shutdown(ctx)

    done := make(chan struct{})
    go func() { p.wg.Wait(); close(done) }()

    select {
    case <-done:
        return nil
    case <-time.After(timeout):
        dumpPath := filepath.Join(stateDir, fmt.Sprintf(
            "shutdown-hang-%s.pprof", time.Now().UTC().Format("20060102T150405")))
        if f, err := os.Create(dumpPath); err == nil {
            pprof.Lookup("goroutine").WriteTo(f, 1)
            f.Close()
        }
        slog.Error("shutdown_timeout",
            "timeout", timeout,
            "goroutines", runtime.NumGoroutine(),
            "pprof", dumpPath,
            "workers", p.workerStatus(),
        )
        return ErrShutdownTimeout
    }
}

var ErrShutdownTimeout = errors.New("shutdown timeout exceeded")
```

### Config

`[proxy]`:

| Field | Type | Default |
|-------|------|---------|
| `shutdown_timeout_seconds` | int | 30 |

ENV: `SLIMFERENCE_SHUTDOWN_TIMEOUT_SECONDS`.

### Worker status

Each worker pool exposes `Status() WorkerStatus`:

```go
type WorkerStatus struct {
    Name       string
    Running    int
    Queued     int
    Processed  int64
}
```

On timeout, log aggregated status.

### Exit codes

Integrated with T44 exit-code taxonomy:

| Code | Meaning |
|------|---------|
| 0 | clean shutdown |
| 6 | shutdown-timeout (goroutines still alive; dump written) |

## Implementation Plan

### WP1 - Timeout variant of Shutdown.

### WP2 - Goroutine dump on timeout.

### WP3 - Worker status snapshot.

### WP4 - Config + ENV.

### WP5 - Integration with headless (T44) + daemon.

### WP6 - Tests
- Fake long-running worker → shutdown returns `ErrShutdownTimeout`
  within timeout + 100 ms, dump file present.
- Clean shutdown within timeout returns nil.

---

## Subtasks

- [ ] Timeout-aware Shutdown signature.
- [ ] Goroutine pprof dump on timeout.
- [ ] Worker status snapshot helper.
- [ ] Config field + ENV.
- [ ] Exit code 6 wired in headless mode.
- [ ] Tests for both code paths.
- [ ] Docs: `docs/documentation.md` §12 Operability.

## Risks

- pprof dump on disk in a restricted environment (no write perms).
  Mitigation: fallback to stderr with `pprof.Lookup("goroutine").
  WriteTo(os.Stderr, 1)`.
- Exit code 6 may surprise systemd/launchd restart policy. Document
  that restart-on-failure is correct behaviour.

## Acceptance Criteria

- [ ] `Shutdown(30*time.Second)` returns within 30.1 s even with a
      hanging worker.
- [ ] pprof dump written on timeout.
- [ ] Exit 6 on timeout in headless mode.
- [ ] `go test -race ./internal/proxy/...` green.

## Out of Scope

- Trying to un-hang specific goroutines (signal them).
- Per-pool timeouts (uniform timeout is enough).

---

## Validation

```
go test -race -run Shutdown ./internal/proxy/...
./slimference --no-tui &
kill -TERM $!
# expect exit 0 in <1s
```
