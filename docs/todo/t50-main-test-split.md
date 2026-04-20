# T50 - `cmd/slimference/main_test.go` Split nach Subcommand-Domäne

Status: todo
Priority: P1
Scope: `cmd/slimference/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`cmd/slimference/main_test.go` is ~5,800 LOC and mixes tests for every
subcommand: filter, hook, rewrite, posttool, readhook, expand, gain,
debug, service, doctor, version, plus TUI bootstrap, plus flag parsing.

Pain points:

1. Every change to any subcommand forces re-reading a 6k-LOC file to
   locate the relevant test block.
2. Test names collide by luck of position; table-driven helpers are
   copy-pasted across sections because nobody wants to edit 6k lines.
3. IDE navigation is slow.
4. Git blame is useless on the file.
5. Parallel test runs are limited because helpers share package-level
   state with tight coupling to position-in-file.

This is a code-health task: no behaviour changes, strictly a refactor
that lowers future per-TASK friction.

## Current State

- Single `main_test.go` (5,844 LOC).
- Helpers like `makeTempDir`, `fakeExec`, `captureStdout` duplicated or
  scoped by convention.
- All subcommands tested in one file.

## Target State

Split into subcommand-scoped test files:

```
cmd/slimference/
  main.go
  main_test.go           - only TUI bootstrap + empty-args + --help + --version
  filter_test.go         - `slimference filter ...`
  hook_test.go           - `slimference hook ...`
  rewrite_test.go        - `slimference rewrite ...`
  posttool_test.go       - `slimference posttool ...`
  readhook_test.go       - `slimference readhook ...`
  expand_test.go         - `slimference expand ...`
  gain_test.go           - `slimference gain ...`
  debug_test.go          - `slimference debug ...`
  service_test.go        - `slimference service ...`
  doctor_test.go         - `slimference doctor`
  version_test.go        - `slimference version|--version|-V`
  helpers_test.go        - shared test helpers (makeTempDir, fakeExec,
                           captureStdout, withEnv, withConfig)
```

## Design

### Move rules

- Preserve every existing test by name. No assertion changes.
- Extract shared helpers into `helpers_test.go` as the single source.
- Package stays `package main`; test helpers remain unexported.

### Helper inventory (pre-work)

Audit `main_test.go` for duplicated helpers. Typical candidates:

- `makeTempDir(t *testing.T) string`
- `captureStdout(t *testing.T, fn func()) string`
- `captureStderr(t *testing.T, fn func()) string`
- `withEnv(t *testing.T, k, v string)`
- `withConfig(t *testing.T, toml string) (path string, cleanup func())`
- `fakeExec(t *testing.T, args []string, stdout, stderr string, exit int)`

Consolidate into `helpers_test.go`.

### Parallelism enablement

After split, mark tests `t.Parallel()` where safe (no env mutation, no
shared fs). Target: halve wall-clock for
`go test ./cmd/slimference/...`.

### Coverage preservation

- Before: capture baseline `go test -cover ./cmd/slimference/...`.
- After: compare coverage byte-for-byte via
  `go test -coverprofile=before.out` vs `after.out`, ensure identical
  set of covered lines.

## Implementation Plan

### WP1 - Baseline
- Run `go test -coverprofile=pre.out ./cmd/slimference/...`.
- Save `pre.out` as reference.

### WP2 - Helper extraction
- Identify shared helpers; move to `helpers_test.go`.
- Ensure helpers compile standalone.

### WP3 - Per-subcommand file moves
- For each subcommand, cut the test block and paste into
  `<subcmd>_test.go`.
- Run tests after each move to catch accidental drops.

### WP4 - Parallelism
- Add `t.Parallel()` where state-safe.

### WP5 - Coverage verify
- Run `go test -coverprofile=post.out`.
- Diff: `go tool cover -html=pre.out` vs `post.out`.
- Must match 100 % coverage; no regressions.

### WP6 - Doc touch
- Update any doc that references line numbers in old `main_test.go`
  (grep first).

---

## Subtasks

- [ ] Capture baseline coverage profile.
- [ ] Inventory + extract shared helpers.
- [ ] Split into 12 subcommand test files.
- [ ] Add `t.Parallel()` where safe.
- [ ] Verify 100 % coverage preserved.
- [ ] Update any doc references to line numbers.
- [ ] CI green on main branch merge.

## Risks

- Test-order dependency: some tests may currently rely on
  package-init ordering in the single file. Pre-flight: run with
  `-shuffle=on` before the split to surface any order coupling.
- Helper name collisions when consolidating: prefix with `test`
  (e.g. `testMakeTempDir`).

## Acceptance Criteria

- [ ] 12+ test files under `cmd/slimference/`.
- [ ] No shared helper duplicated more than once.
- [ ] `go test -shuffle=on -count=3 ./cmd/slimference/...` green.
- [ ] Coverage identical (line set) pre/post.
- [ ] Wall-clock improvement ≥ 30 % (bonus).

## Out of Scope

- Rewriting tests themselves (behaviour-preserving refactor only).
- Moving out of `package main`.

---

## Validation

```
go test -coverprofile=pre.out -count=1 ./cmd/slimference/...
# do the split
go test -coverprofile=post.out -count=1 -shuffle=on ./cmd/slimference/...
diff <(sort pre.out) <(sort post.out)
```
