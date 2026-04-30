# TASK 99: Layer 2 mid-exchange summarization

Status: SHIPPED 2026-04-30 (deterministic stub, default off). Live MiniMax-driven summary path tracked as T99b.
Priority: P2
Scope: `internal/summarization/`, `internal/compression/exchange_window.go`, `internal/proxy/handler.go`
Driver: Sliding-window granularity is per-exchange. If the current in-flight exchange already exceeds budget (e.g. a 20k-token tool result), Layer 2 cannot help because its window does not cover in-progress exchanges. Mid-exchange summarisation with a "still in progress" marker covers the gap.

---

## Problem

Today the L2 window covers user-completed exchanges. A long, single-exchange burst (large tool output, multi-turn assistant reasoning) cannot be compressed by L2 because L2 only summarises closed exchanges. The result is a budget cliff that L1 has to handle alone.

## Target State

Layer 2 gains a "mid-exchange" mode that summarises content within the in-flight exchange:

- Triggered when the in-flight exchange exceeds `[summarization] mid_exchange_threshold_tokens` (default 10k).
- Summary is tagged `[in-progress, anchor=msg-N]` so the model knows the source content is incomplete.
- On exchange completion the summary is replaced by the standard end-of-exchange summary.
- The original in-progress content goes through T76 archive.

## Implementation Plan

### WP1 - Trigger detection
- Token budget watcher per request; when in-flight exchange tokens exceed threshold, mark the exchange for mid-summary.

### WP2 - In-progress prompt template
- A new prompt template variant that produces "in-progress" marker bullets.
- T86 prompt store registers it as an additional template.

### WP3 - Replacement on completion
- When the exchange closes, swap mid-summary for the final summary; archive both for T76.

### WP4 - Tests
- Long-tool-output fixture; assert mid-summary fires and is replaced cleanly on completion.

## Acceptance Criteria

- [x] Long in-flight exchanges produce mid-summary entries when over the threshold.
- [x] Marker reflects the in-progress nature so the model interprets correctly (`[in-progress summary, anchor=msg #N]`).
- [x] Coverage 100%; race tests green.
- [ ] **Tracked as T99b**: Live MiniMax-driven summary content (currently the stub emits "completed steps summarized" plus an anchor; a real summary needs a MiniMax round-trip wired through `summarization.Layer2` rather than the local `ApplyMidExchange` shortcut).
- [ ] **Tracked as T99c**: Replacement on exchange completion does not double-charge. Stub today simply runs again on the next request and re-collapses; needs an idempotency check that recognises an already-collapsed range.

## Out of Scope

- Streaming summarisation (one shot at threshold; not chunked).

## Validation

```
go test ./internal/summarization/... ./internal/compression/...
```
