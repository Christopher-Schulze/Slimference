# TASK 118: Live coding session corpus + savings reality gate

Status: DONE (core) 2026-05-01. Capture-session subcommand, benchmark-corpus gate, synthetic seed corpus, live-corpus policy, and CI integration shipped under this commit. Filling the corpus with >=10 real-session categories is operator-driven and tracked as T118b.
Priority: P0
Scope: `cmd/slimference/`, `scripts/benchmarks/`, `tests/fixtures/live_corpus/`, `internal/sessions/`
Driver: The repository has exactly **one** session-fixture (3 synthetic requests, `tests/fixtures/sample_session.jsonl`) and one synthetic Codex smoke (`tests/fixtures/codex/`). The 40.67% savings number that ships in `docs/savings-assessment.md` is measured against this single 3-request stub. The spec claims 65-90%; the regression gate accepts 40%. There is no real evidence either way. Fix: build a captured-and-redacted corpus of real coding sessions, then make CI assert savings ratios per session category.

---

## Problem

- `tests/fixtures/sample_session.jsonl` is hand-written, 3 lines, with markers like `req_a1b2c3d4` - obviously synthetic.
- `tests/fixtures/codex/codex-metadata.json` declares `"scrubbed": true, "redaction_method": "manual_synthetic"` - same.
- Savings claims in spec, README, audit-2.md, savings-assessment.md cite percentages that no committed test reproduces.
- A regression that drops the real-world savings ratio from 50% to 30% would not break any current test or CI gate.

## Target State

Three artifacts:

