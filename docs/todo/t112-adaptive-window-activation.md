# TASK 112: Adaptive sliding window - hot-path activation + measurement

Status: DONE 2026-05-01 (audit-driven mitigation 2026-04-30)
Priority: P1
Scope: `internal/summarization/adaptive_window.go`, `internal/compression/exchange_window.go`, `internal/compression/layer1.go`, `internal/proxy/handler.go`, `internal/config/`
Driver: `AdaptiveWindowSize` exists in `internal/summarization/adaptive_window.go` and computes a complexity score over (unique file paths, anchor density, tool diversity) - solid logic, fully unit-tested. **Zero production code calls it.** The hot path uses the static `cfg.SlidingWindow = 5` everywhere via `CompressiblePrefixEnd`. This is dead code by audit, dead value by reality. Either wire it up so it earns its keep, or delete it. This task wires it up because the underlying signal (a high-complexity exchange deserves a smaller compressible boundary, a low-complexity one a larger boundary) is sound and its absence is a missed Layer 2 lever.

---

## Problem

`adaptive_window.go:16-39`:
```go
func AdaptiveWindowSize(messages []types.Message, defaultWindow int) int {
    score := computeComplexityScore(messages)
    switch {
    case score >= ComplexityHigh: return min(defaultWindow, 3)
    case score >= ComplexityMedium: return defaultWindow
    default: return defaultWindow + 2
    }
}
```

`grep -r AdaptiveWindowSize internal/ cmd/`:
- `internal/summarization/adaptive_window.go` (definition)
- `internal/summarization/adaptive_window_test.go` (tests against the function in isolation)
- nothing else.

`internal/compression/exchange_window.go::CompressiblePrefixEnd` always receives `cfg.SlidingWindow` (a config-fixed integer). The dynamic decision is never made.

## Target State

Three deliverables:

1. **Wire-in**: handler.go computes the adaptive window once per request, passes the resulting integer to `CompressiblePrefixEnd` and to the L2 path. The static `cfg.SlidingWindow` becomes the **default / floor** value that adaptive sizing tunes around.
2. **Telemetry**: chosen window size, complexity score, decision reason recorded per request; histogram exposed at `/admin/status.adaptive_window`.
3. **Safety**: capped between `[compression.tuning] adaptive_window_min` (default 3) and `..._max` (default 12); A/B-toggleable via `[compression.tuning] adaptive_window_enabled` (default OFF until soak data confirms). Off behaviour byte-equal to today.

When ON, the proxy compresses more aggressively on simple "ls / grep / cat" tail exchanges (window grows -> larger compressible prefix -> more Layer 1 + L2 work earlier) and less aggressively on dense edit/error exchanges (window shrinks -> smaller compressible prefix -> recent context preserved at full fidelity).

## Implementation Plan

### WP1 - Score normalisation
- `AdaptiveWindowSize` API extension: return both the chosen size and a `WindowDecision{Score float64, Reason string, Min, Max int}` for telemetry.
- Validate the existing scoring against fixtures; consider tuning weights if benchmark data exposes weakness (deferred to WP6).

### WP2 - Hot-path integration
- `Proxy.handleCompressibleRequest`: before the L1 call, compute `windowDecision := summarization.ResolveWindow(cfg, messages)`.
- Pass `windowDecision.Size` everywhere `cfg.SlidingWindow` is used in this request's scope:
  - `compression.CompressiblePrefixEnd(messages, windowDecision.Size)` for L1 prefix.
  - Similar override path for L2 trigger checks (`Layer2.ShouldTriggerCompression`).
  - Prompt-cache breakpoint placement uses the adaptive boundary.

### WP3 - Config knobs
- New `[compression.tuning] adaptive_window_enabled = false`, `adaptive_window_min = 3`, `adaptive_window_max = 12` in `config/defaults.go`.
- The static `[compression] sliding_window = 5` remains the floor / default when adaptive is off OR when the score is right in the middle.

### WP4 - Telemetry
- `RequestSummary.AdaptiveWindow{Size int, Score float64, Reason string}` field.
- `/admin/status.adaptive_window.{requests, avg_size, p50_size, p95_size, distribution[]}`.
- `slimference debug last` prints the chosen window for the most recent request.

### WP5 - Tests
- Three fixture exchanges: low / medium / high complexity. Assert window sizes 7 / 5 / 3 respectively.
- Off-flag invariant: with `adaptive_window_enabled=false`, the window equals the static config value byte-for-byte against pre-task baseline.
- Bounds enforcement: synthetic high score gets clamped to `adaptive_window_min`.
- Race test: two parallel requests pick independent window sizes (no shared state mutation).

### WP6 - Soak measurement (deferred to T112b)
- Once the flag is shipped, run a soak window (1-2 weeks of real traffic) with `adaptive_window_enabled=true` on a non-prod proxy.
- Compare savings ratio + quality-loss signals (T77) vs static-window baseline.
- Decision: tune weights, default-on, default-off-permanent, or revert.

## Acceptance Criteria

- [ ] `adaptive_window_enabled=false` is byte-equal to today's behaviour.
- [ ] `adaptive_window_enabled=true` causes the chosen window to vary based on the exchange complexity score.
- [ ] Bounds (`min` <= size <= `max`) enforced.
- [ ] Telemetry surfaces size + score + reason per request.
- [ ] Coverage 100%; race tests green.
- [ ] Soak verification tracked as T112b.

## Out of Scope

- Re-training the complexity weights from real corpora (T112b, after live data exists).
- Per-provider window tuning (would re-introduce capability fragmentation; orthogonal concern).
- Adaptive window for L0 filter pipeline (different concept).

## Validation

```
go test -race ./internal/summarization/... ./internal/compression/... ./internal/proxy/...
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
```
