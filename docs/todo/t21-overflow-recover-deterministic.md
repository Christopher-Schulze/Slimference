# T21 - Overflow-Recover without MiniMax in the Sync Path

Status: done
Priority: high
Scope: internal/proxy/handler.go `buildAggressiveCompressedBodyContext`

---

## Problem

When the upstream returns 400 with a context-length signature, the proxy
retries via `buildAggressiveCompressedBodyContext` (handler.go:515). That
function currently runs **synchronously** inside the user-facing request:

1. Rebuild a stricter compression config (window=2, summary ratio=10%).
2. Invoke `compression.NewDeterministicCompressor` - fine, pure CPU.
3. Invoke `summarization.NewLayer2(...)` + `l2.RunCompressionJobContext(...)` -
   this **calls MiniMax over the network synchronously** inside the hot path.

If MiniMax hangs or is rate-limited, the user request hangs with it. The
recover path was intended to be a guaranteed fast fallback, and a network-
dependent call contradicts that.

---

## MiniMax stays - just not in this sync path

The user asked whether MiniMax should be removed entirely. The answer is **no**:

- MiniMax/Layer 2 continues to run in the normal async queue driven by
  `ShouldTriggerCompression` (handler.go:322). That is exactly where it
  belongs: off the hot path, best-effort, retries allowed.
- The overflow **sync** recover path must become deterministic. It should
  rely only on:
  - Layer 1 with a minimal window (`SlidingWindow = 2`).
  - Any **already cached** L2 summary (read-only, never compute a new one).
  - Final fallback: original body.

The result is: MiniMax keeps its compression role during steady-state use;
the panic-button recover path is pure local CPU and cannot hang.

---

## Desired End State

`buildAggressiveCompressedBodyContext`:

1. Runs Layer 1 aggressively.
2. Consults `Layer2.Cache` for an existing, valid summary covering the current
   message range. If present, apply it.
3. Never calls `RunCompressionJobContext` synchronously.
4. Reconstructs body and returns - total path is local CPU only.

Separately: if no cached L2 summary exists and aggressive L1 still exceeds
context, fall back to original body (current behaviour, line 533-539).

---

## Work Packages

### WP1 - Drop sync MiniMax call

- Remove the `l2.RunCompressionJobContext(ctx, msgs)` line from
  `buildAggressiveCompressedBodyContext`.
- Replace with a read-only call: `l2.ApplyToMessages(msgs)` which already
  consumes only what is in the cache.

### WP2 - Trigger async L2 anyway

- After recover dispatch, enqueue a `CompressJob` onto `p.compressQueue` so
  next time the session has a fresh summary ready.
- Non-blocking send, like the existing pattern at handler.go:322-327.

### WP3 - Regression proof

- New test: upstream returns 400 context-length and MiniMax is wired to a
  hanging stub - the proxy must still respond within a bounded time by
  falling through to original body.
- Existing overflow tests stay green.

---

## Subtasks

- [x] Patch `buildAggressiveCompressedBodyContext` to drop the sync MiniMax call.
- [x] Ensure an async L2 job is enqueued on overflow for future requests.
- [x] Add hanging-stub test proving no sync dependency on MiniMax.
- [x] Update inline doc-comment and `spec+.md` §17.4 note.

## Acceptance Criteria

- Overflow recover latency is bounded by local CPU only.
- MiniMax remains functional in its normal async path.
- Coverage stays at 100 %, race-clean.
