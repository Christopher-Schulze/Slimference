# TASK 77: Quality calibration loop

Status: todo
Priority: P0
Scope: `internal/analytics/`, `internal/proxy/handler.go`, `internal/debug/`, `internal/tui/`, `cmd/slimference/`
Driver: Slimference measures token savings, not output quality. Today nothing detects "compression hurts": a session where the model has to re-read the same file three times pays back the savings and then some. Without a quality signal, every tuning decision is guesswork.

---

## Problem

The current analytics pipeline tracks compression ratio, layer attribution, prompt-cache hits, and provider counts. It does not track whether the *model's behaviour after compression* indicates the compression was too aggressive. There is no signal that says "this session compresses well and answers well" vs "this session compresses well and answers worse".

This makes every tuning call (dedup threshold, structure-preview budget, MiniMax target tokens, sliding-window size) speculative. T76 archive layer is necessary but not sufficient on its own; without measurement, defaults stay conservative forever.

## Target State

Three lightweight quality signals run alongside existing analytics, none of them require an LLM round-trip:

1. **Re-read count per session**: counter increments when the same file path / tool key is read twice within K turns. Healthy sessions read once, edit, move on.
2. **Prompt-cache miss-spike alarm**: when prompt-cache hit ratio drops > X% over a rolling window after a compression-config change, log and surface in `/admin/status.quality`.
3. **Compression-vs-cache trade-off**: per-session ratio of `tokens_saved_by_compression` vs `tokens_lost_by_cache_invalidation`. Negative ratio = compression is destabilizing the cache prefix.

Each signal is exposed as a counter, surfaced in `/admin/status.quality`, rendered in the TUI Quality view, and persisted in `analytics.db` so `slimference savings` (T80) can show "savings net of quality cost".

## Implementation Plan

### WP1 - Re-read detector
- New `internal/quality/reread.go`: in-memory map per session of `tool_key -> last_seen_turn`.
- Increment `re_read_count` when the same key appears within `[quality] reread_window_turns` (default 10).
- Persist to `analytics.db` per-session column.

### WP2 - Cache-miss spike detector
- Use existing prompt-cache analytics as input (T23).
- Rolling window (default 50 requests) computes hit ratio; alarm when ratio drops > `[quality] cache_miss_spike_threshold` (default 25%) compared to baseline.
- Surface via admin endpoint with rate-limited slog warn.

### WP3 - Net-savings field
- Compute `net_saved_tokens = saved_tokens - estimated_cache_invalidation_cost` per request.
- Surface in `RequestSummary`.

### WP4 - TUI Quality view
- New "Quality" tab in `internal/tui/views.go` with re-read rate, cache-miss-spike state, net savings.

### WP5 - CLI
- `slimference quality today|week|month` mirrors `slimference savings` output structure.
- `slimference quality --json` for scripting.

## Acceptance Criteria

- [ ] `RequestSummary` carries `re_read_count` and `net_saved_tokens` fields.
- [ ] `/admin/status.quality` exposes `reread_rate`, `cache_miss_spike_active`, `net_savings_ratio`.
- [ ] TUI Quality view renders all three signals.
- [ ] `slimference quality --json` returns the same data.
- [ ] Hooks: changing `dedup` threshold in config and re-running an integration session shows the spike detector reacting.
- [ ] `go run ./scripts/ci` PASS, coverage 100%.

## Out of Scope

- LLM-based quality judging (separate research track).
- Cross-session learning / auto-tuning (T-future).

## Validation

```
go test ./internal/quality/... ./internal/proxy/...
slimference quality today
curl localhost:8990/admin/status | jq .quality
```
