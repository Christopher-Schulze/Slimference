# Structure Extract Accuracy

Date: 2026-04-19

This document records the current output of the checked-in
`structure-accuracy` harness. The current harness is intentionally a
scaffolding-grade measurement: it reports compression ratio and a lightweight
declaration-overlap signal (`decl_recall`), not full parser-backed precision
and recall.

## Reproduction

Command:

```bash
go run ./scripts/utils structure-accuracy repos
```

Aggregate result from the current foreign-repo corpus:

```text
Files: 538  changed: 474  size_ratio: 0.30  decl_recall: 0.35 (5650/15964)
```

## What This Means

- The harness is working and produces a stable, diffable baseline.
- The current regex extractor is achieving aggressive size reduction on the
  scanned corpus (`size_ratio: 0.30`).
- The current `decl_recall` signal is deliberately conservative and incomplete.
  It only checks whether declaration-like lines survive in the extracted
  summary. It is useful for drift tracking, but it is not yet a true
  parser-backed accuracy metric.

## Current Decision

- Keep the regex-based extractor for now.
- Treat this report as a guardrail and trend signal, not as final proof of
  language-level precision/recall.
- The next step is corpus and truth quality, not more reporting scaffolding:
  add parser-backed ground truth per language, then rerun the same harness.
