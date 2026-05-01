# TASK 128: Conversation state + prompt-cache hardening

Status: CODE-COMPLETE / LIVE-SAVING-PROOF PENDING (2026-05-02)
Priority: P1
Scope: `internal/promptcache/` (new package) or extension of existing server-state/cache packages, `internal/proxy/handler.go`, `internal/compression/layer1.go`, provider-specific (`internal/proxy/provider.go`). Must reuse T78 instead of duplicating it.
Driver: every coding-agent turn tends to re-send a large conversation prefix. Anthropic prompt caching can make repeated prefixes cheaper when cache-control breakpoints and TTL are used correctly. OpenAI Responses has `previous_response_id`, but official provider semantics still bill previous input tokens; therefore `previous_response_id` is a state/wire/latency lever, not a standalone token-cost lever. ChatGPT's backend has its own conversation-state. T128 hardens provider-native mechanisms and measures actual savings instead of claiming automatic 50-80% reduction.

This task is provider-specific because the mechanisms differ. We support all three: Anthropic (cache_control breakpoints), OpenAI (T78 `previous_response_id` hardening + prompt-cache-compatible request stability where supported), ChatGPT-Plus backend (prefix stability, stay out of native cache semantics). Realistic expected saving: 25-45% input-token cost on continuous long sessions where provider cache semantics actually apply; lower or zero after idle/cache-expiry or unsupported providers.

## Reality correction (2026-05-01 audit)

- T78 already shipped a non-streaming OpenAI/Codex `previous_response_id` lever, default off. T128 must extend/harden it, not rebuild it.
- Do not claim OpenAI `previous_response_id` by itself reduces billed input tokens; previous input tokens can still be billed.
- Anthropic's prompt cache is the strongest direct cost-saving path, but TTL/breakpoint limits make 50-80% a best-case, not an acceptance target.
- Acceptance must be measured against T118b/live corpus, not only synthetic conversations.

---

## Problem (current state)

`internal/caching/response_cache.go` and `internal/caching/file_watcher.go` exist for L3 file-content caching. T78 also exists for non-streaming OpenAI/Codex server-state via `previous_response_id`, default off. The remaining gap is provider-specific cache placement/stability and honest measurement across Anthropic, OpenAI Responses, and ChatGPT backend paths.

Symptoms in real session traffic:

- Anthropic call with 30-message conversation: Slimference annotates one cache breakpoint (system->user transition). Anthropic caches that prefix; the next turn re-uses it. But the next 28 messages are uncached, so the next turn pays full price for the prefix it already shipped.
- OpenAI Responses API call: T78 can pass `previous_response_id` on non-streaming paths, but it is default-off and streaming response-id capture is deferred. This needs hardening and measurement, not a second implementation.
- ChatGPT-Plus backend: Codex Desktop chains conversations natively via `chatgpt.com/backend-api/dev`'s session-id. Slimference passes the session-id through unchanged, which is correct. But we duplicate conversation history if Slimference's L2 summarisation modifies the prefix mid-session - then the next turn's hash mismatches the previous one, breaking ChatGPT's own cache.

## Target state

T128 deliberately stays provider-specific:

1. Anthropic: place up to four `cache_control` breakpoints where they have the highest expected cache value: large stable tool results first, then late stable assistant/user turns. This is implemented in `internal/compression/prompt_cache.go` by replacing uniform T45 placement with deterministic value scoring.
2. OpenAI / Codex Responses: reuse T78's existing `previous_response_id` server-state path. Do not add a second tracker and do not count state reuse as token-cost saving unless provider usage fields prove a cache read / reduced billed input.
3. ChatGPT backend: preserve native conversation/session state and avoid request-shape churn. Prefix-stability policy remains a future config lever because blindly disabling L2 mid-session would trade a known token saving for an unmeasured latency-only cache effect.
4. Measurement: `/admin/status.prompt_cache` exposes both Slimference-injected breakpoint count and provider-reported cache read/create token counters.

## Implementation plan

### WP1 - Existing owner check

Completed. T78 already owns request/response server-state and OpenAI/Codex `previous_response_id`. T128 does not introduce `internal/promptcache/` because that would duplicate state ownership and increase recovery risk. The remaining code owner for Anthropic prompt-cache placement is `internal/compression/prompt_cache.go`.

### WP2 - Anthropic breakpoint placement

Completed. `OptimizeCacheBreakpoints` now:

