# T331: Cache net proof accounting

Status: done

## Why

Provider-cache savings must be provable, not guessed. Gross cache-read tokens are
not enough because cache creation can add cost, and a changed cache strategy can
lower hit rate while local token deletion still looks positive. The product
needs visible per-period and per-session cache net accounting so regressions are
obvious.

## Acceptance

- `slimference savings` reports provider-cache read, create, net, hit request
  count, hit rate, create request count, and negative-cache-net request count.
- Per-session savings rows include cache net and cache hit rate.
- JSON/CSV carry the same fields for later TUI/admin use.
- A create-only cache request is tested as negative net instead of being hidden.
- No runtime cache steering or prompt mutation changes are introduced.

## Subtasks

- [x] Add cache-net fields to `SavingsSummary` and `SavingsSessionSummary`.
- [x] Aggregate cache read/create/net/hit-rate from decision-log request
  summaries.
- [x] Render cache net and hit-rate in text output and CSV/JSON fields.
- [x] Add regression tests for positive and negative cache-net accounting.
- [x] Update docs to explain local savings vs provider-cache net proof.

## Notes

- This is proof/accounting only. It does not change cache keys, retention,
  provider request fields, WSS mutation, or any savings policy.
- Negative cache net is surfaced as a first-class number because it is the
  clearest signal that cache steering is not paying for itself.

## Verification

- `go test ./cmd/slimference -run 'TestComputeSavingsDecisionMechanismBreakdown|TestFormatSavingsTextDecisionCacheAndSigned|TestComputeSavingsDetectsNegativeCacheNet' -count=1`
