# TASK 150: L3 stable-prefix cache planner

Status: DONE (completed 2026-05-15)
Priority: P0
Parent: T145
Scope: `internal/proxy/openai_prompt_cache.go`, `internal/proxy/handler.go`, `internal/types/provider_caps.go`, `internal/analytics/`, `cmd/slimference/gain_cmd.go`, `docs/documentation.md`

## Why

Provider-native cache/state reuse is the cleanest large saving lever because it does not alter model-visible content. The current OpenAI prompt-cache hint path can attach `prompt_cache_key` and retention, but the key is mostly session-shaped and the min-token gate is based on the whole request, not the actually stable prefix. That can waste hint attempts on one-turn requests and can keep a cache key stable even after system/developer/tool-schema content changes.

## Target State

Layer 3 owns a stable-prefix cache plan before request dispatch:

1. Identify provider-supported reuse levers for the current provider/model.
2. Segment request content into stable prefix and volatile latest turn.
3. Hash only stable, non-secret request structure into prompt-cache keys.
4. Rotate keys when stable prefix, tool schema, model, provider, or session class changes.
5. Gate prompt-cache hints on stable-prefix token count, not total request tokens.
6. Preserve caller-owned cache fields.
7. Retry without hints on provider rejection.
8. Report only provider-reported cache savings as billing-equivalent savings.

## Implementation Plan

### WP1 - Stable prefix detector

- Parse OpenAI/Codex-compatible `messages` and Responses `input` arrays.
- Treat all items before the final user turn as stable prefix.
- Include top-level stable fields such as `instructions`, `system`, `developer`, and `tools` in the stable hash.
- Exclude latest user turn and volatile response parameters from the key.
- Fail closed: invalid JSON or unknown body shape returns no plan and no mutation.

### WP2 - Prompt-cache key planner

- Extend OpenAI prompt-cache key construction with stable-prefix hash.
- Keep `static` strategy exactly caller-controlled.
- Preserve existing `session` and `model_session` strategies but rotate them by stable-prefix hash when available.
- Never place raw user text, paths, or secrets into the key.

### WP3 - Hint gating

- Use stable-prefix estimated tokens when the stable prefix is detected.
- Fall back to full input token gate only when no stable-prefix shape is available.
- Return explicit decision reasons: `stable_prefix_below_min_tokens`, `no_stable_prefix`, `caller_owned`, `rate_limited`, `applied`.

### WP4 - Telemetry

- Add decision fields for stable-prefix tokens/hash where the existing flight/debug surfaces can carry them without logging raw content.
- Keep `previous_response_id` separate from billable savings unless provider usage proves cache read savings.

### WP5 - Tests

- One-turn request does not receive prompt-cache hints just because total tokens are high.
- Multi-turn stable prefix receives hints and key rotates when the stable prefix changes.
- Latest user turn changes do not rotate the key.
- Tool schema change rotates the key.
- Caller-owned prompt-cache fields remain untouched.
- Invalid JSON and unknown shapes pass through unchanged.

## Acceptance

- [x] OpenAI prompt-cache hints are stable-prefix gated.
- [x] Prompt-cache keys rotate on stable prefix/tool-schema/session changes. Model rotation is preserved for `model_session` strategy.
- [x] Latest user text changes do not rotate keys.
- [x] No raw user content appears in generated keys.
- [x] Existing rejection retry remains intact.
- [x] Provider-reported cache accounting remains the only billable saving source.
- [x] `go test ./...` passes.

## Implementation Notes

- `internal/proxy/openai_prompt_cache.go` now builds a stable-prefix plan before injecting hints.
- Stable prefix includes top-level `instructions`, `system`, `developer`, `tools`, and every `messages` / Responses `input` item before the final user turn.
- Prompt-cache keys include a stable-prefix hash for `session` and `model_session` strategies; `static` remains caller-controlled.
- One-turn requests without stable prefix return `no_stable_prefix` and are not mutated.
- `internal/debug` flight records now expose content-free prompt-cache hint telemetry: applied/reason, stable-prefix hash, stable-prefix tokens, key-set flag, and retention.
- Tests cover stable-prefix gating, latest-turn non-rotation, stable-prefix/tool-schema rotation, caller-owned fields, rejection retry, and flight telemetry.

## Non-Goals

- No repo-onboarding capsule.
- No WebSocket response-id parsing before T142 shape proof.
- No claim that `previous_response_id` alone saves billable tokens.
- No provider-unsupported cache fields on unknown providers.
