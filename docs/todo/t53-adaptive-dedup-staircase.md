# T53 - Adaptive Dedup-Similarity-Staircase

Status: todo
Priority: P2
Scope: `internal/compression/dedup_minhash.go`, `internal/config/`, `internal/analytics/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

MinHash/LSH dedup uses a hard-coded Jaccard threshold
`dedup_similarity_threshold = 0.85` at `internal/compression/dedup_minhash.go`.
Long sessions (>30 exchanges) have measurably more near-duplicate tool
output (npm installs, build logs, `ls -la`, test runs) with 0.78-0.85
Jaccard. The conservative 0.85 misses those.

Evidence from `docs/benchmarks.md`:

- Sessions with 30-50 messages: ~8 % additional dedup opportunity at
  0.80 threshold vs 0.85, with < 0.5 % false-collapse rate.
- At 0.75: ~14 % more dedup but false-collapse rate climbs to ~3 %.

Right answer: **adaptive staircase** that lowers threshold as the session
grows, bounded at 0.78.

## Current State

- Single constant threshold 0.85.
- No analytics on near-miss cases (pairs that had Jaccard 0.78-0.84 and
  were not collapsed).

## Target State

Staircase rule (default, configurable):

| Exchanges | Threshold |
|-----------|-----------|
| 0-10      | 0.88 (tighter to avoid early-session false collapse) |
| 11-20     | 0.85 (current default) |
| 21-40     | 0.82 |
| 41+       | 0.78 (floor) |

Config-overridable. Per-session telemetry:
`slim_dedup_near_miss_total` (pairs with Jaccard in [floor, cur-threshold)
that were not collapsed) so the staircase can be tuned empirically.

## Design

### Config

`[compression.tuning.dedup_staircase]`:

```toml
enabled = true
steps = [
  { max_exchanges = 10, threshold = 0.88 },
  { max_exchanges = 20, threshold = 0.85 },
  { max_exchanges = 40, threshold = 0.82 },
  { max_exchanges = 0,  threshold = 0.78 },  # 0 = infinity / floor
]
min_threshold = 0.70
max_threshold = 0.95
```

ENV override: `SLIMFERENCE_DEDUP_STAIRCASE_ENABLED=false` for hard
rollback. Also single-value fallback
`SLIMFERENCE_DEDUP_SIMILARITY_THRESHOLD=0.85` that overrides the whole
staircase.

### Exchange-count source

`internal/sessions` already tracks exchange count per session.
Dedup pipeline receives it via request context (injected in handler).

### Near-miss telemetry

In `dedup_minhash.go::compareCandidates`:

```go
if jaccard < threshold && jaccard >= cfg.MinThreshold {
    analytics.Inc("dedup_near_miss", 1)
    analytics.Observe("dedup_near_miss_jaccard", jaccard)
}
```

Histogram in analytics snapshot: bucket edges [0.70, 0.75, 0.78, 0.80,
0.82, 0.85, 0.88, 0.90, 0.95].

### Safety rails

- Hard min 0.70 (can never go below).
- Hard max 0.95 (can never go above, to avoid no-dedup trivial case).
- If `enabled = false`, fall back to legacy single threshold.

## Implementation Plan

### WP1 - Config surface
- TOML + defaults + ENV overrides + validation.

### WP2 - Threshold resolver
- Function `thresholdForExchange(count int, cfg Staircase) float64`.
- Unit test every boundary.

### WP3 - Pipeline wiring
- Plumb exchange count from handler into dedup pipeline.

### WP4 - Near-miss telemetry
- Counter + histogram in analytics.

### WP5 - A/B bench
- `scripts/benchmarks/dedup-staircase-ab.ts` running a fixture corpus
  with staircase on vs off, report token delta.

### WP6 - Safety test
- Golden fixtures with known duplicate pairs at 0.83 Jaccard: with
  staircase default, exchange>20 → collapsed; exchange<20 → not
  collapsed.

---

## Subtasks

- [ ] Config struct + defaults + ENV overrides.
- [ ] `thresholdForExchange` resolver with unit tests.
- [ ] Plumb exchange count through handler → compression pipeline.
- [ ] Near-miss counter + histogram.
- [ ] A/B benchmark script.
- [ ] Golden-fixture regression tests.
- [ ] Docs: `docs/tuning-inventory.md` entry + `docs/documentation.md`
      §5.3.

## Risks

- False-collapse of legitimately different-but-similar outputs (two
  test runs that happen to share 80 % of lines but differ in the one
  line that matters). Mitigation: preserve existing "never dedup across
  tool_use IDs" guard, only trigger staircase within the same
  tool-result stream / session-role scope.
- Analytics cost: histogram every comparison is cheap (atomic add)
  but rendering in TUI must be throttled.

## Acceptance Criteria

- [ ] Staircase resolver boundary tests green.
- [ ] Near-miss counter non-zero on long-session fixture.
- [ ] Token savings on 30-exchange fixture ≥ +5 % vs legacy.
- [ ] `SLIMFERENCE_DEDUP_STAIRCASE_ENABLED=false` preserves legacy.
- [ ] `go test -race ./internal/compression/...` green.

## Out of Scope

- Adaptive shingle size.
- Per-tool-type thresholds (separate future TASK if evidence supports).

---

## Validation

```
go test -race ./internal/compression/...
bun run scripts/benchmarks/dedup-staircase-ab.ts
curl -s 127.0.0.1:8990/admin/status | jq .dedup
```
