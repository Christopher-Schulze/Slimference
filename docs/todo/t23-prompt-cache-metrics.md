# T23 - Prompt-Cache Live Metrics and `stats prompt-cache`

Status: done
Priority: high
Scope: internal/analytics, internal/proxy, internal/tui, cmd/slimference

---

## Problem

Anthropic's prompt caching grants a ~90 % read-discount on cached prefixes.
The proxy already injects cache breakpoints via `OptimizeCacheBreakpoints`
(handler.go:134), but we never **measure** whether upstream honors them. We
cannot tell if:

- breakpoints are placed on stable content (which they should be after L1's
  deterministic compression).
- the Anthropic response actually hit the cache.
- the effective savings match the spec claim.

This is one of the highest-ROI measurement gaps: prompt-caching is easily the
single largest token-cost lever in production. Not measuring it means we are
flying partially blind on savings.

---

## Desired End State

- Every upstream response exposes its `cache_creation_input_tokens` and
  `cache_read_input_tokens` (Anthropic) into analytics.
- `slimference stats prompt-cache` CLI command prints a report: hit rate,
  cached tokens (read), newly cached tokens (create), net savings.
- TUI has a dedicated panel showing rolling prompt-cache effectiveness.
- Debug log entries include the cache fields per request.

---

## Work Packages

### WP1 - Parse upstream usage fields

- In `streamingRelay` and `passthrough`: parse the final `message_stop` /
  usage block for Anthropic; ignore on OpenAI for now.
- Extract `cache_creation_input_tokens`, `cache_read_input_tokens`,
  `input_tokens`, `output_tokens`.

### WP2 - Analytics event extension

- Add fields to `types.AnalyticsEvent`:
  - `CacheReadTokens int`
  - `CacheCreateTokens int`
- Populate from WP1.

### WP3 - CLI command

- `slimference stats prompt-cache [today|week|month|all]` with `--json` /
  `--csv`.
- Report: total requests, requests with cache_read > 0, % hit rate,
  sum(cache_read), sum(cache_create), estimated $ savings (reuse existing
  USD rate config from `slimference gain`).

### WP4 - TUI panel

- New view segment: prompt-cache hit-rate gauge + absolute saved tokens.
- Uses the same analytics stream as the existing request feed.

### WP5 - Debug enrichment

- `dbg.RequestSummary` gains the two cache fields.
- JSONL debug stream exposes them for offline analysis.

---

## Subtasks

- [x] Parse Anthropic usage fields in passthrough + streaming.
- [x] Extend analytics event + storage.
- [x] Implement `stats prompt-cache` CLI.
- [x] Add TUI panel with live gauge.
- [x] Extend debug summary + JSONL fields.
- [x] Tests cover: response with cache_read, without, malformed body.

## Acceptance Criteria

- A real Claude session shows concrete cache hit/read numbers in TUI and CLI.
- The numbers line up (within tolerance) with Anthropic's own accounting.
- Coverage stays at 100 %.
