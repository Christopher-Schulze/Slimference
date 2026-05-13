# TASK 136: OpenAI/Codex prompt-cache and conversation-state modernization

Status: DONE (safe local scope 2026-05-13; live Codex/WebSocket proof remains T140)
Priority: P0
Scope: `internal/proxy/streaming.go`, `internal/proxy/handler.go`, `internal/proxy/provider.go`, `internal/types/provider_caps.go`, `internal/analytics/`, `cmd/slimference/gain_cmd.go`, `docs/savings-assessment.md`, `docs/documentation.md`.

## Why

Current code says OpenAI has no equivalent prompt-cache usage field. Official OpenAI docs now expose `usage.prompt_tokens_details.cached_tokens`. OpenAI also documents `prompt_cache_key` and `prompt_cache_retention`. That makes prompt-cache modernization a high-value Codex/OpenAI task.

Important reality boundary:

- `previous_response_id` is conversation state and latency/wire leverage.
- OpenAI states previous input tokens in a response chain can still be billed.
- Therefore `previous_response_id` must not be counted as billable token saving unless provider usage proves reduced billable input.
- `cached_tokens` is the right cost/latency signal for OpenAI prompt caching.

## Target State

Slimference accurately tracks and improves OpenAI/Codex prompt-cache behavior:

1. Parse `cached_tokens` from non-streaming and streaming OpenAI/Codex responses.
2. Expose OpenAI cached-token hit rates in admin, TUI, `gain --cache`, and flight recorder.
3. Optionally set stable `prompt_cache_key` where request shape/provider supports it.
4. Optionally set `prompt_cache_retention` with model-aware defaults.
5. Keep `previous_response_id` separate from cache savings.
6. Handle WebSocket `previous_response_id` semantics and connection-local recovery signals.

## Work Packages

### WP1 - Usage parsing

- Add OpenAI/Codex usage parser:
  - `usage.prompt_tokens`
  - `usage.prompt_tokens_details.cached_tokens`
  - `usage.completion_tokens`
  - `usage.completion_tokens_details.reasoning_tokens`
- Parse non-streaming JSON responses.
- Parse streaming SSE events if usage arrives in terminal events.
- Parse WebSocket `response.done` or equivalent events when T140 captures shapes.
- Feed tokenizer calibration and prompt-cache analytics separately.

### WP2 - Capability map

- Extend `ProviderCapabilities`:
  - `SupportsPromptCacheUsage`
  - `SupportsPromptCacheKey`
  - `SupportsPromptCacheRetention`
  - `SupportsPreviousResponseIDHTTP`
  - `SupportsPreviousResponseIDWebSocket`
  - `BillsPreviousResponseIDContext`
- OpenAI:
  - cached-token usage yes.
  - prompt_cache_key yes when API accepts it.
  - retention yes where model supports it.
  - previous_response_id yes, but not billable-saving by itself.
- CodexChatGPT:
  - only enable fields proven by live shape or official config/API route.

### WP3 - Request injection

- Add config:
  - `[proxy.openai_prompt_cache] enabled`
  - `prompt_cache_key_strategy = "off|session|workspace|model_workspace|static"`
  - `static_prompt_cache_key = ""`
  - `retention = "off|in_memory|24h|auto"`
  - `min_tokens = 1024`
  - `max_requests_per_key_per_minute = 15`
- Inject only on supported request shapes.
- Do not inject into unknown ChatGPT backend routes without live proof.
- Do not break idempotence: if caller already set a field, preserve it unless Slimference owns the field via config.

### WP4 - Key strategy

- Stable keys must be privacy-safe and low-cardinality:
  - no raw prompt text.
  - no full path.
  - hash of workspace root + model + system/developer prefix hash.
- Avoid cache overflow:
  - track per-key request rate.
  - degrade to no key if above configured rate.

### WP5 - Retention strategy

- Model-aware default:
  - `auto` chooses provider default unless model requires/benefits from explicit `24h`.
  - If provider rejects retention field, retry without it and mark capability downgraded for session.
- Expose retention in debug logs and admin status.

### WP6 - Previous response state hardening

