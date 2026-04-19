# T34 - Benchmark Report: Live-Sessions Runner + Evidence Document

Status: done
Priority: high
Scope: scripts/benchmarks, docs/benchmarks.md, internal/debug

Implementation note: the harness and checked-in report are complete. The next
step is feeding a larger real-session corpus, not more benchmark plumbing.

---

## Problem

Spec+.md claims 2-3x savings. Current proof is anecdotal or inferred from
unit benchmarks. There is no end-to-end report with measured per-layer
contribution, per-session breakdown, or rolling production numbers.

Without that report, claims are theoretical and audit closure is fragile.

---

## Desired End State

A reproducible benchmark harness and a checked-in, dated evidence document:

- `scripts/benchmarks/live-sessions.go` (or a new `scripts/utils/bench-live.ts`
  Bun script if that is cleaner) replays a corpus of real recorded sessions
  through the proxy and records per-layer savings.
- `docs/benchmarks.md` presents the output: for N=100 sessions,
  L0-contribution, L1-contribution per sub-layer, L2-contribution, L3 hit
  rate, prompt-cache hit rate (via T23), total savings, distribution.
- Reproducible: the corpus source is described, seed fixed, re-run command
  documented.

---

## Work Packages

### WP1 - Session corpus

- Curate >= 100 recorded sessions covering: short Q&A, heavy code edits,
  long refactors, tool-heavy sessions (grep/build/test spam).
- Store under `tests/fixtures/sessions/` as JSONL if license allows, or as
  synthetic-but-realistic generators otherwise.

### WP2 - Replay harness

- Go or Bun script that:
  - Spins up the proxy in-process with default config.
  - Replays each session as HTTP requests.
  - Collects per-request analytics + debug summary.
  - Aggregates into per-layer and per-session stats.

### WP3 - Report generator

- Produces a Markdown table + simple ASCII charts.
- Emits a `docs/benchmarks.md` file with: date, proxy version, corpus
  description, per-layer savings, totals, histogram.

### WP4 - CI integration (optional)

- Nightly or weekly run via `scripts/ci` produces an artefact; regressions
  exceeding a tolerance fail the job.

### WP5 - Paired A/B against "no proxy"

- Key row: "same corpus forwarded verbatim" vs "through Slimference".
- Demonstrates net savings as observed by the upstream API.

---

## Subtasks

- [x] Assemble session corpus.
- [x] Build replay harness.
- [x] Build report generator + check in `docs/benchmarks.md`.
- [x] Document reproduction command in the report.
- [x] Optional: wire into CI with tolerance-based regression gate.

## Acceptance Criteria

- `docs/benchmarks.md` contains a dated table with concrete per-layer
  savings matching reality, not theory.
- The report is reproducible with a single command.
- Any future regression surfaces as a diff in the report.
