# TASK 145: Layer 3 provider-cache and state-reuse maximizer

Status: PENDING (planned 2026-05-13)
Priority: P0
Scope: `internal/caching/`, `internal/proxy/handler.go`, `internal/proxy/streaming.go`, `internal/provider/`, `internal/sessions/`, `internal/flight/`, `cmd/slimference/gain_cmd.go`, `cmd/slimference/cache_cmd.go`, `docs/savings-assessment.md`.

## Why

Layer 3 is not only telemetry. It is the place where provider-native reuse can eliminate billing for stable prefixes without changing content. The current implementation parses provider cached-token usage and has safe local prompt-cache hints, but it does not yet act like an aggressive cache planner across Codex/OpenAI/Anthropic request shapes.

This task maximizes provider-supported reuse while keeping savings claims honest: only provider-reported `cached_tokens` / cache read/create fields count as billing savings.

## Target State

Layer 3 becomes a provider-capability optimizer:

1. Detect stable prefixes.
2. Place provider cache hints/breakpoints where they are most likely to hit.
3. Use `prompt_cache_key` / retention when supported and safe.
4. Preserve and reuse `previous_response_id` where Codex/OpenAI semantics allow it.
5. Track cache heat and misses.
6. Retry without hints on provider rejection.
7. Report only real provider usage.

## Work Packages

### WP1 - Provider capability matrix refresh

- Codex/OpenAI:
  - `cached_tokens` in `usage.prompt_tokens_details`.
  - `prompt_cache_key` when endpoint/model supports it.
  - `prompt_cache_retention` when endpoint/model supports it.
  - `previous_response_id` / server-state chaining where semantically valid.
- Anthropic:
  - cache-control block placement.
  - cache read/create input tokens.
  - TTL limits.
  - breakpoint count caps.
- Unknown provider:
  - no cache hints unless explicitly configured.

### WP2 - Stable prefix planner

- Compute prefix segments:
  - system/developer instructions.
  - tool definitions.
  - repository rules.
  - old conversation history.
  - stable summaries.
  - latest volatile turn.
- Pick cache boundaries based on:
  - segment size.
  - expected reuse.
  - provider caps.
  - TTL.
  - mutation risk.
  - T149 planner output.

### WP3 - Prompt-cache key strategy

- Generate deterministic keys from:
  - normalized system/developer prompt hash.
  - tool schema hash.
  - workspace/session class.
  - provider/model.
- Never include secrets or raw user content in keys.
- Rotate keys when:
  - AGENTS/spec/config changes.
  - tool schema changes.
  - model changes.
  - provider rejects key.

### WP4 - Previous-response state owner

- Centralize `previous_response_id` ownership in session state.
- Record:
  - last response ID.
  - request hash.
  - provider/model.
  - invalidation reason.
- Use only when the provider contract says the server state can supply prior context.
- Fallback to full context on:
  - 400/404/409.
  - model/provider switch.
  - missing response ID.
  - session reset.
  - user forces direct/full mode.

### WP5 - WebSocket continuation support

- After T142 inspect corpus identifies response events, parse response IDs from known WebSocket events in inspect/shadow mode.
- Do not assume HTTP response shapes apply to WebSocket frames.
- Only enable WebSocket previous-response reuse when the event shape is version-gated.

### WP6 - Cache heat map

- Track per provider/model/session:
  - prefix hash.
  - cache hint used.
  - cache read tokens.
  - cache create tokens.
  - miss reason.
  - TTL age.
  - request size.
- Surface:
  - `slimference gain --cache`.
  - admin status.
  - TUI stats.
  - flight export.

### WP7 - Safety and retries

- One retry without cache hints on provider rejection.
- Cache hints disabled for that provider/model/endpoint after repeated rejection.
- Never mutate request content solely to chase cache hits unless T149 approves and quality gate passes.

## Acceptance

- [ ] Provider cache capability matrix reflects current supported fields in code, not stale comments.
- [ ] Stable prefix planner places hints only on stable segments.
- [ ] OpenAI/Codex cache hints are endpoint/model-gated and retry-safe.
- [ ] Anthropic cache-control placement respects provider caps.
- [ ] `previous_response_id` owner is session-scoped and invalidates correctly.
- [ ] WebSocket response IDs are only parsed after T142 shape proof.
- [ ] `gain --cache` separates provider-reported savings from estimates.
- [ ] T146 live corpus proves cache hit rates on 30+ turn sessions.
- [ ] `go run ./scripts/ci` passes with 100% coverage for new Go code.

## Expected Upside

- High variance by provider and idle time.
- Continuous 30+ turn sessions: 25-60% input billing reduction if provider caches hit.
- Idle sessions beyond provider TTL: near 0% cache savings, correctly reported.
- Strategic upside: this is the cleanest large saving lever because content quality is unchanged.

## Non-Goals

- Do not fake cache savings from local estimates.
- Do not add cross-user cache sharing.
- Do not keep secrets in cache keys.
- Do not rely on WebSocket response IDs before T142/T146 prove shapes.

