# T275 - Analytics/proof event queue hardening

## Status

Done.

## Source

External model-review follow-up after validating `trySendAnalytics` and the
analytics worker implementation at commit `f0f96ed`.

## Evidence

`internal/proxy/proxy.go::trySendAnalytics` sends to a buffered queue and drops
on overflow while incrementing `analyticsDropped` and emitting a rate-limited
warning. This protects request latency, but proof-critical events can still be
lost under burst pressure.

## Why

Slimference's product claims depend on content-free telemetry and proof
counters. Dropping low-value debug events is acceptable. Silently losing
proof-critical request, reducer, fallback, or safety events is not acceptable
for release evidence. The queue should preserve request latency and fail-open
routing while giving product/proof events priority.

## Scope

- Classify analytics events by priority:
  - proof/safety/product events
  - request-processed and overflow events
  - debug or low-value events
- Preserve non-blocking request behavior for low-value events.
- For proof-critical events, choose one of:
  - small bounded high-priority queue drained first
  - short timeout enqueue with explicit dropped-proof counter
  - synchronous minimal counter update plus async rich event
- Surface separate counters for dropped low-priority and dropped
  proof-critical events.
- Ensure `/admin/state` and release proof tooling can detect proof-event loss.

## Non-goals

- Do not block normal request hot paths indefinitely.
- Do not log raw payloads.
- Do not make analytics delivery a dependency for routing success.

## Acceptance

- Table-driven tests show high-priority events survive low-priority queue
  saturation.
- Tests show low-priority events can still drop without request failure.
- `/admin/state` or existing product status exposes proof-event drop counters.
- Release proof tooling fails closed if proof-critical event loss is reported in
  the proof window.
- `go test ./internal/proxy ./internal/control ./scripts/utils -count=1`
  passes.
- `go run ./scripts/ci` passes.

## Verification

- Synthetic queue saturation tests.
- Focused release-proof-report test with a proof-drop counter.
- Full CI.

## Notes

- Runtime model quality is unaffected by the queue, but proof integrity is part
  of the user's max-out bar.
- Implemented a priority classification for analytics events. Request,
  secret, error, rate-limit, and overflow events are proof-critical; debug /
  low-value events remain low-priority.
- Proof-critical events get one bounded synchronous drain attempt when the
  shared queue is full, then retry enqueue. If they still cannot enqueue,
  Slimference increments `analytics_proof_events_dropped`; low-priority drops
  increment `analytics_low_priority_events_dropped`.
- `/admin/state.savings` and `/admin/state.savings.product` now expose total,
  proof-critical, and low-priority analytics drop counters. Any proof-critical
  loss pushes product status to `attention`.
- Aggregate, workday, WSS proof matrix, proof inventory, capture summaries, and
  release proof reports carry the proof-loss counters. Release proof fails
  closed if a proof window reports proof-critical analytics loss.
- Verification:
  - `go test ./internal/proxy -run 'AnalyticsQueue|SavingsProbe' -count=1`
  - `go test ./internal/control -run 'SavingsSummaryProductSignals' -count=1`
  - `go test ./scripts/utils -run 'AggregateSavingsReportsProofEventDrops|WorkdaySavingsStartAndFinishJSONDelta|ReleaseProofReportRejectsAnomalyRows|ReleaseResourceProofBundleRejectsProofEventDrops|WSSProof|CodexCapture' -count=1`