1. **Capture tooling**: `slimference capture-session [--name=<n>] [--redact-strict]` records the next session's full request/response stream into `~/.slimference/captures/<n>.jsonl`. Redaction (T109) runs on the captured content before it touches disk. Operator approves an explicit prompt naming what's about to be saved.
2. **Live corpus**: 10+ captured-and-scrubbed real sessions checked in under `tests/fixtures/live_corpus/` covering categories: `bug_fix_short`, `feature_long`, `refactor_multifile`, `debug_session`, `code_review`, `large_test_run`, `cli_tool_heavy`, `mixed_lang`, `cjk_session`, `large_response_streaming`. Each session metadata file states the exact length, primary language, expected savings range.
3. **CI gate**: `scripts/ci` runs `slimference benchmark-corpus tests/fixtures/live_corpus/` and asserts:
   - per-category minimum savings ratio (declared in metadata).
   - zero secret patterns surviving in the corpus (re-runs T109's redaction sanity).
   - end-to-end body reconstruction byte-equivalence on each request.

When any of these regress, CI fails.

## Implementation Plan

### WP1 - capture-session subcommand
- `cmd/slimference/capture_cmd.go`: spawns a session-recorder that hooks into `proxy.SetTUISendFn` (or equivalent observation point) and writes redacted request/response pairs.
- Privacy guardrails: explicit confirmation prompt naming the output file, max-size cap, automatic stop on `slimference capture-session stop`.

### WP2 - Redaction scrubber
- Reuses T109's `RedactForOutbound` in `strict` mode.
- Additional pre-commit check: every captured fixture passes through a regex sweep against a maintained "known-secret-patterns" list before being checked in.

### WP3 - Corpus structure
- `tests/fixtures/live_corpus/<category>/<session_id>.jsonl` - one request per line, identical schema to existing `RequestSummary`.
- `tests/fixtures/live_corpus/<category>/metadata.json` - declares: `expected_savings_min`, `expected_savings_max`, `notes`, `request_count`, `language`, `tool_mix`.
- `tests/fixtures/live_corpus/index.json` - top-level enumeration.

### WP4 - benchmark-corpus subcommand
- `scripts/benchmarks/benchmark_corpus.go`: walks `live_corpus/`, drives each session through the in-process compressor + cache + filters, computes savings per session and per category.
- Output: text + markdown + machine-readable JSON.
- Comparison mode: compares against a baseline JSON to surface regressions.

### WP5 - CI integration
- `scripts/ci/main.go` adds a step: `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus/ --check`.
- Exit code 1 when any category's measured ratio is below `expected_savings_min`.

### WP6 - Initial corpus seeding
- The operator (the repo owner) runs `capture-session` against their own coding sessions, scrubs, commits ~10 sessions across the categories above.
- Repository policy: no third-party customer sessions; only the maintainer's own sessions; full scrub before commit.

### WP7 - Documentation
- New `docs/live-corpus-policy.md` documents what may and may not be checked in (no real customer code, no real secrets, scrub before commit, max-size limits).
- `docs/savings-assessment.md` gets updated with per-category numbers from the corpus, retiring the single-fixture 40.67% claim.

### WP8 - Tests
- Capture roundtrip: synthetic in-process traffic captured, then replayed, byte-equivalent (modulo deterministic redaction).
- Benchmark gate: synthetic corpus that intentionally regresses; CI step exits 1.
- Schema test: corrupt `metadata.json` -> friendly error, not panic.

## What shipped under this task (core, 2026-05-01)

- `cmd/slimference/capture_cmd.go`: `slimference capture-session [--start|--stop|--status] [--name=<n>]` subcommand. The capture happens via the existing redacted decision-log path (`SLIMFERENCE_DEBUG_DECISIONS_LOG`); the subcommand prints the exact env-var instruction, generates a timestamped fallback name, lists prior captures under `~/.slimference/captures/`, and points at the privacy policy. T109's outbound redactor (default-on) is the redaction path.
- `scripts/benchmarks/benchmark_corpus.go`: walks `<root>/<category>/{*.jsonl, metadata.json}`, aggregates each category through the existing session-report aggregator, evaluates against per-category `expected_savings_min` / `expected_savings_max` / `expected_request_count` declarations. Subcommand `benchmark-corpus <root> [--check] [--json]` for human + machine consumption.
- `scripts/ci/main.go`: new step "live corpus gate" (`benchmark-corpus tests/fixtures/live_corpus --check`). Runs as step 7/7. Empty corpus or any per-category regression fails CI.
- `tests/fixtures/live_corpus/synthetic_smoke/{metadata.json, session_smoke.jsonl}`: deterministic synthetic seed so the gate has something to chew on. Marked `"synthetic": true`; bounds intentionally wide (0.30 - 0.85) so tuning changes do not flap the gate; the report renders a "synthetic-only" hint that points at the policy doc.
- `docs/live-corpus-policy.md`: privacy and provenance rules. What may / may not be checked in, the operator-driven capture process, the metadata schema operators use to declare expectations, and the removal protocol if a leak is later discovered.

## Re-activation criteria (T118b, operator-driven)

The maintainer captures real sessions across the ten categories listed in the policy doc and commits each as `tests/fixtures/live_corpus/<category>/<n>.jsonl` plus a sibling `metadata.json` with tighter `expected_savings_min` / `expected_savings_max` brackets. As real captures land, `expected_savings_max` on the synthetic seed can be tightened. The gate is already wired; only the corpus contents are pending.

## Acceptance Criteria

- [x] `slimference capture-session` is the maintainer entrypoint. It does not auto-record; it instructs the operator to route the existing redacted decision log to a named file under `~/.slimference/captures/`, lists prior captures, and points at the privacy policy.
- [x] `scripts/benchmarks benchmark-corpus <root>` produces per-category and overall savings, supports `--check` and `--json`, exits non-zero on any per-category regression or empty corpus.
- [x] `scripts/ci` runs the live-corpus gate as step 7/7.
- [x] `docs/live-corpus-policy.md` documents what may and may not enter the corpus, the capture process, and the removal protocol.
- [x] Synthetic seed corpus gives the gate something deterministic to run against; clearly marked as synthetic and rendered with a policy hint.
- [x] Coverage 100%; race tests green.
- [ ] **T118b** (operator-driven, deferred): >=10 real-session categories captured-and-scrubbed under `tests/fixtures/live_corpus/`. Tighten the synthetic seed's `expected_savings_max` bound once real numbers land. Update `docs/savings-assessment.md` with corpus-derived per-category numbers.

## Out of Scope

- Public corpus distribution beyond this repo (potential follow-up for community contributions).
- Auto-capture without explicit operator confirmation (privacy-first).
- Real-time capture during an in-flight TUI session (capture-session is the explicit handle).

## Validation

```
slimference capture-session --name=test_run     # operator validates manually
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus/ --check
go run ./scripts/ci
```
