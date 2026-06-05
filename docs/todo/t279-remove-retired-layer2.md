# TASK 279: Remove retired Layer 2

## Why

Layer 2 summary/context replacement cannot satisfy Slimference's zero-drawdown
product rule. Any semantic summary or capsule replacement can hide details the
model later needs. The correct product direction is to remove Layer 2 entirely
and keep savings on deterministic, recoverable, fail-open mechanisms.

## Acceptance

- No Go production, test, script, or fixture code imports or references
  `internal/summarization`, `internal/contextledger`, MiniMax, OCRL, Layer2
  runtime status, Layer2 commands, summary queues, summary configs, or
  `after_layer2` accounting.
- Current product docs describe only Layer 0, Layer 1, Layer 3, and Layer 4 as
  active layers.
- Historical docs that remain linked clearly state that Layer 2 is retired and
  do not instruct operators to enable it.
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

- Layer 2 may be mentioned only as a retired/removed mechanism in current docs.
- Do not replace it with a new summary layer unless the replacement can prove no
  model-quality drawdown.
- `go test ./...` passed.
- `go run ./scripts/ci` passed all 8 steps.
- Final Go/script/fixture audit has zero active Layer 2/MiniMax/OCRL hits.
- Removed core paths verified absent.

## Deviations

- Large stale docs were replaced instead of edited line-by-line because their
  old content was a false normative target.
