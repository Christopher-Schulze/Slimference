# TASK 185: Output-reduce telemetry in admin status

Status: TODO (planning 2026-05-16; closes T165/T166/T167 observability gap)
Priority: P1 (operator visibility is the difference between "we shipped it" and "we know it works")
Scope: `internal/proxy/admin.go`, `internal/proxy/handler.go` (counter sites), new types in `internal/types/admin.go` (or wherever AdminStatus lives)

## Why

The T165/T166/T167 sprint emits everything via `log.Debug` (`outstop merged`, `streamcut fired`, `repdet body rewritten`). Operators running the proxy without `--log-level=debug` see nothing. Without per-session counters surfaced in `/admin/status`, there is no way to answer "did the output-reduction stack save anything today?" - which makes auto-tuning, regression detection, and live-corpus calibration impossible.

**Why:** Telemetry is the load-bearing signal for the entire output-reduction subsystem. Without counters in the admin surface, T173 / T169 / T177 follow-ups have no baseline to measure against.
**How to apply:** Add atomic counters on the `Proxy` struct, increment from the three wire sites (outstop merge, streamcut fire, repdet rewrite), expose under `admin.output_reduce.outstop_*`, `admin.output_reduce.streamcut_*`, `admin.output_reduce.repdet_*` in the JSON status payload.

## Target State

1. New `OutputReduceCounters` struct on `Proxy` with atomic.Uint64 fields:
   - `StopSeqRequestsModified`, `StopSeqPhrasesAdded`
   - `StreamcutFired`, `StreamcutBytesObserved`
   - `RepdetMatchesRewritten`, `RepdetBytesSaved`, `RepdetResponsesRewritten`
2. Increment sites: handler.go step 8.7 (outstop), streamingRelayWithCutter (streamcut), passthroughAnthropicWithRepdet + future OpenAI variant (repdet).
3. Snapshot surface: `Proxy.outputReduceCountersSnapshot() OutputReduceTelemetry` returning a value-type read.
4. `admin.go` /status handler includes `"output_reduce": {…}` under the existing status JSON with the snapshot fields.
5. CLI: `slimference status` (or whatever the existing surface is) shows a one-line summary.

## Acceptance

- After a request with outstop injection: counter increment visible on /admin/status.
- After a streamcut fire: streamcut counter +1.
- After a repdet rewrite: matches counter goes up; bytes_saved reflects the sum of replaced span lengths.
- Counters are atomic-safe under concurrent requests (race detector clean).
- JSON shape stable, documented in docs/output-reduce.md.
- 100% coverage on the snapshot accessor.

## Sub-Tasks

- [ ] Counter struct + increment helpers.
- [ ] Increment site in handler.go (outstop).
- [ ] Increment site in streamingRelayWithCutter (streamcut).
- [ ] Increment sites in passthroughAnthropicWithRepdet (repdet).
- [ ] Snapshot accessor.
- [ ] Admin JSON field.
- [ ] Race-detector test under -race.
- [ ] Doc update in output-reduce.md "Measurement" section.

## Notes

- Counters are monotonic-only (no resets), matching the pattern of `analytics_drop_count` etc.
- Per-provider breakdown is a follow-up (would need maps - keep v1 single-counter to start).
- A future T169 quality A/B harness reuses these counters as the "treatment cohort emitted X" signal.

## Deviations

(none yet)
