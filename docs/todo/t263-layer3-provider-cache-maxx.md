# T263 - Layer 3 provider/prompt cache max-out

## Why

Layer 3 is the cleanest savings class: it can reduce cost without changing the
model-facing context. That makes it the best place to chase aggressive gains.
The remaining work is precision: cache keys, invalidation, provider accounting,
and long-session proof.

## Current reality check

- Response/provider cache logic exists.
- Local response-cache safety now rejects tool-capable request shapes: `tools`,
  `functions`, `tool_choice`, `function_call`, tool/function roles, and
  Responses `function_call_output` inputs full-pass upstream instead of being
  replayed from cache.
- Prompt-cache planning exists for stable prefixes.
- Layer 3 is default-enabled for supported paths.
- Savings claims must be separated from local byte savings and output-wire
  savings.

## Product target

Maximize provider-side cache hits and local response-cache hits without changing
what the model sees. All headline savings must come from billable provider
accounting or locally proven upstream bypass, not mixed counters.

## Technical work packages

1. Normalize cache accounting:
   - billable input savings
   - provider prompt-cache read/write tokens
   - local response-cache upstream-bypass savings
   - output-wire savings separately
2. Harden key derivation:
   - model/provider/version
   - route
   - stable prefix hash
   - tool schema hash
   - relevant config flags
   - dependency paths and mtimes where applicable
3. Harden invalidation:
   - file dependency watcher
   - tool schema changes
   - provider/model changes
   - prompt-cache breakpoint policy changes
   - Codex version tuple changes
4. Add provider-specific proof:
   - OpenAI/Codex Responses delta behavior
   - Anthropic prompt cache behavior where relevant
   - fallback behavior when provider does not report cache usage
5. Add long-session proof harness:
   - stable prefix across many turns
   - latest user message changes do not rotate stable-prefix key
   - tool schema pruning does not poison cache keys
   - prompt-cache hit survives read/search reducers

## Zero product-drawdown gates

- No cache hit can return a response across different model/provider/config
  semantics.
- Local response-cache bypass must be disabled for streaming or tool-call shapes
  where replaying a cached response would change workflow timing or tool state.
- Prompt-cache optimization must not mutate model-facing prompt blocks just to
  increase local savings counters.
- Any key uncertainty full-passes upstream.

## Savings targets

- Provider-reported prompt-cache read tokens should be visible and separated in
  reports.
- Long-session stable-prefix proof should show cache-read growth after the first
  stable turns.
- No mixed headline figure that adds billable input savings and output-wire
  bytes.

## Verification

- Unit tests for cache key rotation and non-rotation.
- Dependency invalidation tests.
- Tool-call safety tests for local response-cache bypass.
- Provider-accounting fixture tests.
- Long-session replay with stable prefix.
- `go test ./internal/caching ./internal/proxy ./scripts/utils`
- `go run ./scripts/ci`

## Progress

- 2026-05-31: Hardened local response-cache eligibility so tool-capable shapes
  never replay a cached response. This protects workflow semantics for tool
  calls while leaving deterministic non-tool requests cacheable.

## Done

Layer 3 is maxxed when cache hits are maximized by stable-prefix and provider
accounting, every invalidation path is tested, and no model-facing context is
changed for the sake of cache savings.
