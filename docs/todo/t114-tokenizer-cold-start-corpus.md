# TASK 114: Anthropic tokenizer cold-start corpus calibration

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P2
Scope: `internal/tokens/`, `tests/fixtures/tokenizer_corpus/`, `scripts/utils/`
Driver: `anthropicTokenizer` starts with `bytes_per_token = 3500` (3.5 chars/token) and converges via EMA over upstream `usage.input_tokens` events with `alpha=0.05` - meaning ~20+ samples are required to fully shift the ratio. Until then, every saved-token report is wrong by up to ~20%. For dashboards, gain reports, and CI thresholds the cold-start error compounds: a fresh proxy install reports inflated savings for the first hour of use. Fix: ship a measured corpus and seed the EMA from it on startup, so cold-start error is bounded to ±5% from request 1.

---

## Problem

`internal/tokens/provider.go::newAnthropicTokenizer` always stores `3500` as the initial bytes/token ratio. The 3500 default was derived from Anthropic's blog post rule-of-thumb for English. Real ratios diverge:

- Anthropic Sonnet 4.6 on dense English: ~3.5
- On code (heavy ASCII, lots of punctuation): ~3.2
- On JSON (heavy quotes/braces): ~3.0
- On CJK content: ~1.0-1.5 (CJK correction handled separately)

Until 20+ EMA samples flow, every report is biased high or low depending on workload. We have ground truth: every `streamingRelayWithUsage` already extracts `usage.input_tokens` from upstream. We just don't persist it across restarts.

## Target State

1. **Bundled corpus**: `tests/fixtures/tokenizer_corpus/anthropic_sonnet.json` and `anthropic_opus.json` containing measured (text_sha8 -> observed_tokens, byte_len, model) tuples gathered from synthetic + lab measurements. Per-model entries.
2. **Cold-start fit**: `newAnthropicTokenizer(model)` loads the corpus, fits the per-model bytes/token ratio via least-squares regression, stores it as the initial value. EMA continues from there.
3. **Persistent calibration**: every observed `(estimated, observed)` pair is appended to `~/.slimference/calibration/anthropic.jsonl`. On startup, the file is replayed (capped at the last 1000 entries) so the EMA picks up where it left off rather than at 3500.
4. **Per-model branching**: the OEM tokenizer struct exposes per-model state (`map[string]int64` of byte-per-token ratios) so Opus and Sonnet can hold distinct ratios.

## Implementation Plan

### WP1 - Corpus format
- JSON Lines per model: `{"model":"claude-sonnet-4-6","sha8":"...","bytes":1234,"observed_tokens":345,"workload":"code|prose|json|cjk"}`.
- Tooling: `scripts/utils` adds `gather-tokenizer-corpus` subcommand that reads a debug session log, extracts `(text, observed)` pairs from streaming-relay events, and emits the fixture.

### WP2 - Fit at construction
- `newAnthropicTokenizer(model string)` reads `tests/fixtures/tokenizer_corpus/anthropic_<model_family>.json` (embedded via `go:embed`) and computes the initial ratio by minimising `sum((bytes/ratio - observed)^2)` over the corpus.
- Fallback when corpus missing or empty: 3500 default (today's behaviour).

### WP3 - Per-model ratio map
- Replace single `bytesPerTokenX1000 atomic.Int64` with `perModelRatios sync.Map` keyed on model string.
- `CountString(s)` and `observe(observed, estimated)` accept a model parameter.
- Existing call sites (`tokens.CountMessages`) get the model from the request context.

### WP4 - Persistent EMA
- New `internal/tokens/persist.go` with `LoadCalibration(path)` + `AppendObservation(path, ...)`.
- Calibration file capped at 1000 lines; oldest pruned on append.
- Load on `newAnthropicTokenizer`; append on every `observe`.

### WP5 - Telemetry
- `/admin/status.tokenizer.{model, bytes_per_token, samples_observed, calibration_age_hours}`.
- `slimference doctor` prints the active ratio per model and warns if no observations yet (cold-start state).

### WP6 - Tests
- Corpus loading unit tests with synthetic JSONL.
- Cold-start fit against a known corpus -> assert the chosen ratio is within 2% of best-fit.
- EMA continues from corpus-fitted ratio (not 3500) on first observation.
- File corruption / missing -> graceful fallback.
- Per-model isolation: observations on Sonnet do not move the Opus ratio.

### WP7 - Backward compat
- Existing `ObserveUpstreamUsage(provider, observed, estimated)` keeps the same signature; internally extracts the model.
- Old single-ratio EMA path lives behind `[tokens] per_model_calibration = true` (default true once shipped, false for emergency rollback).

## Acceptance Criteria

- [ ] Cold-start estimate within ±5% of observed first 5 requests on the calibrated workload.
- [ ] Per-model ratios are independent.
- [ ] Calibration file roundtrip works across restarts.
- [ ] Telemetry exposes the four fields above.
- [ ] Coverage 100%; race tests green.
- [ ] `scripts/utils gather-tokenizer-corpus` produces a valid corpus from a debug-decisions log.

## Out of Scope

- OpenAI / Codex per-model calibration (cl100k_base / o200k_base are exact, not heuristic).
- Re-fitting on every request (EMA is the runtime adjustment; full re-fit happens only on operator demand via `slimference recalibrate`).
- Public-facing accuracy claim (we still call this an estimator; it just becomes a much better one).

## Validation

```
go test -race ./internal/tokens/...
go run ./scripts/utils gather-tokenizer-corpus tests/fixtures/sample_session.jsonl
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
```
