# Benchmarks

Date: 2026-04-19

This document records the currently checked-in benchmark evidence that is
reproducible from the repository as it exists today.

## Session Replay Baseline

Command:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
go run ./scripts/benchmarks session-report --markdown tests/fixtures/sample_session.jsonl
```

Console summary:

```text
Slimference session report
============================================================
Requests:           3
Original tokens:    8150 (avg 2716 / request)
Final tokens:       4835
Saved tokens:       3315 (avg 1105 / request)
Savings ratio:      40.67%
Layer 1 saved:      2055
Layer 2 saved:      1260
Cache hit rate:     0.00% (0 / 3)

Layer 1 sub-layer breakdown:
  dedup                        875
  json_compact                 780
  ansi_strip                   400

Per-provider request count:
  anthropic    2
  openai       1
```

Markdown export:

| Metric | Value |
| --- | --- |
| Requests | 3 |
| Original tokens | 8150 |
| Final tokens | 4835 |
| Saved tokens | 3315 |
| Savings ratio | 40.67% |
| Layer 1 saved | 2055 |
| Layer 2 saved | 1260 |
| Cache hits | 0 |

## Codex Smoke Corpus

Commands:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex
```

This is a checked-in smoke corpus for the Codex reporting path. It proves the
reporting harness can aggregate a Codex directory, provider split, Codex route
split, and per-layer savings. It is not a live Codex production corpus.

`tests/fixtures/codex/codex-metadata.json` is the single source of truth for
the corpus: it declares the provenance (scrubbing, Codex version, captured
date, scenarios) and the `regression_gate` baseline. `session-report` renders
the metadata block in front of the numbers when the path is a directory, and
`codex-smoke-gate` enforces the baseline. The gate is wired as the last step
of `go run ./scripts/ci`, so any unexpected change in the smoke fixture fails
the local CI gate before review.

When live Codex capture is approved, the same metadata schema applies: replace
the synthetic fixtures and update the `regression_gate` to a value drawn from
the real corpus, not from intuition.

For the per-category live corpus under `tests/fixtures/live_corpus/`, run:

```
go run ./scripts/verify -mode live-corpus-plan -category codex_cli_tool_heavy -client codex_cli
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --check
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --json
```

The category gate reports and can enforce evidence level, input-layer savings,
output tokens, provider-cache read/create/cached tokens, output-reduce hits,
error count, latency p95, and planner replay consistency. Planner replay
compares each recorded dry-run plan with the observed layer execution:
expected-active actions, observed active actions, missed actions, bypass/tunnel
actions that still saw activity, safety-blocked requests, and expected
planner savings. The same reports also emit an observed layer-combination
matrix (`L0+L1`, `L0+L1+L3`, `L0+L1+L3+L4`, `WS`, etc.) with request count,
saved tokens, output tokens, and errors. This keeps future "saves N percent"
and "safe to default-on" claims tied to real captured sessions instead of
synthetic smoke data.

| Metric | Value |
| --- | --- |
| Requests | 2 |
| Original tokens | 5600 |
| Final tokens | 2400 |
| Saved tokens | 3200 |
| Savings ratio | 57.14% |
| Layer 0 saved | 200 |
| Layer 1 saved | 1700 |
| Layer 2 saved | 1000 |
| Layer 3 saved | 300 |
| Cache hits | 1 |
| Prompt cache read tokens | 300 |
| Prompt cache create tokens | 120 |

| Provider | Requests |
| --- | ---: |
| codex_chatgpt | 2 |

| Codex route | Requests |
| --- | ---: |
| /backend-api/codex/responses | 1 |
| /v1/responses | 1 |

## Scope and Limits

- This is the repository's checked-in fixture baseline, not yet a 100-session
  production corpus.
- The Codex smoke corpus is fixture-scale and exists to keep the reporting path
  executable until live Codex capture is explicitly allowed.
- The harness is real and reproducible today.
- The next evidence upgrade is data, not core code: add a larger recorded
  session corpus and rerun the same command.
