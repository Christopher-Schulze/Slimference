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

## Scope and Limits

- This is the repository's checked-in fixture baseline, not yet a 100-session
  production corpus.
- The harness is real and reproducible today.
- The next evidence upgrade is data, not core code: add a larger recorded
  session corpus and rerun the same command.
