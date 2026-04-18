# T19 - Dead-Code Cleanup in the Proxy Hot Path

Status: open
Priority: low
Scope: internal/proxy/handler.go

---

## Problem

Two harmless but noisy dead-code spots inside the hot path:

1. `handler.go:331-332` assigns `_ = layer1Savings; _ = layer2Savings` even
   though both variables are read 15 lines later inside the debug recorder and
   analytics event. The blank assigns are leftover from an earlier iteration.
2. `handler.go:594-596` defines `buildAggressiveCompressedBody` (no context
   argument) as a thin wrapper around `buildAggressiveCompressedBodyContext`.
   `grep` confirms it is only called from two test files - production paths
   always use the context variant.

Neither hurts correctness, but both degrade hot-path readability and make the
file's intent slightly misleading.

---

## Desired End State

- No blank assignments to variables that are actually used later.
- Production code contains no wrapper that exists only for tests.
- Tests use `buildAggressiveCompressedBodyContext(ctx, stash)` with
  `context.Background()` where appropriate.

---

## Work Packages

### WP1 - Remove blank noise

- Delete `_ = layer1Savings` and `_ = layer2Savings` at `handler.go:331-332`.
- Verify `go vet` and `go test ./...` stay green.

### WP2 - Drop the no-ctx wrapper

- Delete `buildAggressiveCompressedBody` (handler.go:594-596).
- Update `internal/proxy/handler_test.go:53` and `:145` to call
  `buildAggressiveCompressedBodyContext(context.Background(), ...)`.
- Keep the context-variant function as the single production path.

---

## Subtasks

- [ ] Remove blank `_ =` assignments.
- [ ] Delete `buildAggressiveCompressedBody`.
- [ ] Port the two test call-sites to the context variant.
- [ ] `go vet ./...` and `go test -race -count=1 ./...` green.

## Acceptance Criteria

- Coverage remains 100 %.
- Hot path is shorter by 3-5 lines with no behaviour change.
