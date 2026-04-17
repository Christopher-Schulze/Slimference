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
- [x] `internal/proxy/streaming_test.go` - robustness coverage landed via dedicated streaming tests
- [x] `internal/caching/response_cache_test.go` - cache and invalidation coverage expanded with extra tests
- [x] `internal/tui/` - TUI tests remain intentionally selective about `t.Parallel()` because BubbleTea state is shared
- [x] `scripts/ci/main.go` coverage gate runs cleanly

## Completion Criteria
- [x] `go run ./scripts/coverage -min=100` passes on full codebase
- [x] No production test relies on `time.Sleep` for synchronization when a deterministic synchronization primitive is available
- [x] All new tests added in this session use `t.Parallel()` where applicable
