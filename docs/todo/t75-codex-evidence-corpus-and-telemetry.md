# T75 - Codex Evidence Corpus and Savings Telemetry

Status: todo
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
- Reuse `RequestSummary` JSONL where possible.
- Add a Codex-specific fixture metadata file:
  - Codex version
  - route type
  - hooks enabled
  - proxy layers enabled
  - redaction method

### WP2 - Capture fixtures
- Use local Codex with temp projects and non-sensitive prompts.
- Include:
  - clean run
  - failing test loop
  - repeated `rg` / `git diff`
  - large file read
  - tool output archived/expanded
  - bypass on/off scenario

### WP3 - Extend benchmark reporting
- `scripts/benchmarks session-report` should accept a directory.
- Add provider split and Codex-specific route split.
- Emit Markdown table for docs.
- Keep existing single-file behavior unchanged.

### WP4 - Add regression gates
- A small smoke fixture must run in CI.
- Large corpus can be local/manual if it is too slow or too sensitive.
- Thresholds should be conservative: fail only on obvious regression.

### WP5 - Update docs
- `docs/benchmarks.md`: append Codex corpus results.
- `docs/savings-assessment.md`: replace generic estimates with corpus-backed
  Codex numbers and caveats.
- `docs/integration.md`: include how to reproduce the benchmark after install.

## Acceptance Criteria

- [ ] At least 10 scrubbed Codex-session fixtures exist.
- [ ] Benchmark command can run the full corpus and emit Markdown.
- [ ] Report includes per-layer and per-provider savings.
- [ ] Codex corpus has at least one fixture proving Layer 0 hook savings.
- [ ] Codex corpus has at least one fixture proving proxy-side savings after T73.
- [ ] Docs no longer rely on the single 40.67% fixture for Codex claims.
- [ ] `go test ./scripts/benchmarks/...` and `bun test tests/ts` green.

## Out of Scope

- Public marketing claims.
- Uploading real private sessions.
- Automating live paid API calls in default CI.

## Validation

```
go run ./scripts/benchmarks session-report tests/fixtures/codex
go test ./scripts/benchmarks/...
bun test tests/ts
```
