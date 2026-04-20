# T54 - `min_tokens_for_layer2` Revaluation + Datenbasis

Status: todo
Priority: P2
Scope: `internal/summarization/`, `internal/config/`, `docs/benchmarks.md`, `docs/tuning-inventory.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`min_tokens_for_layer2 = 30000` is set conservatively: Layer 2 (MiniMax
summarisation) skips if the candidate context is below 30 k tokens. The
reasoning was "MiniMax call latency + cost only pays off on large
contexts". No benchmark confirmed or refuted that.

Hypothesis (from `docs/savings-assessment.md`):

- 15-30 k range: ~10-15 % additional token savings are achievable if L2
  is engaged, at the cost of ~300-800 ms extra latency per request.
- 10-15 k range: marginal savings; latency cost dominates.
- < 10 k: L2 rarely pays off.

Right answer: lower default to 15 k, add a **latency budget** guard so
L2 is skipped when the extra latency would exceed a configurable SLA
headroom, not a fixed token count.

## Current State

- Constant `min_tokens_for_layer2 = 30000` in config defaults.
- No latency-budget awareness.
- No benchmark comparing threshold variants.

## Target State

- Default lowered to 15 000 tokens.
- New `layer2_latency_budget_ms = 500` - if projected MiniMax latency
  for the current context exceeds remaining budget, skip L2 and use L1
  only. Projection uses an EMA of observed MiniMax latencies.
- Evidence documented in `docs/benchmarks.md` - A/B results across
  15 k, 20 k, 30 k thresholds.
- TUI Stats shows "L2 Skips (budget): <n>" and "L2 Skips (threshold):
  <n>" per session.

## Design

### Config

`[summarization]`:

| Field | Type | Default | Semantic |
|-------|------|---------|----------|
| `min_tokens_for_layer2`      | int | 15000 | lowered from 30000 |
| `layer2_latency_budget_ms`   | int | 500   | skip if projection exceeds |
| `layer2_latency_ema_alpha`   | float | 0.2 | EMA weight |
| `layer2_latency_projection_multiplier` | float | 1.2 | safety buffer |

ENV: `SLIMFERENCE_MIN_TOKENS_FOR_LAYER2`,
`SLIMFERENCE_LAYER2_LATENCY_BUDGET_MS`.

### Latency EMA

`internal/summarization/latency_estimator.go`:

```go
type Estimator struct {
    alpha float64
    ema   atomic.Uint64 // millis
}

func (e *Estimator) Observe(ms int64) {
    cur := float64(e.ema.Load())
    next := e.alpha*float64(ms) + (1-e.alpha)*cur
    e.ema.Store(uint64(next))
}

func (e *Estimator) Projected() time.Duration {
    return time.Duration(e.ema.Load()) * time.Millisecond
}
```

### Decision rule

```go
func shouldRunLayer2(tokens int, est Estimator, cfg Config) (bool, string) {
    if tokens < cfg.MinTokensForLayer2 {
        return false, "below_threshold"
    }
    projected := est.Projected() * cfg.LatencyProjectionMultiplier
    if projected > cfg.LatencyBudgetMs {
        return false, "latency_budget"
    }
    return true, "run"
}
```

### Metrics

Analytics counters:
- `l2_skipped_threshold_total`
- `l2_skipped_budget_total`
- `l2_ran_total`
- `l2_projected_ms` (current EMA)
- `l2_observed_p50/p95_ms` (rolling window)

TUI Stats row: `Layer 2: 78 ran, 12 skipped (thr), 3 skipped (budget)`.

### Benchmark evidence

`scripts/benchmarks/layer2-threshold-ab.ts` runs a fixture corpus with
thresholds {10k, 15k, 20k, 30k} and reports:
- token savings
- additional latency p50 / p95
- net "useful work per second" metric

Results appended to `docs/benchmarks.md`.

## Implementation Plan

### WP1 - Config + defaults.

### WP2 - Latency estimator.

### WP3 - Decision rule in `layer2.go`.

### WP4 - Metrics + TUI surface.

### WP5 - Benchmark script + docs/benchmarks.md append.

### WP6 - Tuning-inventory doc entry.

---

## Subtasks

- [ ] Config fields + ENV overrides.
- [ ] `latency_estimator.go` with unit tests.
- [ ] Decision rule + reason string.
- [ ] Analytics counters + EMA field.
- [ ] TUI Stats rendering.
- [ ] `scripts/benchmarks/layer2-threshold-ab.ts`.
- [ ] Append results to `docs/benchmarks.md`.
- [ ] Update `docs/tuning-inventory.md`.

## Risks

- Latency EMA biases low at cold start → L2 runs aggressively, bumping
  p95 latency. Mitigation: seed EMA with 400 ms (conservative) until
  first observation.
- Lowering threshold increases MiniMax API cost. Budget guard limits
  damage, but document the cost impact in `docs/tuning-inventory.md`.

## Acceptance Criteria

- [ ] Default `min_tokens_for_layer2 = 15000`.
- [ ] Budget guard skips L2 when projection > budget.
- [ ] Analytics exposes both skip reasons.
- [ ] Benchmark script produces A/B table in `docs/benchmarks.md`.
- [ ] `go test -race ./internal/summarization/...` green.

## Out of Scope

- Adaptive budget per session based on user-set latency SLA.
- Multi-tier summarisation providers.

---

## Validation

```
go test -race ./internal/summarization/...
bun run scripts/benchmarks/layer2-threshold-ab.ts
curl -s 127.0.0.1:8990/admin/status | jq .layer2
```
