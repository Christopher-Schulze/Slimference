# T343: Live savings proof window

## Why

`slimference savings today` is correct for historical accounting, but it can
mix old anonymous Codex rows from previous binaries with current fixed rows.
That makes attribution and cache health look worse even after the installed
daemon is clean. Current-product proof needs a window tied to the running
daemon, not the calendar day.

## Acceptance

- `slimference savings live` is accepted by the CLI and completion.
- `live` uses the running daemon `started_at` timestamp as the decision-log
  window start and falls back to the last 30 minutes when no daemon is running.
- Historical anonymous Codex rows before the daemon start do not affect live
  attribution or cache health.
- The feature changes reporting only. No request payload, cache key, reducer,
  routing, or provider traffic is changed.
- Focused tests and the full CI gate pass.

## Changes

- Added a savings report window helper for `live`.
- Kept `today`/`week`/`month`/`all` behavior unchanged.
- Filtered decision-log proxy-flight summaries by the resolved live window
  before reusing the existing proxy-flight summarizer.
- Documented the live proof window in `docs/documentation.md`.

## Verification

- Focused gate passed:
  - `go test ./cmd/slimference -run 'TestComputeSavingsLiveUsesCurrentDaemonWindow|TestSavingsCodexAttributionHealth|TestComputeSavingsDetectsNegativeCacheNet|TestHandleSavingsCmd_BadPeriod' -count=1`
- Package gate passed:
  - `go test ./cmd/slimference ./internal/proxy ./internal/analytics -count=1`
- Full CI:
  - `go run ./scripts/ci` passed all 8 steps after T344 attribution hardening
    in the same goal turn; total coverage `95.3%`
