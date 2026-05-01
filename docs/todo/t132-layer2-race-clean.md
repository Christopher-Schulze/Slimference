# TASK 132: Layer 2 race-clean blocker

Status: CODE-COMPLETE / FULL-RACE-SUITE GREEN (implemented 2026-05-02)
Priority: P0 for T129
Scope: `internal/summarization/minimax.go`, `internal/proxy/admin.go`, Layer 2 telemetry tests.
Driver: the 2026-05-01 repository audit found `go test -race ./...` failing in Layer 2 telemetry. T129 wants Layer 2 default-on. That is not acceptable while a race exists in the summarization path.

---

## Problem

Race detector output shows concurrent access to `examplePromptCounters`:

- read path: `summarization.ExamplePromptCounts()` called from `proxy.adminStatusSnapshot()`
- write path: `summarization.buildSystemPrompt()` increments `examplePromptCounters[lang]++` during MiniMax summarization

That means admin/status telemetry can race with active Layer 2 summarization. Normal CI can pass while race CI fails. Since T129 increases Layer 2 exposure by making it default-on for fresh installs, this race must be fixed first.

## Target state

Layer 2 telemetry counters are concurrency-safe and the full race suite passes.

## Implementation plan

### WP1 - Counter safety

- [x] Replace the plain package-level `map[string]int64` with a mutex-protected map.
- [x] `ExamplePromptCount`, `ExamplePromptCounts`, `ResetExamplePromptCounts`, and `buildSystemPrompt` use the same synchronization discipline.
- [x] Public helper behaviour remains unchanged.

### WP2 - Regression test

- [x] Added `TestExamplePromptCounts_ConcurrentAccess`, concurrently calling `buildSystemPrompt`, `ExamplePromptCounts`, and `ExamplePromptCount`.
- [x] Focused race validation passes for the counter path.

### WP3 - T129 gate

- [x] T129 may now proceed past the specific `examplePromptCounters` blocker.
- [x] Full `go test -race ./...` passed after the Phase R batch.

## Acceptance criteria

- [x] `examplePromptCounters` has no unsynchronised read/write path.
- [x] Concurrent counter test added.
- [x] Focused race commands pass:
  - `go test -race ./internal/summarization -run 'TestExamplePromptCounts_ConcurrentAccess|TestBuildSystemPrompt|TestExamplePrompt' -count=1`
  - `go test -race ./internal/proxy -run 'TestServeHTTP_OutputReduce|TestAdminHandlers_JSONResponses|TestHandleCompressibleRequest_MidExchangeEnabled' -count=1`
- [x] Full `go test -race ./...` passes.
- [x] T129 detail file references this task as completed before default-on.

## Out of scope

- Changing MiniMax prompt content.
- Changing Layer 2 summarization policy.
- Flipping Layer 2 default-on; that remains T129.

## Validation

```
go test -race ./internal/summarization/... ./internal/proxy/...
go test -race ./...
```
