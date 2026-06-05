# TASK 145: Layer 2 provider-cache and state-reuse maximizer

Status: IN PROGRESS (local/provable slices landed; live proof pending)
Priority: P0
Scope: `internal/caching/`, `internal/proxy/handler.go`, `internal/proxy/streaming.go`, `internal/provider/`, `internal/sessions/`, `internal/flight/`, `cmd/slimference/gain_cmd.go`, `cmd/slimference/cache_cmd.go`, `docs/savings-assessment.md`.

## Why

Layer 2 cache is not only telemetry. It is the place where provider-native reuse can eliminate billing for stable prefixes without changing content. The current implementation parses provider cached-token usage and has safe local prompt-cache hints, but it does not yet act like an aggressive cache planner across Codex/OpenAI/Anthropic request shapes.

This task maximizes provider-supported reuse while keeping savings claims honest: only provider-reported `cached_tokens` / cache read/create fields count as billing savings.

## Target State

Layer 2 cache becomes a provider-capability optimizer:

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
  - Implemented locally:
    - `internal/compression/prompt_cache.go::OptimizeCacheBreakpoints` injects at most four `cache_control: {"type":"ephemeral"}` breakpoints.
    - The stable prefix must be at least 1024 estimated tokens, so tiny one-shot requests skip the hint path.
    - Candidate selection prefers large stable `tool_result` blocks, then late stable user/assistant/tool turns, with deterministic tie-breaking.
    - The caller-owned message slice is not mutated.
    - `internal/proxy/handler_compressible_test.go::TestServeHTTP_promptCacheBreakpointsInjected` verifies the forwarded Anthropic upstream request contains ephemeral cache-control breakpoints.
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
  - `slimference gain --proxy`.
  - admin status.
  - TUI stats.
  - flight export.
- Implemented 2026-05-15:
  - `gain --proxy` emits a content-free prompt-cache heat map grouped by stable-prefix hash.
  - Rows record requests, hint applied/skipped counts, maximum stable-prefix tokens, provider cached tokens, provider cache read tokens, and provider cache create tokens.
  - JSON exposes `prompt_cache_heat`; CSV includes heat-key count, top hash, and top cached-token count; text output prints the five hottest hashes.
  - Cache credits remain labelled as provider/accounting evidence, not local token deletion.

### WP7 - Safety and retries

- One retry without cache hints on provider rejection.
- Cache hints disabled for that provider/model/endpoint after repeated rejection.
- Never mutate request content solely to chase cache hits unless T149 approves and quality gate passes.

## Acceptance

- [x] Provider cache capability matrix reflects current supported fields in code, not stale comments for locally supported paths.
- [x] Stable prefix planner places hints only on stable segments for OpenAI prompt-cache keys and Anthropic cache-control breakpoints.
- [x] OpenAI cache hints are endpoint/model-gated and retry-safe; Codex prompt-cache-key injection remains disabled rather than guessed.
- [x] Anthropic cache-control placement respects current local caps: max 4 breakpoints and 1024-token stable-prefix gate.
- [x] `previous_response_id` owner is session-scoped and invalidates correctly for the HTTP path.
- [ ] WebSocket response IDs are only parsed after T142 shape proof.
- [x] `gain --cache` / `gain --proxy` separate provider-reported savings from estimates.
- [ ] T146 live corpus proves cache hit rates on 30+ turn sessions.
- [x] `go run ./scripts/ci` passes with 100% coverage for new Go code.

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
