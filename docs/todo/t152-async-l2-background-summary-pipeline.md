# TASK 152: Async L2 background summary pipeline

Status: DONE
Priority: P0
Parent: T144
Scope: `internal/summarization/`, `internal/proxy/handler.go`, `internal/sessions/`, `internal/quality/`, `internal/config/`, `cmd/slimference/layer2_cmd.go`

## Why

MiniMax can save a lot on long context, but synchronous summarization in the hot path creates latency, third-party exposure, and quality risk. The low-drawdown version is background-only: prepare summaries after heavy turns, apply only ready/hash-valid summaries, and never make the active request wait.

## Target State

Layer 2 becomes a background optimizer:

1. Score summary candidates after each request.
2. Queue background summary jobs only when projected future savings beat provider cost and latency.
3. Redact and deterministically pre-compress before external summarization.
4. Store summaries session-keyed and content-hash invalidated.
5. Apply summaries only when ready before the next request.
6. Preserve anchors verbatim.
7. Fail open to original context on any timeout, provider failure, stale hash, or quality failure.

## Implementation Plan

### WP1 - Candidate scorer

- [x] Compute old-prefix tokens, repeated tool-output ratio, active-edit proximity, existing coverage, and expected future-turn value.
- [x] Require positive projected net saving before enqueue.

### WP2 - Background queue

- [x] Add bounded worker queue with cancellation and stale-content checks.
- [x] Never block `ServeHTTP` waiting for MiniMax.

### WP3 - Pre-summarization compaction

- [x] Run redaction first.
- [x] Run safe deterministic compaction over summary input.
- [x] Keep anchor windows verbatim.

### WP4 - Apply path

- [x] Apply only cached, hash-matching, validated summaries.
- [x] Record stale-job/hash-miss skip telemetry.

### WP5 - Tests

- [x] Summary job enqueues after tool-heavy prefix.
- [x] Request proceeds without waiting.
- [x] Stale summary is rejected.
- [x] Provider error preserves original context.

## Acceptance

- [x] L2 never blocks active request latency.
- [x] Summary input is redacted and deterministically compacted.
- [x] Summary cache is session-keyed and hash-invalidated.
- [x] Anchor loss fails closed.
- [x] `go test ./...` passes.

## Implementation Notes

- `ScoreBackgroundCandidateSession` centralizes L2 background ROI decisions: provider configured, enough compressible prefix, no active edit/error anchor in the live window, positive projected saving, and insufficient existing cache coverage.
- `CompressJob` now carries an input-prefix hash. The proxy records the newest candidate hash per session before enqueue, and workers drop stale jobs before setting the compressing flag or calling MiniMax.
- `ApplyToMessagesSession` reads only summaries whose covered prefix still matches the live message hash. Hash mismatches fail open to the original messages and increment cache telemetry.
- Admin status exposes `layer2.cache_stats`, including `hash_mismatches`, `candidate_sets`, and `stale_job_skips`.

## Verification

- `go test ./internal/summarization ./internal/proxy -count=1`
- `go test ./...`
- `git diff --check`

## Non-Goals

- No global lowering of `min_tokens_for_layer2`.
- No synchronous MiniMax call from hooks.
- No default-on early trigger without T146/T149 proof.
