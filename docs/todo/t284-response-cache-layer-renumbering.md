# TASK 284: Response cache layer renumbering

## Why

After retiring semantic context replacement, the product should not keep a
visible numbering gap where the safe response/provider cache is still called
Layer 2. The cache layer must become the current Layer 2 everywhere product
state, reports, docs, scripts, and operator UI describe active layers.

The old `Layer 2` semantic summarization/OCRL/context-ledger path stays removed.
This task is a renumbering and compatibility cleanup, not a reintroduction of
semantic summaries.

## Acceptance

- Current product docs describe active layers as Layer 0, Layer 1, Layer 2
  response/provider cache, and Layer 3 output/tool-surface reduction.
- Planner, benchmark reports, release proof summaries, analytics, and TUI use
  `Layer 2` / `L2` for response/provider cache.
- Existing persisted or fixture fields using old `layer3_*` names remain
  readable as legacy aliases where needed.
- No model-facing semantic summary, OCRL, context-ledger, or MiniMax path is
  reintroduced.
- `go test ./...` passes.
- `go run ./scripts/ci` passes.

## Sub-Tasks

- [x] Audit and rename current Layer 2/L2 product surfaces.
- [x] Preserve legacy input aliases for old `layer3_*` persisted/fixture fields.
- [x] Update docs and TODO current-state wording.
- [x] Re-run full gates and commit.

## Notes

- This task intentionally avoids implementing RTK aggressive code-signature
  summaries because they are not default-safe under the project drawdown rule.
- Active product surfaces now use Layer 2 / L2 for response/provider cache:
  config, runtime toggles, planner, analytics, benchmark reports, TUI state,
  TUI keybindings, admin/state, savings reports, and current docs.
- Old `layer3_enabled`, `min_layer3_saved`, `l3`, and runtime layer `3` inputs
  remain accepted only as compatibility aliases for old configs, fixtures, or
  historical records. New output writes Layer 2 / L2.
- Layer 0 was re-audited against T260 evidence contracts. Existing guards
  require shorter output, fail-open/full-pass on ambiguous shapes, first-read
  full-pass, search-shape guards, late critical evidence retention, and parser
  family tests. No further default-on aggression was added because the next
  possible wins would require fresh real traffic proving no evidence loss.
- Layer 3 was re-audited against T267/T268. Aggressive output-reduce remains
  gated by task shape, exact-reply/command-output/repair guards, A/B evidence,
  auto-downgrade, and repair/re-ask cooldowns. No broad unsafe shortening was
  promoted.
- RTK delta review remains closed: command-output and hook-safe ideas are
  already ported or covered; lossy aggressive code-signature summaries remain
  rejected as a default product path.
- Verification:
  - `go test ./internal/config ./internal/proxy ./internal/tui ./scripts/benchmarks ./scripts/utils ./cmd/slimference`
  - `go test ./...`
  - `go run ./scripts/ci` passed all 8 steps with total coverage 96.6%,
    live-corpus PASS, codex smoke PASS, and leaf-audit PASS.

## Deviations

- None.
