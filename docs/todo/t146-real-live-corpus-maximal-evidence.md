# TASK 146: Real live corpus maximal evidence program

Status: IN PROGRESS (T146a evidence foundation, T146b planner replay metrics, and T146c layer-combination matrix landed 2026-05-14; live operator captures still pending)
Priority: P0
Scope: `tests/fixtures/live_corpus/`, `scripts/benchmarks/`, `scripts/verify/`, `internal/flight/`, `internal/redaction/`, `internal/quality/`, `cmd/slimference/debug_cmd.go`, `cmd/slimference/gain_cmd.go`, `docs/live-corpus-policy.md`, `docs/savings-assessment.md`.

## Why

Every serious percentage claim depends on real traffic. Synthetic corpora are useful for regression, not for product truth. T118b and T140 already say live corpus is needed; Phase AB makes it the evidence gate for every aggressive default, WebSocket mutation, Layer 2 early trigger, cache claim, parser expansion, and output-reduce profile.

## Target State

Slimference has a repeatable evidence program:

1. Capture real operator sessions with redaction.
2. Replay them through layer combinations.
3. Compare baseline vs optimized runs.
4. Measure tokens, cached tokens, output tokens, latency, failures, and quality.
5. Publish honest numbers by session category and layer.
6. Block default-on changes when evidence is missing or negative.

## Corpus Categories

Required minimum categories:

- Codex CLI short direct Q/A.
- Codex CLI tool-heavy coding loop.
- Codex CLI large file-read/code-audit loop.
- Codex CLI failing-test/debug loop.
- Codex CLI long 30+ turn session.
- Codex App text turn through transparent proxy.
- Codex App WebSocket turn if observed.
- Browser-Use passthrough metadata.
- Voice/microphone bypass metadata.
- Disable/uninstall recovery path.
- Claude Code later, only after Codex path is certified.

## Work Packages

### WP1 - Capture workflow

- [x] Add a guided capture command:
  - start marker.
  - category label.
  - layer configuration snapshot.
  - proxy status.
  - Codex version/path if available.
  - stop marker.
  - export path.
- Implemented foundation: `go run ./scripts/verify -mode live-corpus-plan -category <category> -client codex_cli` prints the capture path, export command, metadata skeleton, benchmark checks, and mandatory privacy review.
- Never call paid/live providers from CI.

### WP2 - Redaction hardening

- [ ] Redact:
  - auth headers.
  - cookies.
  - API keys.
  - local absolute paths.
  - user-specific names.
  - repo secrets.
  - screenshots/images.
  - raw prompt text when policy says shape-only.
- Preserve:
  - token counts.
  - byte counts.
  - route mode.
  - layer decisions.
  - parser names.
  - cache usage numbers.
  - latency.
  - failure codes.
  - scrubbed structural payloads when approved.
- Current foundation consumes the existing T109/redacted `RequestSummary`/flight export path. Additional raw-payload redaction hardening remains pending until live captures expose a concrete gap.

### WP3 - Replay harness

- [ ] Replay corpus through:
  - no Slimference baseline where possible.
  - L0 only.
  - L1 only.
  - L2 only.
  - L3 only.
  - output reduce only.
  - planned combinations.
  - full pipeline.
- Implemented foundation: `benchmark-corpus` now aggregates category-level cache, output, error, latency evidence, recorded planner-vs-actual execution signals, and observed layer-combination matrices from existing JSONL request summaries. True alternate-run layer-combination replay remains pending.
- Record:
  - input tokens.
  - output tokens.
  - provider cached tokens.
  - estimated vs reported splits.
  - latency p50/p95/p99.
  - error/fallback count.
  - quality check result.

### WP4 - Quality checks

- Scenario-specific validators:
  - exact error retained.
  - file/path retained or intentionally normalized.
  - command retained.
  - changed-file set retained.
  - user decision retained.
  - tool success/failure retained.
- For L2 summaries, compare full-context answers to summarized-context answers on golden questions.
- For output reduce, detect repair turns and user re-asks.
- Implemented foundation: corpus metadata can now fail categories on `expected_max_errors`.
- Implemented 2026-05-15: category metadata supports failable `scenario_validators`
  for `tool_heavy`, `cache_reuse`, `output_reduce`, `planner_alignment`,
  `websocket`, `low_error`, and `layer_combo_diversity`. Unknown
  validator names fail the gate instead of silently weakening evidence.

### WP5 - Reporting

- [ ] Update `docs/savings-assessment.md` from generated evidence, not manual optimism.
- Report by:
  - layer.
  - provider/model.
  - route mode.
  - task category.
  - short/medium/long session.
  - cache hit/miss.
- Labels:
  - measured.
  - provider-reported.
  - estimated.
  - synthetic.
  - insufficient evidence.
