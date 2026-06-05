# TASK 279: Remove retired semantic Layer 2

Historical numbering note: this task removed the old semantic Layer 2
summary/OCRL/context-replacement path. Current product Layer 2 is the
response/provider cache layer.

## Why

Semantic summary/context replacement cannot satisfy Slimference's zero-drawdown
product rule. Any semantic summary or capsule replacement can hide details the
model later needs. The correct product direction was to remove that semantic
path entirely and keep savings on deterministic, recoverable, fail-open
mechanisms.

## Acceptance

- No Go production, test, script, or fixture code imports or references
  `internal/summarization`, `internal/contextledger`, MiniMax, OCRL, Layer2
  runtime status, Layer2 commands, summary queues, summary configs, or
  `after_layer2` accounting.
- Current product docs describe only Layer 0, Layer 1, Layer 2 cache, and Layer
  4 as active layers.
- Historical docs that remain linked clearly state that semantic Layer 2 was
  retired and do not instruct operators to enable summarization/OCRL.
- `go test ./...` passes.
- `go run ./scripts/ci` passes.
- Final audit proves no stale active Layer 2 surface remains.

## Sub-Tasks

- [x] Remove Layer 2 code packages and direct call sites.
- [x] Remove Layer 2 script, benchmark, verifier, and fixture accounting.
- [x] Update current architecture, policy, savings, map, benchmark, and context
  docs.
- [x] Retire stale Layer 2 task-detail files or isolate them as historical.
- [x] Run full tests and CI.
- [x] Run final stale-reference audit.
- [x] Commit locally.

## Notes

- Semantic Layer 2 may be mentioned only as a retired/removed mechanism in
  current docs; current Layer 2 means response/provider cache.
- Do not replace it with a new summary layer unless the replacement can prove no
  model-quality drawdown.
- `go test ./...` passed.
- `go run ./scripts/ci` passed all 8 steps.
- Final Go/script/fixture audit has zero active Layer 2/MiniMax/OCRL hits.
- Removed core paths verified absent.

## Deviations

- Large stale docs were replaced instead of edited line-by-line because their
  old content was a false normative target.
