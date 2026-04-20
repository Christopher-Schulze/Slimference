# T51 - Streaming Upload-Limit Integration-Test (> 32 MiB chunked)

Status: todo
Priority: P1
Scope: `internal/proxy/handler.go`, `tests/integration/`, `internal/proxy/handler_test.go`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`maxRequestBodySize = 32 MiB` is enforced inside `readBody` via
`io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))`. This works
for buffered clients but the behaviour for **Transfer-Encoding: chunked**
clients with a body that grows incrementally past 32 MiB is only covered
by unit tests that pre-fill the body - the streaming boundary is
untested.

Risks on the untested path:

- Client never sends `Content-Length`; server reads until limit; then
  must close cleanly without leaking the half-read connection.
- Upstream may have received part of the body already if proxy started
  forwarding before buffering - verify it does not (spec+.md says always
  buffer L1 prefix first).
- Memory spike: 32 MiB alloc is fine, but a bug could cause 2× via
  tee + copy.

Tool-result blocks from very large `Read` outputs (pre-T37) can easily
hit this limit.

## Current State

- Unit test `TestReadBody_oversizeRejected` uses a single `bytes.Buffer`
  pre-filled with > 32 MiB → hits `LimitReader` boundary → returns 413.
- No integration test uses `Transfer-Encoding: chunked` with incremental
  writes.
- No memory-ceiling test.

## Target State

- Integration test under `tests/integration/streaming_upload_test.go`:
  - Starts a Slimference proxy with a stub upstream.
  - Client sends chunked request that grows past 32 MiB via 1 MiB
    increments with `time.Sleep` between chunks.
  - Asserts:
    - Proxy returns 413 cleanly.
    - Connection is closed after response.
    - No bytes reached upstream.
    - Memory high-water stays ≤ 40 MiB (runtime.MemStats before / after).
- Unit test `TestReadBody_chunkedOversize` with a slow reader fake.
- Metric: total upload attempts exceeding limit in
  `/admin/status.uploads.rejected_oversize`.

## Design

### Test harness

Use `net/http/httptest` + manual `http.NewRequestWithContext` with a
custom body reader that implements `io.Reader` and returns 1 MiB at a
time. Set `Transfer-Encoding: chunked` by leaving Content-Length unset
and using `ContentLength: -1`.

### Memory measurement

```go
var before runtime.MemStats
runtime.ReadMemStats(&before)
// run test
var after runtime.MemStats
runtime.ReadMemStats(&after)
delta := after.Alloc - before.Alloc
```

Assert `delta < 40 * 1024 * 1024`.

### Config

New `[proxy]` field:

| Field | Type | Default |
|-------|------|---------|
| `max_request_body_bytes` | int64 | 33554432 (32 MiB) |

ENV: `SLIMFERENCE_MAX_REQUEST_BODY_BYTES`.

Today's hard-coded value becomes config-driven so large-context
deployments can raise to e.g. 64 MiB at their own risk.

### Admin surface

Add to `/admin/status`:

```json
"uploads": {
  "accepted_total": 1234,
  "rejected_oversize_total": 3,
  "largest_accepted_bytes": 30457600
}
```

### Handler flow verification

Add slog field `event=request_body_rejected reason=oversize
bytes_read=<n> limit=<m>` so ops can triage legit over-limit clients.

## Implementation Plan

### WP1 - Config
- Expose `max_request_body_bytes` in config + defaults + ENV.

### WP2 - Metric counters
- Add `uploadsAccepted`, `uploadsRejected`, `largestAccepted` atomic
  counters in proxy.

### WP3 - Unit test
- `TestReadBody_chunkedOversize` with a synthetic chunked reader.

### WP4 - Integration test
- `tests/integration/streaming_upload_test.go` with real net pipe.

### WP5 - Memory assertion
- Instrument test with `runtime.MemStats` pre/post.

### WP6 - Admin surface
- Extend `/admin/status` JSON.

### WP7 - Docs
- `docs/documentation.md` §12 Operability note.

---

## Subtasks

- [ ] Config field + ENV override.
- [ ] Atomic counters for uploads.
- [ ] Unit test with chunked fake reader.
- [ ] Integration test with real net pipe.
- [ ] Memory-ceiling assertion.
- [ ] `/admin/status` JSON extension.
- [ ] slog event on oversize reject.
- [ ] Docs update.

## Risks

- Flaky memory assertion under `-race`. Mitigation: run assertion only
  when `-race` absent (build tag `!race`), or loosen ceiling to 80 MiB
  under race.
- CI slowness from 32 MiB test. Mitigation: gate under build tag
  `integration` that normal `go test ./...` skips; CI runs with tag.

## Acceptance Criteria

- [ ] Unit + integration tests green.
- [ ] Memory assertion passes with 40 MiB ceiling (non-race).
- [ ] `/admin/status.uploads` fields present and update.
- [ ] `go test -race ./...` still green.

## Out of Scope

- Graceful partial-body handling (spec+ forbids it).
- Streaming compression during upload (not how Slimference works).

---

## Validation

```
go test -tags integration -run StreamingUpload ./tests/integration/...
go test -race ./internal/proxy/...
curl -s 127.0.0.1:8990/admin/status | jq .uploads
```
