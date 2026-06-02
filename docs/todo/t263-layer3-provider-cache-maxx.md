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
- Local response-cache safety now also requires explicit deterministic sampling
  (`temperature: 0`). Missing sampling fields are provider defaults, not proof
  that replaying a prior model answer is behavior-preserving.
- Local response-cache keys now include the HTTP route path and query string in
  addition to provider, canonical body, and semantic headers. The same provider
  and body on `/v1/responses`, `/v1/chat/completions`, or route variants cannot
  alias.
- Local response-cache keys now also include request-affecting Slimference policy
  partitions for stop-sequence injection and be-terse treatment cohorts. A
  cached deterministic response from one request-policy state cannot replay
  across a later policy change.
- Streaming OpenAI/Codex provider-cache accounting now treats `cached_tokens`
  usage reports as per-request totals, not additive deltas, so intermediate and
  final SSE usage events cannot inflate provider-cache read-token claims.
- Local response-cache eligibility is now route-aware for server-state routes:
  OpenAI/Codex Responses requests full-pass unless they explicitly set
  `store:false`, and any `previous_response_id`, conversation, thread, or
  assistant state marker full-passes upstream. This prevents local replay from
  skipping upstream response-id creation or continuation side effects.
- Local response-cache eligibility also blocks nested state metadata:
  `metadata.session_id`, `metadata.conversation_id`, `metadata.thread_id`,
  `metadata.assistant_id`, and Codex
  `client_metadata.x-codex-turn-metadata`. This matches the server-state key
  extractor and prevents a local cache hit from skipping an upstream
  conversation/session state update.
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
- Local response-cache bypass must be disabled for upstream server-state shapes
  unless the request explicitly proves it is stateless.
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
- 2026-05-31: Hardened local response-cache keying with route-aware request keys
  and tests for path/query partitioning. Existing cache-hit tests now seed the
  exact route key, so cache proofs cannot rely on cross-endpoint aliasing.
- 2026-06-01: Hardened local response-cache eligibility again: implicit provider
  sampling defaults now full-pass upstream, and cache replay requires an
  explicit deterministic request. Integration coverage proves both explicit
  stochastic settings and missing sampling fields bypass Layer 3.
- 2026-06-02: Hardened local response-cache keying with request-policy
  partitions. Stage-A and Stage-B cache keys now include stop-sequence policy
  and be-terse cohort/hint partitioning. Regression coverage proves a cached
  deterministic Anthropic response is not reused after stop-sequence injection
  becomes active for the same user request. Follow-up regression coverage also
  proves Stage-A cannot replay a control-cohort response into a treatment-cohort
  BeTerse request when the original user body and account key are otherwise
  identical.
- 2026-06-02: Hardened local response-cache route keying again by including the
  HTTP method in the effective route key. This closes the remaining theoretical
  cross-method alias path while preserving the existing path/query, provider,
  policy, body, and header partitions.
- 2026-06-02: Hardened streaming provider-cache accounting. OpenAI/Codex
  `cached_tokens` in SSE usage events are now merged by maximum/final
  per-request total instead of summed across intermediate and final usage
  frames. Regression coverage proves a stream with 250 cached tokens followed
  by a final 300 cached-token event reports 300, not 550.
- 2026-06-03: Hardened local response-cache eligibility for server-state
  routes. `IsRequestCacheSafeWithRoute` now blocks local replay for Responses
  requests without explicit `store:false`, for `store:true`, and for
  continuation/server-state fields such as `previous_response_id`,
  conversation, thread, or assistant ids. Handler Stage-A and Stage-B cache
  checks now use the same effective route key for eligibility and hashing.
- 2026-06-03: Aligned response-cache safety with server-state metadata
  extraction. The cache now full-passes requests carrying
  `metadata.session_id`, `metadata.conversation_id`, metadata thread/assistant
  ids, or Codex turn metadata, and handler coverage proves repeated metadata
  session requests go upstream twice instead of replaying locally.

## Done

Layer 3 is maxxed when cache hits are maximized by stable-prefix and provider
accounting, every invalidation path is tested, and no model-facing context is
changed for the sake of cache savings.
