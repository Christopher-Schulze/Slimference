# T25 - Layer 0 Filter Expansion: Python Traceback, npm/pnpm Install, Terraform Plan

Status: done
Priority: medium
Scope: internal/filter, internal/filter/testdata, spec+.md §4

---

## Problem

Three high-frequency tool outputs are not compressed today:

1. **Python tracebacks** (pytest, runtime crashes, mypy in traceback mode).
   They are long, repetitive, and the meaningful part is the last frame plus
   the exception message.
2. **npm / pnpm / yarn install output** (progress bars stripped, but the
   "added N packages in Ts" line drowns inside hundreds of "reify" lines).
3. **Terraform plan output** ("Plan: X to add, Y to change, Z to destroy"
   summary at the bottom of hundreds of resource diff lines).

All three are common in interactive sessions and all three compress
aggressively with zero information loss when done correctly.

---

## Desired End State

Three new built-in filters under `internal/filter/`:

- `builtin_python_traceback.go` + test + fixtures.
- `builtin_npm_install.go` (covers npm/pnpm/yarn/bun add|install).
- `builtin_terraform.go` (plan, apply).

Each follows the existing `TryCompact*` shape: returns `([]byte, true)` when a
shorter output was produced, `(nil, false)` otherwise. Safety invariant stays:
**only emit if strictly shorter**.

Each filter registered in `dispatch.go` with sensible command matchers.

---

## Work Packages

### WP1 - Python traceback

- Detect `Traceback (most recent call last):` anchor.
- Preserve final frame (`File ".../x.py", line N, in fn`) and exception line.
- Collapse intermediate frames unless they sit in user code (heuristic: path
  does not start with site-packages, .venv, /usr/lib).
- Test fixtures: plain traceback, chained exceptions, pytest collapsed
  traceback, mypy in tb mode.

### WP2 - npm / pnpm / yarn / bun install

- Detect install commands via command matcher.
- Extract the final summary: "added N, removed M, updated K, audited J".
- Preserve any `ERR!` / `WARN` lines that carry real content (not just
  "deprecation noise").
- Drop `reify`, progress ticks, spinner frames.

### WP3 - Terraform plan / apply

- Detect `terraform` binary.
- Keep: summary line, any line with `~`, `+`, `-` prefix that refers to a
  resource header (e.g. `+ resource "aws_s3_bucket"`). Drop per-attribute
  diff unless under a small threshold.
- Apply: keep the "Apply complete!" summary and any errors.

### WP4 - Integration

- Register each filter in `dispatch.go` following the existing priority rule
  (built-ins before TOML DSL).
- Ensure each respects `passthrough_max_chars` fallback.
- Add at least 2 fixtures per filter under `internal/filter/testdata/`.

### WP5 - Docs and tracking

- `spec+.md` §4 filter list gets three new entries (F25, F26, F27).
- `docs/documentation.md` built-in filter table updated.
- `slimference gain --by-command` should now attribute savings to these.

---

## Subtasks

- [x] Implement + test Python traceback filter.
- [x] Implement + test npm/pnpm/yarn/bun install filter.
- [x] Implement + test Terraform plan/apply filter.
- [x] Register filters in dispatch and wire testdata.
- [x] Update spec and documentation filter lists.

## Acceptance Criteria

- Each filter compresses its canonical fixture by at least 70 %.
- Each filter passes the "only emit if shorter" guard.
- 100 % coverage on new files, race-clean.
- No regression on existing filters or dispatch.
