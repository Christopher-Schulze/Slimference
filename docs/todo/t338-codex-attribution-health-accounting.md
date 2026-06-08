# T338 Codex Attribution Health Accounting

## Why

Correct thread attribution must be measurable, not just assumed from individual rows. A savings report can otherwise look plausible while still hiding anonymous Codex traffic in fallback buckets.

## Acceptance

- `slimference savings` reports Codex decision-log attribution health.
- JSON and CSV include total Codex decision requests, attributed Codex requests, unattributed Codex requests, and attribution rate.
- Text output shows a compact `Codex attribution` line when Codex decision rows exist.
- Attribution means a routed Codex request has a strong `codex-http:<thread>` or `codex-wss:<thread>` session ID.
- Non-Codex provider rows are not counted as Codex attribution misses.
- No runtime payload mutation, cache steering change, prompt change, or model-facing behavior change.

## Notes

- Product impact: proof and diagnostics. This makes attribution regressions visible without changing savings behavior.
- Drawdown risk: none; this is offline/report-only aggregation over existing content-free decision summaries.

## Verification

- `go test ./cmd/slimference -run 'TestSavingsCodexAttributionHealth|TestSavingsSessionsKeepParallelCodexThreadsSeparate|TestSavingsSessionsUseCodexHTTPThreadMetadata|TestComputeSavingsDecisionMechanismBreakdown' -count=1`
