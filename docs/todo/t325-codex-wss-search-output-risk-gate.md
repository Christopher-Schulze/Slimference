# T325 Codex WSS Search-Output Reducer Risk Gate

## Why

Fresh scoped Codex CLI testing against `/Users/christopher/CODE/Golem` still
hit upstream `invalid_request_error` after T324. The live log showed the
previous WSS request still ran `layer0 filter applied filter=search_output`.
That means the original WSS search fail-open was too narrow: it blocked the
stable search-key workload, but not every path that can reach the same
`search_output` reducer.

Reconc was not the root cause. The visible `.reconc` command was a normal tool
command in the same workflow; the unsafe class was Slimference mutating Codex
WSS search/path-list tool output before the upstream request.

## Acceptance

- Codex WSS Phase-F blocks every command that can route to the `search_output`
  reducer, including `rg` / `grep` / `git grep`, `find`, `fd`, empty-result
  search tools, and output-inferred search payloads.
- The block is WSS-only. HTTP, hook, and non-WSS routes keep deterministic
  search/path-list savings.
- WSS `.reconc` path-list output stays byte-for-byte model-facing original text.
- WSS shell-wrapper or unresolved command search payloads stay byte-for-byte
  original text when the output itself looks like search results.
- Focused filter/proxy regressions, full tests, CI, and rebuilt installed binary
  pass.

## Sub-Tasks

- [x] Add an explicit filter-side predicate for commands that can enter
  `search_output`.
- [x] Use that predicate plus output-shape inference as the Codex WSS risk gate.
- [x] Add WSS regression coverage for shell-wrapper/inferred search output.
- [x] Add WSS regression coverage for `find .reconc ...` path-list output.
- [x] Update docs and task SSOT.
- [x] Run verification gates and install the new build.

## Notes

- The product contract is no upstream 400s and no model-facing context loss.
  WSS search/path-list token savings are therefore disabled until a future live
  capture proves this exact Codex WSS shape safe.
- Deterministic read/ranged-read, archive-backed read-delta, chunk dedup, output
  reduce, tool pruning, and non-WSS Layer 0 reducers are not removed by this
  task.
- Reconc support means its outputs pass through safely in WSS unless a
  separately certified deterministic reducer exists for that exact shape.

## Deviations

- None.
