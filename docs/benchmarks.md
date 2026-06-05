# Benchmarks

Date: 2026-06-05

This document records benchmark evidence that should remain reproducible from
the checked-in repository. Layer 2 has been removed; current reports and claims
must attribute savings only to active product paths.

## Session Reports

Run:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
go run ./scripts/benchmarks session-report --markdown tests/fixtures/sample_session.jsonl
```

Expected report dimensions:

| Dimension | Meaning |
| --- | --- |
| Original tokens | Estimated model-visible input before Slimference reducers |
| Final tokens | Estimated input after active reducers |
| Saved tokens | `original - final` |
| Layer 0 saved | Tool-output / Codex reducer savings |
| Layer 1 saved | Deterministic compression savings |
| Layer 2 saved | Response/cache savings where applicable |
| Output reduce | Provider-output / directive accounting |
| Cache hits | Local response-cache hits |

Do not record or expect a Layer 2 savings column. If a fixture still contains
Layer 2 fields, the fixture is stale and must be regenerated or scrubbed.

## Codex Smoke Corpus

Run:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex
```

The Codex smoke corpus proves the reporting and regression-gate path on
checked-in scrubbed data. It is not a production savings claim. The final
step of `go run ./scripts/ci` enforces the smoke gate so report schema drift is
caught locally.

`tests/fixtures/codex/codex-metadata.json` declares provenance, scenarios, and
the regression baseline. It must list only active layers and active workload
classes.

## Live Corpus

For the per-category live corpus:

```bash
go run ./scripts/verify -mode live-corpus-plan -category codex_cli_tool_heavy -client codex_cli
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --check
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --promotion-check
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --maxx-check
```

The gate reports and can enforce evidence level, input-layer savings,
output-wire savings, provider-cache read/create/cached tokens, output-reduce
hits, error count, latency p95, host-resource status, and planner replay
consistency. It also emits an observed layer-combination matrix such as
`L0+L1`, `L0+L1+L2`, `L3`, `WS`, and `none`.

## Scope and Limits

- Checked-in smoke data keeps gates executable; it is not enough for a final
  production savings claim.
- Release claims require clean live Codex CLI and Desktop evidence plus
  resource/profile bundles.
- Savings must be reported by active layer and route. Provider-cache economics,
  local input savings, and output-wire savings stay separate.
- A removed Layer 2 must not appear in benchmark output, metadata, fixtures, or
  release proof.
