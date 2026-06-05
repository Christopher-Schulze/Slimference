# T278 - Hot-path allocation micro-sweep

## Status

Done.

## Source

External model-review follow-up after validating performance-related claims at
commit `f0f96ed`.

## Reality check

Several model claims were stale or overbroad:

- App and WSS manager pointers are already `atomic.Pointer`.
- Anthropic prompt-cache placement already supports multiple breakpoints and a
  hot/cold placement hint.
- SQLite filter tracking uses a one-shot local path; prior T84 closed broad WAL
  assumptions as a no-op.
- Layer-1 message-level fan-out already exists and T104b previously found
  sub-layer fan-out not worth the complexity without profiler evidence.

Some micro-optimization candidates remain worth benchmarking, but only after
measurement.

## Candidate areas

- Layer-2 redaction off-mode currently preserves a deep-copy invariant. If the
  caller can safely accept identity passthrough under `outbound_redaction=off`,
  this can reduce allocations. If the invariant is relied on, keep it.
- MinHash/dedup buffers may benefit from `sync.Pool`, but only if benchmarks
  show allocation pressure in real Layer-1 workloads.
- Proof/capture parsers can avoid repeated allocations when scanning large WSS
  frame files.
- Chunk-dedup/token accounting already has host-budget gates; any optimization
  must improve overhead without weakening fail-open behavior.

## Non-goals

- No unsafe or assembly path without benchmark proof and clear fallback.
- No complexity that makes model-facing safety harder to reason about.
- No change to model-facing bytes, archive IDs, recovery handles, or proof
  counters unless explicitly tested.

## Acceptance

- Add or reuse benchmarks that isolate each candidate.
- Implement only candidates with measurable improvement and no safety tradeoff.
- Benchmarks record before/after allocs and ns/op.
- Relevant package tests pass with `-race` where concurrency is touched.
- `go run ./scripts/ci` passes.

## Verification

- `go test ./internal/compression ./internal/summarization ./scripts/utils -bench=. -benchmem`
  or narrower targeted benchmarks, depending on touched code.
- Focused unit tests for changed invariants.
- Full CI after implementation.

## Notes

- This is a measured polish task. "Sounds faster" is not enough.
- Implemented measured Redaction-Off optimization. `Redactor.Redact` now
  returns the original message slice unchanged when
  `outbound_redaction=off`, because Layer 2 callers only read the returned
  slice. Redacting modes still copy before mutation.
  - Before: `BenchmarkRedactorOffModeLargeHistory` = 6977-7893 ns/op,
    39040 B/op, 81 allocs/op.
  - After: `BenchmarkRedactorOffModeLargeHistory` = 5.289-5.626 ns/op,
    0 B/op, 0 allocs/op.
- Implemented measured MinHash allocation reduction without changing
  model-facing bytes or near-dedup signatures. The signature path now hashes
  word-span shingles directly and uses inline FNV-1a instead of materialising
  shingle strings.
  - Before: `BenchmarkMinHashSignatureFromText` = 4.52-5.08 ms/op,
    ~184655 B/op, 4010 allocs/op.
  - After: `BenchmarkMinHashSignatureFromText` = 4.39-4.43 ms/op,
    ~32768 B/op, 1 alloc/op.
- Proof/capture parser pooling and deeper MinHash algorithm changes were not
  implemented in this pass because no profile in this task showed them as the
  next bottleneck. They should reopen only with pprof/bench evidence.
- Verification:
  - `go test ./internal/summarization -run 'Redactor|BuildRedactor' -count=1`
  - `go test ./internal/compression -run 'MinHash|NearDedup' -count=1`
  - `go test ./internal/summarization -run '^$' -bench '^BenchmarkRedactorOffModeLargeHistory$' -benchmem -count=5`
  - `go test ./internal/compression -run '^$' -bench '^BenchmarkMinHashSignatureFromText$' -benchmem -count=5`
