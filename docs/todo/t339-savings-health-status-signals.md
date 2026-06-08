# T339 Savings Health Status Signals

## Why

Raw cache and attribution counters are useful, but users and scripts need a single status signal that says whether a savings report is clean, warming, neutral, or needs attention.

## Acceptance

- `slimference savings` exposes `decision_cache_status` in JSON and CSV.
- Cache status is `ok` for positive cache reuse, `warming` for create-only cache activity, `attention` for negative net cache impact, and `none` when no decision-log cache activity exists.
- `slimference savings` exposes `decision_codex_attribution_status` in JSON and CSV.
- Codex attribution status is `ok` when all Codex decision rows carry strong `codex-http:<thread>` or `codex-wss:<thread>` session IDs, and `attention` when any Codex row is anonymous.
- Text output includes the same status labels next to cache net and Codex attribution lines.
- No runtime payload mutation, cache steering change, prompt change, or model-facing behavior change.

## Notes

- Product impact: clearer proof. This turns existing accounting into actionable regression signals.
- Drawdown risk: none; this is report-only aggregation over existing content-free decision summaries.

## Verification

- `go test ./cmd/slimference -run 'TestSavingsCodexAttributionHealth|TestFormatSavingsTextDecisionCacheAndSigned|TestFormatSavingsTextNegativeCacheNet|TestComputeSavingsDecisionMechanismBreakdown' -count=1`
