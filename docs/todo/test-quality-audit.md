# Test Quality Audit (AGENTS.md §5)

**Status:** done  
**Priority:** low  
**Scope:** `cmd/`, `internal/`

## Task

Verify all test files comply with AGENTS.md §5 requirements:
- 100% statement/branch coverage on cmd/ and internal/
- Hard edge cases and error scenarios covered (not just happy paths)
- Table-driven tests where sensible
- `t.Parallel()` wherever safe
- Deterministic (no timing-dependent tests)
- Meaningful error messages

## Known Compliant Packages
All packages were at ~100% coverage at the time of audit. The following were explicitly verified:
- `internal/filter/` - 100% coverage
- `internal/hooks/` - 100% coverage  
- `internal/compression/` - 100% coverage
- `internal/debug/` - 100% coverage

## Items to Check
- [ ] `internal/proxy/streaming_test.go` - new tests added for robustness (covered by sse-streaming-robustness task)
- [ ] `internal/caching/response_cache_test.go` - LRU tests added; verify parallel markers
- [ ] `internal/tui/` - TUI tests do not use t.Parallel() for BubbleTea model tests (acceptable - BubbleTea is stateful)
- [ ] `scripts/ci/main.go` coverage gate runs cleanly

## Completion Criteria
- [ ] `go run ./scripts/coverage -- -min=100` passes on full codebase
- [ ] No test uses `time.Sleep` for synchronization (use channels or sync primitives)
- [ ] All new tests added in this session have `t.Parallel()` where applicable