- Keep T78 as owner.
- Add streaming/WebSocket response-id capture if shapes are proven.
- Recovery:
  - HTTP 4xx unknown previous id: retry full body as today.
  - WebSocket uncached previous id: send new turn with previous_response_id null and full context when supported.
- Metrics:
  - state_reuse_total
  - state_recover_total
  - billable_saving_from_state_total always zero unless usage proves otherwise.

### WP7 - Reporting

- Admin status:
  - `openai_cache.cached_tokens`
  - `openai_cache.prompt_tokens`
  - `openai_cache.hit_rate`
  - `openai_cache.keys_active`
  - `openai_cache.retention_used`
- `slimference gain --cache` must show:
  - Anthropic cache read/create.
  - OpenAI cached tokens.
  - OpenAI previous_response_id state reuse separately.
- Flight recorder stores exact provider-reported usage.

### WP8 - Tests

- Non-streaming OpenAI usage parser.
- Streaming usage parser.
- Injection idempotence.
- Provider rejection retry without cache fields.
- Prompt-cache key rate limiting.
- Reporting tests.

## Acceptance

- [x] OpenAI/Codex cached-token usage is parsed and reported.
- [x] `prompt_cache_key` and `prompt_cache_retention` are injected only when safe and configured.
- [x] `previous_response_id` is never misreported as billable token saving.
- [x] WebSocket continuation support is scoped by captured live shapes and remains T140-only until captured.
- [x] `gain --cache` separates Anthropic, OpenAI cached tokens, and state reuse through existing prompt-cache/flight telemetry.
- [x] `go run ./scripts/ci` passes.

## Sources

- OpenAI Prompt Caching docs: `cached_tokens`, `prompt_cache_key`, `prompt_cache_retention`.
- OpenAI Conversation State docs: `previous_response_id` and billing caveat.

## Notes

- 2026-05-13 implementation:
  - OpenAI/Codex cached-token parsing landed in `internal/proxy/streaming.go` and `internal/proxy/handler.go` before this task closed: non-streaming and streaming usage now read `usage.prompt_tokens_details.cached_tokens` and `usage.input_tokens_details.cached_tokens`, and T134 flight records store provider input/cached/output tokens separately.
  - `types.ProviderCapabilities` now exposes prompt-cache and previous-response capability flags: cache usage, cache key, cache retention, HTTP/WebSocket previous-response support, and whether previous-response context remains billable.
  - `config.Proxy.OpenAIPromptCache` adds `[proxy.openai_prompt_cache]`:
    - `enabled`
    - `prompt_cache_key_strategy = "off|session|model_session|static"`
    - `static_prompt_cache_key`
    - `retention = "off|in_memory|24h|auto"`
    - `min_tokens`
    - `max_requests_per_key_per_minute`
  - Defaults are conservative: disabled, session-key strategy prepared, retention off, 1024-token minimum, 15 requests/key/minute cap.
  - `internal/proxy/openai_prompt_cache.go` injects only for generic OpenAI API requests, never for `CodexChatGPT` backend routes without live proof.
  - Injection preserves caller-owned `prompt_cache_key` / `prompt_cache_retention`; generated keys are hashed and never include raw prompt text or full paths.
  - `24h` retention is emitted only for models documented as supporting extended prompt cache families (`gpt-5.1*`, `gpt-5*`, `gpt-4.1*`); `auto` leaves provider defaults untouched.
  - If upstream rejects cache fields with a 4xx mentioning `prompt_cache_key` or `prompt_cache_retention`, the proxy retries once without those hints.
  - Tests cover idempotence, provider scoping, rate limiting, retention model gating, and rejection-peek body restoration.
  - `go run ./scripts/ci` passes 8/8 with 100.0% total statement coverage after adding the prompt-cache handler retry and branch coverage tests.
- Boundaries:
  - `previous_response_id` remains owned by T78 and is reported as state reuse, not billable token savings.
  - CodexChatGPT prompt-cache-key injection is intentionally off until T140 captures real backend request acceptance.
  - WebSocket continuation and `response.done` shape proof remain T140 because they need live Codex App traffic.
