# T324 Codex WSS Search-Output Fail-Open

## Why

After T323 separated output-reduce from Layer 0, a fresh scoped Golem Codex WSS
session still reproduced upstream `invalid_request_error`. Debug telemetry showed
`output_reduce.applied=false`, but the previous request had a Layer 0
`search_output` mutation. That makes the current root cause narrower and harder:
Codex WSS search-output mutation is not live-safe on the current Desktop/CLI
protocol shape.

The product rule wins over savings: a reducer that can trigger upstream 400s is
not allowed in the default WSS path. Search output must therefore pass through
unchanged on Codex WSS Phase-F until a new live capture set proves the exact WSS
shape safe again.

## Acceptance

- Codex WSS Phase-F search-output blocks do not enter first-pass search grouping.
- Codex WSS Phase-F search-output blocks do not seed or emit repeated
  search-output deltas.
- HTTP, hook, and non-WSS routes keep deterministic search-output reducers.
- Regression coverage proves WSS search output remains model-facing original text
  and records no Layer 0 search savings.
- Documentation no longer presents historical WSS search-loop savings as a
  current default-WSS promotion claim.
- Full tests, CI, and local rebuild/install pass.

## Sub-Tasks

- [x] Add a WSS search-output block in the Layer 0 tool-output reducer.
- [x] Keep non-WSS search reducers intact.
- [x] Replace the WSS search compaction regression with a pass-through safety
  regression.
- [x] Update product docs and task notes to separate historical search evidence
  from current WSS-safe product behavior.
- [x] Run focused proxy tests, full Go tests, CI, and reinstall the latest
  binary.

## Notes

- Root cause narrowed by live evidence: T323 made output-reduce telemetry truthful
  and disabled it on Layer0-mutated WSS turns. The next live Golem failure still
  followed a WSS `search_output` Layer 0 mutation, so the remaining unsafe
  mechanism is WSS search mutation itself.
- Savings impact: WSS `rg` / `grep` / `git grep` outputs currently save zero via
  search grouping or repeated search delta. This is intentional. Read/ranged
  read, git status/diff, test/build filters, exec-envelope stripping,
  provider-cache steering, and non-WSS search reducers remain available.
- Drawdown impact: none in model quality. The model receives original search
  output instead of a reduced form. The only cost is lower token savings for WSS
  search until re-certified.

## Deviations

- None.
