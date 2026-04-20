# T63 - Tee-Recovery Exit-Code-Matrix in spec+.md dokumentieren

Status: todo
Priority: P2
Scope: `spec+.md`, `internal/filter/`, `docs/documentation.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Layer 0's Tee-Recovery path (`slimference filter -- <cmd>` saves a raw
copy of child output before filter mutation) has clear code but the
**exit-code contract** between child, filter, and Slimference is
documented only implicitly. Audit-relevant questions:

- What exit code does Slimference return when the child exits non-zero
  but filter succeeded?
- What if the filter itself crashes?
- What if the Tee copy fails (disk full)?
- What if user passes a command that emits to stderr only?

Without a written matrix, behaviour changes silently over time. This
violates AGENTS.md "Exit-Codes Layer 0" cross-cutting requirement.

## Current State

- Code correctly propagates child exit code on clean run.
- Filter failure currently falls back to passthrough; unclear from
  docs.
- Tee-fail behaviour unclear.
- `spec+.md` §Layer-0 mentions exit codes but no matrix.

## Target State

spec+.md §Layer-0 contains a complete exit-code table:

| Scenario | Child exit | Filter result | Tee result | Slimference exit | Behaviour |
|----------|-----------|---------------|------------|------------------|-----------|
| clean run | 0 | applied | ok | 0 | filtered output to stdout |
| child non-zero, filter ok | N (!= 0) | applied | ok | N | filtered output, preserve child exit |
| child ok, filter err | 0 | error | ok | 0 | raw Tee output to stdout + log warn |
| child ok, filter panic | 0 | panic | ok | 0 | raw Tee output to stdout + log error + pprof dump |
| child err, filter err | N | error | ok | N | raw Tee output to stdout + log warn, preserve child exit |
| child err, filter err, tee err | N | error | error | N | best-effort: raw to stdout from in-memory ring if fits, stderr "slimference: partial output"; preserve child exit |
| signal kill (SIGTERM from parent) | - | - | - | 130/143 | pass signal to child, propagate |
| child hang > filter_timeout | - | - | - | 124 | SIGKILL child, emit hint |

Slimference **never** swallows a non-zero child exit - that is a hard
invariant. Filter failures are degradation signals, not errors to
return.

## Design

### Code audit

Verify every code path matches the matrix. Discrepancies: fix code,
not matrix.

Concrete files to audit:
- `internal/filter/pipeline.go` - filter execution
- `internal/filter/tee.go` - Tee-copy path
- `cmd/slimference/main.go` - exit-code propagation after filter call

### Test matrix

Table-driven test in `internal/filter/pipeline_test.go` implementing
each row as a fixture:

```go
cases := []struct {
    name             string
    childExitCode    int
    filterInjectErr  error
    teeInjectErr     error
    expectedExit     int
    expectedStdout   string
    expectedLog      string
}{
    {"clean_run", 0, nil, nil, 0, "filtered", ""},
    {"child_err_filter_ok", 7, nil, nil, 7, "filtered", ""},
    {"filter_err", 0, errors.New("x"), nil, 0, "raw", "filter_failed"},
    // ... all rows from matrix
}
```

### Signal handling

- Parent SIGTERM → forward to child → wait for child → exit 143 (128+15).
- Parent SIGINT → forward → wait → exit 130 (128+2).

### Hang detection

`[filter] command_timeout_seconds = 0` (0 = no timeout). If > 0 and
child exceeds: SIGTERM → 5 s → SIGKILL → exit 124 with hint.

### Docs output

1. Add `### Exit Code Matrix` section to spec+.md §Layer-0.
2. Link from `docs/documentation.md` §4.
3. Add `slimference filter --help` text excerpt with the matrix
   compressed.

## Implementation Plan

### WP1 - Code audit against matrix.
### WP2 - Fix any discrepancies found.
### WP3 - Table-driven test covering every row.
### WP4 - Spec + docs update.
### WP5 - `filter --help` update.
### WP6 - Signal + timeout integration tests.

---

## Subtasks

- [ ] Audit filter pipeline + Tee + main.go exit-code path.
- [ ] Fix any matrix-mismatches.
- [ ] Implement `command_timeout_seconds` config.
- [ ] Table-driven tests for all scenarios.
- [ ] Signal-propagation integration test.
- [ ] Update spec+.md §Layer-0 with exit-code matrix.
- [ ] Update `docs/documentation.md` §4.
- [ ] Update `filter --help` text.

## Risks

- Current code may not match the matrix on obscure edge cases (tee-fail,
  filter-panic). Audit will surface; expect small bug-fixes.
- Timeout introduces new failure mode. Default = 0 (disabled) keeps
  legacy behaviour.

## Acceptance Criteria

- [ ] Spec+.md §Layer-0 contains the exit-code matrix.
- [ ] Every row covered by a named test.
- [ ] Timeout-based SIGKILL path tested.
- [ ] Signal propagation tested.
- [ ] `go test -race ./internal/filter/...` green.

## Out of Scope

- Exit-code matrix for hooks/posttool (separate surface).
- Windows signal semantics (macOS/Linux only).

---

## Validation

```
go test -race -run ExitCodeMatrix ./internal/filter/...
./slimference filter -- false ; echo $?    # expect 1
./slimference filter -- sleep 60           # with timeout=5, expect 124 after 5s
```
