# T28 - Per-Provider Tokenizer (Opus 4.7 Aware, Calibrated Fallback)

Status: done
Priority: high
Scope: internal/tokens, internal/compression, internal/summarization,
       internal/proxy (usage reconciliation)

---

## Problem

Token accounting across the proxy uses a single estimator:

- `pkoukk/tiktoken-go` for some paths (OpenAI BPE-aligned).
- `len(text)/4` heuristic in hot paths that need speed.
- A word-based estimator in `internal/summarization`.

Anthropic models (Claude 3.x, 4.x, now **Opus 4.7**) do not use GPT-BPE.
Opus 4.7 in particular introduced a new tokenizer. That means every threshold
that gates "is this message worth compressing" is off by a provider-specific
factor. Over-estimating wastes compression cycles; under-estimating risks
letting oversized requests through.

---

## Desired End State

Central tokenizer interface with per-provider implementations:

```go
type Tokenizer interface {
    CountString(s string) int
    CountMessages(msgs []types.Message) int
    Name() string
}
```

- `AnthropicTokenizer`: ideally a real port once Anthropic publishes an
  official one; interim, a calibrated heuristic.
- `OpenAITokenizer`: tiktoken (cl100k_base / o200k_base by model).
- `UniversalFallback`: calibrated heuristic with known Anthropic/OpenAI
  deltas measured against real usage responses.

Calibration happens offline: record `usage.input_tokens` from real responses,
compare against our estimate, adjust the Anthropic and OpenAI coefficients,
ship the numbers in config.

---

## Work Packages

### WP1 - Interface and current coverage

- Define `Tokenizer` interface in `internal/tokens`.
- Refactor `tokens.CountMessages` to dispatch per provider via interface.
- Pass the active tokenizer down through the compression pipeline.

### WP2 - OpenAI path (low risk)

- Keep tiktoken-go, select `cl100k_base` vs `o200k_base` by model string.

### WP3 - Anthropic path

- Research: is there a public Anthropic tokenizer for Opus 4.7? Check
  upstream repos, official SDK, published artefacts.
- If yes: port or bind. If no: continue with a calibrated heuristic, see WP4.

### WP4 - Calibration harness

- Under `scripts/utils/calibrate-tokens.ts`: run a small recorded session
  through the proxy, compare our estimate to Anthropic's `usage.input_tokens`
  field, emit a calibration coefficient per model family.
- Output: `docs/tokenizer-calibration.md` with measured deltas.
- Update fallback coefficients accordingly.

### WP5 - Usage-based self-correction

- When the proxy sees `usage.input_tokens` in a response, compare against
  our pre-send estimate, log the delta to analytics for continuous
  calibration.
- Optional knob `[tokens.self_calibrate]` to auto-adjust in a narrow band.

### WP6 - Tests

- Unit tests per tokenizer.
- Regression: old fixtures must still count within +-5 % of previous values
  to avoid silent tuning shifts.
- Integration: end-to-end proxy test verifies deltas within tolerance.

---

## Subtasks

- [x] Design `Tokenizer` interface and wire call sites.
- [x] Implement OpenAI per-model selector.
- [x] Research Opus 4.7 tokenizer availability; port or calibrate.
- [x] Build calibration harness under `scripts/utils/`.
- [x] Implement usage-based self-calibration loop.
- [x] Tests and docs.

## Acceptance Criteria

- Pre-send token estimates for Anthropic Opus 4.7 are within +-5 % of
  Anthropic's reported `usage.input_tokens` on benchmark sessions.
- OpenAI counts unchanged or more accurate.
- No silent regressions on existing fixtures.
- Coverage stays at 100 %.