- Implemented foundation: `benchmark-corpus` text/JSON report now separates input-layer savings, output tokens, provider cache read/create/cached tokens, output-reduce applications, error count, latency p95, and evidence level.
- Planner replay is now reported when captured summaries contain `plan`: requests with plan, decision count, expected planner savings, expected-active actions, observed-active actions, misses, bypass/tunnel activity, and safety-blocked requests.
- Layer-combination reporting is now emitted for every corpus category and
  session report. It groups actual observed combinations such as `L0+L1+L3`,
  `L1+L2`, `L0+L1+L3+L4`, or `WS` by request count, saved tokens, output
  tokens, and errors. This is not a fake simulator; it is factual evidence
  about which combinations actually ran.

### WP6 - Default-on gate

- [x] Each aggressive task declares corpus thresholds:
  - minimum samples.
  - minimum net saving.
  - max latency overhead.
  - max fallback/error rate.
  - max quality regression.
- Default-on requires passing thresholds.
- Implemented foundation: category `metadata.json` supports `evidence_level`, `expected_max_errors`, `expected_latency_p95_max_ms`, `expected_provider_cache_read_min`, `expected_output_reduce_applied_min`, `expected_planner_missed_max`, `expected_planner_bypass_applied_max`, and `scenario_validators` in addition to savings/request gates.

## Acceptance

- [ ] At least the required Codex CLI categories are captured and scrubbed.
- [ ] Codex App categories are captured when operator runs T140.
- [ ] Replay can compare layer combinations without live network calls.
- [x] Reports show observed layer-combination matrices without live network calls.
- [x] Reports separate input compression, output reduce, cache billing, and latency.
- [x] Reports planned-vs-actual planner execution signals from captured request summaries.
- [x] Quality gates are scenario-specific and failable.
- [ ] Every Phase AB task references its required corpus gate.
- [x] `go run ./scripts/ci` includes a corpus regression check that skips only live capture, not replay validation.

## Implementation Notes

- 2026-05-14 T146a:
  - `scripts/verify` gained `-mode live-corpus-plan`.
  - `scripts/benchmarks benchmark-corpus` now carries output-token, provider-cache, output-reduce, error, latency-p95, and evidence-level fields in `CategoryResult`.
  - `metadata.json` category gates now support explicit cache/output/latency/error thresholds.
  - `docs/live-corpus-policy.md` documents the new runbook command and metadata fields.
  - Focus tests: `go test ./scripts/benchmarks ./scripts/verify`.
- 2026-05-14 T146b:
  - `scripts/benchmarks` aggregates `plan` / `flight.plan` records from captured `RequestSummary` JSONL.
  - Category reports and JSON now include planner replay: requests with plans, decision count, expected planner savings, expected-active/observed-active/missed active actions, bypass-applied count, safety-blocked count, action counts, and risk counts.
  - Category metadata can gate `expected_planner_missed_max` and `expected_planner_bypass_applied_max`.
  - This is replay evidence only; it does not simulate alternative layer combinations or call providers.
  - Focus test: `go test ./scripts/benchmarks -cover`.
- 2026-05-14 T146c:
  - `session-report` and `benchmark-corpus` now emit an observed
    layer-combination matrix.
  - Combination keys use stable labels (`L0`, `L1`, `L2`, `L3`, `L4`, `WS`,
    or `none`) and aggregate requests, original tokens, saved tokens, output
    tokens, and errors.
  - The JSON report exposes `layer_combinations` per category so later A/B
    analysis can compare full-pipeline sessions against individual or planned
    combinations.
  - Focus test: `go test ./scripts/benchmarks -cover`.
- 2026-05-15 T146d:
  - `metadata.json` category gates now support `scenario_validators`.
  - The validators bind category names to concrete evidence: tool-heavy sessions
    must show L0/L1 savings, cache-reuse sessions must show L3/provider-cache
    evidence, output-reduce sessions must apply output-reduce, planner-alignment
    sessions must have no missed/bypass-applied plan actions, WebSocket sessions
    must show `WS`, low-error sessions must have zero errors, combo-diversity
    sessions must have multiple observed layer combinations, and L2-summary
    sessions must show L2 savings.
  - Focus test: `go test ./scripts/benchmarks -cover`.

## Expected Upside

- No direct saving, but it unlocks honest engineering.
- Prevents building expensive features that look good on synthetic samples and fail in real Codex loops.
- Gives the operator the only number that matters: actual usage reduction on their workflow.

## Non-Goals

- Do not commit raw private conversation payloads.
- Do not make CI depend on live OpenAI/Anthropic/MiniMax endpoints.
- Do not collapse synthetic and live evidence into one percentage.
