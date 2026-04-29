# TASK 105: Default-on token-estimator self-calibration

Status: todo
Priority: P2
Scope: `internal/tokens/`, `internal/proxy/handler.go`, `internal/analytics/`
Driver: T28 introduced per-provider tokenizers and self-calibration from `usage` headers. It is opt-in. Default-on with a conservative learning rate makes every later layer's budgets more accurate.

---

## Problem

`internal/tokens/` uses `len/4` for fast estimation on the hot path. It is OK for ranking but always wrong by some margin: code is denser, prose is sparser. T28 added self-calibration that consumes upstream `usage` headers (Anthropic exposes them, OpenAI Responses partially) but the wiring is opt-in and only Anthropic is fully exercised.

## Target State

Default-on calibration across all upstream providers that expose token usage (Anthropic, OpenAI Responses, OpenAI Chat Completions, ChatGPT-Backend where exposed):

- Calibration runs on every successful response that carries usage.
- Per-provider EMA (exponential moving average) updates the multiplier.
- Defaults: alpha=0.1, clamp multiplier in [0.5, 2.0].
- New providers without usage default to multiplier 1.0 with a one-line slog warn after N requests so the operator notices.

## Implementation Plan

### WP1 - Calibration core
- Move T28 logic into a small `calibrator.go` with explicit per-provider state.

### WP2 - Default-on switch
- `[tokens] calibration = "auto"` becomes default; previous opt-in flag still respected.

### WP3 - Fallback logic for providers without usage
- Counter `tokens_calibration_no_usage_total` per provider; rate-limited warn.

### WP4 - Tests
- Synthetic upstream responses with varying usage; assert multiplier converges.

## Acceptance Criteria

- [ ] All exposed providers self-calibrate by default.
- [ ] Multiplier is bounded; no runaway adjustment from outliers.
- [ ] No-usage providers warn but do not break.
- [ ] Counter exposed in `/admin/status.tokens`.
- [ ] Coverage 100%.

## Out of Scope

- Per-message-type calibration (one multiplier per provider is enough).
- BPE tokenizer integration (fast-path stays heuristic).

## Validation

```
go test ./internal/tokens/...
```
