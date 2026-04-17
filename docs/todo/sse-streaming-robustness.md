# SSE Streaming Robustness

**Status:** done  
**Priority:** high  
**File:** `internal/proxy/streaming.go`

## Problem

Three real gaps in the SSE streaming relay identified during spec parity audit (2026-04-13):

### 1. No Context Cancellation Propagation
`streamingRelay()` and `passthrough()` accept no `context.Context`. When a client disconnects mid-stream, the upstream request continues consuming resources until the upstream closes the connection. The `http.ResponseWriter` already carries the request context via `r.Context()`, but the scanner loop has no mechanism to detect client-abort.

**Fix:** Check `r.Context().Done()` inside the scan loop and return early.

### 2. Flush Errors Silently Ignored
Line ~42: `flusher.Flush()` is called without checking for write errors. If the client has disconnected, `Flush()` may fail, but the loop continues writing and flushing to a dead connection.

**Fix:** After each write to `w`, check if the context is cancelled rather than trying to detect flush errors (HTTP `Flush()` returns no error in Go's stdlib). The context-cancelled check from fix #1 covers this.

### 3. Scanner Buffer Overflow Not Reported
`scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)` — lines over 1 MB are silently dropped by `bufio.Scanner`. `scanner.Err()` will return `bufio.ErrTooLong`, but this is only checked after the scan loop exits, and is currently only logged at DEBUG level without the actual token count being finalised.

**Fix:** After scan loop, if `scanner.Err() == bufio.ErrTooLong`, log a WARN (not DEBUG) with the request ID so the operator knows.

## Implementation Plan

### streaming.go changes

1. In `streamingRelay(w http.ResponseWriter, resp *http.Response, provider types.Provider)`:
   - Add `ctx context.Context` parameter
   - Inside scan loop, after each `fmt.Fprintf`/`Flush`, check `ctx.Err() != nil` and break early

2. Callers of `streamingRelay` must pass `r.Context()`.

3. Change `scanner.Err()` log from `slog.Debug` to `slog.Warn`.

### Tests to add (streaming_test.go)

- `TestStreamingRelay_contextCancelled`: relay with a context that's cancelled after first SSE event is forwarded - relay must stop without blocking
- `TestStreamingRelay_scannerOverflow`: relay with a fake upstream that sends a line >1MB - verify WARN is logged, relay exits cleanly

## Files Affected
- `internal/proxy/streaming.go` (implementation)
- `internal/proxy/streaming_test.go` (new tests)
- `internal/proxy/handler.go` (update `streamingRelay` call site)

## Completion Criteria
- [x] `streamingRelay` accepts `ctx context.Context` and exits on cancel
- [x] Scanner overflow logs WARN
- [x] `TestStreamingRelay_contextCancelled` green
- [x] `TestStreamingRelay_scannerOverflow` green
- [x] `go test ./internal/proxy/...` passes