1. Runs only when the stable prefix exceeds the existing 1024-token floor.
2. Scores eligible stable-prefix messages by value.
3. Prioritises `tool_result` blocks >= 5KB.
4. Prefers late stable assistant/user/tool turns over early low-value content.
5. Selects at most four breakpoints, sorted back into request order.
6. Preserves the previous no-mutation guarantee.

Tests cover high-value tool-result priority, four-breakpoint cap, empty-content skip, stable deterministic placement, and counter accounting.

### WP3 - OpenAI Responses API hardening

No new code in T128. T78's existing server-state path remains the single owner:

- Capture response ids for supported non-streaming paths.
- Keep recovery on `previous_response_id not found` 4xx.
- Add telemetry that separates wire/state reuse from billable-token savings.
- Add prompt-cache-compatible `prompt_cache_key` / stable-prefix support only when the provider API exposes stable documented semantics.

`previous_response_id` is not counted as token-saving unless provider usage fields prove a cache read / reduced billable input.

### WP4 - ChatGPT-Plus backend stability

No code in this pass. The correct current behavior is passthrough of ChatGPT backend session state plus the existing compression stack. A future `keep_prefix_stable_for_chatgpt` flag is only justified after live latency/cache evidence, because disabling L2 to chase backend cache stability can be worse for token cost.

Most ChatGPT-Plus users care about latency here, not per-token cost. This must stay an opt-in policy lever, not a silent default.

### WP5 - Cache-invalidate signalling

Deferred. No separate tracker exists after the owner check above. Current practical behavior: Anthropic breakpoints are recomputed from the actual outbound body each request; OpenAI/Codex server-state recovery remains owned by T78; ChatGPT prefix-stability needs live evidence before adding invalidation policy.

### WP6 - Telemetry

Partially complete:

- `/admin/status.prompt_cache.breakpoints_injected_total` reports Slimference cache-control placement.
- `/admin/status.prompt_cache.cache_read_tokens` reports provider-reported cache-read tokens already captured by analytics.
- `/admin/status.prompt_cache.cache_create_tokens` reports provider-reported cache-create tokens already captured by analytics.
- `/admin/status.prompt_cache.estimated_saved_read_tokens` reports the Anthropic-style 90% read-token discount estimate and does not count OpenAI `previous_response_id`.

Completed: `slimference gain --cache` formats the same persisted provider-reported cache read/create counters exposed by prompt-cache stats. This is reporting only; it does not invent OpenAI `previous_response_id` token-saving claims.

### WP7 - Tests

- `internal/compression`: breakpoint placement, priority scoring, caps, stability, no empty-content placement, counter accounting.
- `internal/proxy`: admin prompt-cache telemetry from provider-reported cache read/create usage.
- Live corpus still required for actual 25%+ provider-cache saving proof.

## Acceptance criteria

- [x] Anthropic cache breakpoints maxed (up to 4 per request) on long stable prefixes.
- [x] Existing T78 OpenAI/Codex server-state path is reused; no duplicate implementation introduced.
- [x] OpenAI `previous_response_id` telemetry is not counted as token-cost saving unless provider usage proves cache read / reduced billable input.
- [ ] ChatGPT-Plus backend prefix-stability policy is proven on live traffic before adding a default behavior.
- [ ] Cache-invalidate policy is added only if a future tracker owns cross-provider prefix hashes.
- [ ] On a 30+-turn live/scrubbed corpus, measured provider-cache input saving is at least 25% on continuous Anthropic-like supported sessions; best-case 40%+ is reported separately.
- [x] `slimference gain --cache` reports persisted provider prompt-cache counters.
- [x] Focused tests green for touched packages.
- [x] Full coverage/race/CI gate green after the full Phase R batch.

## Out of scope

- Speculative pre-cache (precomputing likely-next-prefixes). Per the user's brief, that path is rejected.
- Cross-session cache sharing (operator may have many sessions; shared cache is rare-cache complex). Future T128b if motivated.
- Modifying the response body to inject our own `cache_control` (we annotate request only; Anthropic returns it in usage stats).

## Validation

```
go test ./internal/compression ./internal/proxy
go test -race ./internal/compression ./internal/proxy
slimference gain --cache
```

## Notes

The user's brief specifically said yes to "Conversational State Diffing" and yes to caching aggressiveness, but no to "Cross-Session Cache". This task respects both: aggressive **within** a session, no sharing **across** sessions.
