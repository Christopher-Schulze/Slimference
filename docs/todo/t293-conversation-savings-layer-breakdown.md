# TASK T293: Conversation savings layer breakdown

## Why

The unified savings view already reports aggregate layer totals and top
decision sessions, but operators cannot see which Codex conversation saved
tokens through which layer. The product needs a zero-drawdown, measured-only
view that answers: per conversation, how many tokens were saved by Layer 0,
Layer 1, Layer 2, Layer 3, output-reduce, and tool pruning.

## Acceptance

- `slimference savings` text output shows a compact per-conversation layer
  breakdown for top decision-log sessions.
- JSON output includes the same per-session layer fields.
- Totals are measured from decision logs only; no guessed savings are invented.
- Fallback logic uses request token stages when mechanism-level accounting is
  absent.
- Tests cover session ordering, layer allocation, fallback, and text output.
- Docs/help describe the conversation breakdown.

## Sub-Tasks

- [x] Add per-session layer fields and aggregation.
- [x] Render conversation layer breakdown in text/JSON.
- [x] Update tests and docs/help.
- [x] Run focused tests and full CI.

## Notes

- This is observability only and cannot introduce model-quality drawdown.
- Implemented measured-only `L0`, `L1`, `L2`, `L3`, `out`, and `tools` net
  fields on decision sessions and aggregate decision totals.
- Cache accounting is classified as current Layer 2 only; legacy semantic
  summary Layer 2 remains absent from the product path.
- Output-reduce and tool-prune stay separate from layer totals because their
  current request summaries expose different accounting surfaces.
- Verification:
  - `go test ./cmd/slimference -run 'TestComputeSavingsDecisionMechanismBreakdown|TestFormatSavingsTextDecisionCacheAndSigned|TestFormatSavingsCSV|TestHandleSavingsCmd|TestSavingsHelpDocumentsConversationLayerBreakdown'`
  - `go test ./cmd/slimference`
  - `go test ./docs`
  - `go test ./...`
  - `go run ./scripts/ci`
  - `go run ./scripts/build --install`
  - `/Users/christopher/.local/bin/slimference savings today`

## Deviations

- None.
