# T346 Deterministic evidence selector and decision manifest

## Why

Slimference already has strong deterministic reducers, cache guards, and
flight/savings telemetry, but the product still lacks one unified block-level
truth: content class, preserved evidence, safety guard, selected strategy,
skip reason, savings, and cache impact. This task turns the Headroom/RTK
research takeaways into Slimference-native Go logic without adding a lossy
model, semantic summary, manual experiment surface, or default-off feature.

## Acceptance

- Every new mechanism is deterministic, default-on-safe or auto-safe, and
  fail-open.
- No local/external model, semantic summarizer, CCR retrieval dependency, or
  model-facing memory insertion is introduced.
- A shared content classifier covers `test`, `log`, `search`, `diff`,
  `stacktrace`, `json`, `code`, `plain`, and `unknown`.
- Evidence signals are explicit and content-free: error keywords, stacktrace,
  outlier, dedupe, changed hunks, recency, cache hot zone, first/last,
  exit/status, paths, counts, and warnings.
- Layer 0 reducer decisions export content class, evidence contract, safety
  class, action, reason, before/after token estimates, and net savings.
- Request flight/decision logs carry the unified manifest without raw prompt or
  tool payload.
- Cache hot-zone and negative-net guards remain visible so prompt-cache
  regressions cannot be hidden by local reducer savings.
- Tests prove classifier stability, evidence signal detection, manifest
  redaction, positive/negative accounting, and fail-open behavior.
- `docs/documentation.md`, `docs/benchmarks.md`, and relevant reports describe
  the final product surface accurately.
- Final gate: `go run ./scripts/ci` green, or any blocker is documented with
  exact root cause.

## Plan

1. Add a small Go-native `internal/evidence` package for content classes,
   evidence signals, safety labels, and block decision records.
2. Wire Layer 0 reducer dispatch into the evidence package so every reducer
   attempt can explain what it did and why, without storing output text.
3. Extend `debug.DecisionEntry`, `MechanismAccounting`, and flight summaries
   with the manifest fields while keeping JSON backward compatible.
4. Add deterministic tests and redaction tests before broader reporting work.
5. Surface the manifest in session savings reports/TUI only as aggregate
   counters and reasons, never as raw content.
6. Update docs and run the full CI gate.

## Sub-Tasks

- [x] Read current reducer, decision-log, flight, savings, and docs surfaces.
- [x] Add `internal/evidence` types/classifier/signal detector with tests.
- [x] Wire Layer 0 reducer attempts into evidence decisions.
- [x] Extend debug flight/accounting without raw payload leakage.
- [x] Add reporting/TUI aggregate view where existing surfaces already consume
  decision logs.
- [x] Update docs and benchmark semantics.
- [x] Run targeted tests and `go run ./scripts/ci`.

## Notes

- Product rule: build only mechanisms that can remain active by default/auto.
- Rejected by design: local ML compression, semantic summaries, CCR retrieval
  as a default product path, and broad output-token brevity prompts.
- `cache_hot_zone` is now emitted from provider-cache/prompt-cache accounting,
  not inferred from raw prompt text.
- `slimference savings` reports aggregate evidence counts/classes/signals only;
  raw prompt/tool payload stays out of logs and reports.
- Second verification pass found and closed the UI gap: the Savings TUI now
  shows the same content-free evidence decision aggregate as CLI/JSON reports.
- Targeted tests passed: `go test ./internal/evidence ./internal/debug
  ./internal/filter ./internal/proxy ./cmd/slimference`.
- Follow-up targeted TUI test passed: `go test ./internal/tui`.
- Final gate passed: `go run ./scripts/ci`.

## Deviations

None.
