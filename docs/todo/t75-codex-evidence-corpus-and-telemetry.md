# T75 - Codex Evidence Corpus and Savings Telemetry

Status: partial - live corpus blocked
Priority: P1
Scope: `tests/fixtures/`, `scripts/benchmarks/`, `internal/analytics/`, `internal/proxy/`, `docs/benchmarks.md`, `docs/savings-assessment.md`
Driver: Existing savings proof is real but fixture-scale; Codex-specific claims need real Codex-session evidence.

---

## Problem

The checked-in `sample_session.jsonl` proves 40.67% savings on a small mixed
fixture. That is useful, but not enough to claim Codex CLI works perfectly or
to tune Codex-specific compression. Codex has different request paths, auth,
payload shape, and hook surfaces. Savings and safety must be measured on real
Codex-like sessions.

## Target State

A small, scrubbed Codex evidence corpus exists and can be re-run locally:

- 10-20 Codex sessions covering coding, search, test failure, repeated build,
  large file read, and long debugging loop.
- Every fixture is redacted and safe to commit.
- Benchmark output splits savings by:
  - Layer 0 hooks
  - Layer 1 deterministic compression
  - Layer 2 summary
  - Layer 3 cache
  - read cache / tool archive where applicable
- Docs state conservative, normal, and best-case Codex savings from corpus data,
  not intuition.

## Implementation Plan

### WP1 - Define corpus format
- [x] Reuse `RequestSummary` JSONL where possible.
- [x] Add a Codex-specific fixture metadata file:
  - Codex version
  - route type
  - hooks enabled
  - proxy layers enabled
  - redaction method
  - regression gate baseline
  - File: `tests/fixtures/codex/codex-metadata.json` with schema_version=1.

### WP2 - Capture fixtures
- [!] Use local Codex with temp projects and non-sensitive prompts.
- Include:
  - clean run
  - failing test loop
  - repeated `rg` / `git diff`
  - large file read
  - tool output archived/expanded
  - bypass on/off scenario

Blocked: live Codex capture is intentionally not allowed in the current pass.

### WP3 - Extend benchmark reporting
- [x] `scripts/benchmarks session-report` should accept a directory.
- [x] Add provider split and Codex-specific route split.
- [x] Emit Markdown table for docs.
- [x] Keep existing single-file behavior unchanged.

### WP4 - Add regression gates
- [x] A small smoke fixture must run in CI/package tests.
- [x] Large corpus can be local/manual if it is too slow or too sensitive.
- [x] Thresholds should be conservative: fail only on obvious regression.
  Implemented as `scripts/benchmarks codex-smoke-gate <dir>`: reads
  `regression_gate` from `codex-metadata.json` and asserts min request count,
  min savings ratio, per-layer min saved tokens, and minimum provider/route
  counts. Wired into `scripts/ci` as the final step so the smoke corpus
  cannot regress without failing local CI.

### WP5 - Update docs
- [x] `docs/benchmarks.md`: append Codex corpus smoke results, document the
  metadata schema + regression gate workflow.
- [x] `docs/savings-assessment.md`: stop framing the synthetic 57.14% smoke
  result as a Codex savings claim; explicitly mark it as a regression
  backstop only and route real claims to a future live corpus.
- [x] `docs/integration.md`: include how to reproduce the benchmark and run
  the smoke gate after install.

## Acceptance Criteria

- [ ] At least 10 scrubbed Codex-session fixtures exist.
- [x] Benchmark command can run a Codex corpus directory and emit Markdown.
- [x] Report includes per-layer and per-provider savings.
- [x] Codex smoke corpus has at least one fixture representing Layer 0 hook savings.
- [x] Codex smoke corpus has at least one fixture representing proxy-side savings after T73.
- [x] Docs no longer rely on the single 40.67% fixture for real Codex claims.
  (Codex section in `docs/savings-assessment.md` now explicitly flags the
  smoke numbers as a regression backstop, not a savings claim.)
- [x] `go test ./scripts/benchmarks/...` and `bun test tests/ts` green.
- [x] Corpus metadata schema declared and committed
  (`tests/fixtures/codex/codex-metadata.json`, schema_version=1).
- [x] Regression gate is enforced in `scripts/ci`
  (`codex-smoke-gate` step).

## Out of Scope

- Public marketing claims.
- Uploading real private sessions.
- Automating live paid API calls in default CI.

## Validation

```
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex
go test ./scripts/benchmarks/...
go run ./scripts/ci
bun test tests/ts
```

## Closure Notes

- Offline/reporting and gating parts are implemented:
  - `session-report` accepts a file or a directory.
  - Directory mode recursively reads `*.jsonl` and renders the corpus
    metadata block in front of the numbers when
    `codex-metadata.json` is present in the directory.
  - Reports include Layer 0/1/2/3, prompt-cache read/create tokens, provider
    counts, and optional `codex_route` counts.
  - `tests/fixtures/codex/session-smoke.jsonl` keeps the Codex reporting path
    executable in CI.
  - `tests/fixtures/codex/codex-metadata.json` declares the corpus
    provenance and the `regression_gate` baseline.
  - `scripts/benchmarks codex-smoke-gate` enforces the baseline and is wired
    as the final step of `scripts/ci`.
- Remaining blocker is data capture, not core reporting code: a real 10-20
  session Codex corpus requires explicit permission to run/live-wire Codex.
