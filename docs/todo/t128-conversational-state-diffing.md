# TASK 128: Conversational state diffing (aggressive prompt-cache placement)

Status: PENDING (planned 2026-05-01)
Priority: P1
Scope: `internal/promptcache/` (new package), `internal/proxy/handler.go`, `internal/compression/layer1.go`, provider-specific (`internal/proxy/provider.go`).
Driver: every coding-agent turn re-sends the entire conversation array - system prompt + every prior user message + every prior assistant message + every prior tool result - even though only the latest 1-3 messages are new. Anthropic offers prompt-caching to reuse prefix segments at 10x cheaper read pricing; OpenAI's Responses API offers `previous_response_id` to chain turns; ChatGPT's backend has its own conversation-state. Slimference today annotates basic cache breakpoints; T128 aggressively places breakpoints + tracks per-session prefix hashes so every turn maxes out cache reuse for the provider's native mechanism. Saving on long-conversation turns: 50-80% on input tokens.

This task is provider-specific because the mechanisms differ. We support all three: Anthropic (cache_control breakpoints), OpenAI (previous_response_id chaining), ChatGPT-Plus backend (which already does its own caching - we just stay out of its way). The harness is the same; the per-provider wiring differs.

---

## Problem (current state)

`internal/caching/response_cache.go` and `internal/caching/file_watcher.go` exist for L3 file-content caching but not for conversation-prefix caching. `internal/proxy/handler.go` may set a single `cache_control: ephemeral` breakpoint at the system-prompt boundary for Anthropic; the rest of the prefix is uncached. For OpenAI the API-key transport sees no caching support; for ChatGPT-Plus backend we do not chain via `previous_response_id`.

Symptoms in real session traffic:

- Anthropic call with 30-message conversation: Slimference annotates one cache breakpoint (system->user transition). Anthropic caches that prefix; the next turn re-uses it. But the next 28 messages are uncached, so the next turn pays full price for the prefix it already shipped.
- OpenAI Responses API call: Slimference does not pass `previous_response_id` through, every turn is treated as standalone. OpenAI's prompt-cache (recently announced for the Responses API) is not exploited.
- ChatGPT-Plus backend: Codex Desktop chains conversations natively via `chatgpt.com/backend-api/dev`'s session-id. Slimference passes the session-id through unchanged, which is correct. But we duplicate conversation history if Slimference's L2 summarisation modifies the prefix mid-session - then the next turn's hash mismatches the previous one, breaking ChatGPT's own cache.

## Target state

A `promptcache.Tracker` per session that:

1. Computes a stable rolling hash of the conversation prefix after every turn so we know exactly which prefix segments the provider has already seen.
2. Places `cache_control` breakpoints at every meaningful boundary (system / user msg N / tool-result block / each cached message), maxing out provider cache hit rate.
3. For OpenAI Responses API: passes `previous_response_id` from the prior turn so the provider chains.
4. For ChatGPT-Plus backend: keeps the prefix byte-stable so the provider's own session-cache hits.
5. Detects when L2 summarisation has rewritten prefix segments and emits a `cache_invalidate` signal so the next turn's breakpoints reset.

## Implementation plan

### WP1 - promptcache package

`internal/promptcache/tracker.go`:

```go
type Tracker struct {
    sessions map[string]*sessionPrefix
    mu       sync.RWMutex
}

type sessionPrefix struct {
    // Rolling per-message hash chain so we know exactly what the
    // provider has cached on the previous turn.
    messageHashes []hash64
    // Anthropic-specific: which breakpoints have we annotated?
    breakpoints []int
    // OpenAI-specific: the previous_response_id the provider returned.
    prevResponseID string
    // ChatGPT-backend-specific: whether the prefix has been rewritten
    // by Slimference (e.g. by L2 summarisation) since the last turn.
    prefixRewritten bool
    lastUpdate time.Time
}
```

The hash chain lets us answer "after the last turn, what does the provider have cached?" deterministically.

### WP2 - Anthropic breakpoint placement

`internal/promptcache/anthropic.go`: walks the message array on outbound and inserts `cache_control: {type: "ephemeral"}` markers at:

1. End of system prompt (always).
2. End of every user message that has been in the conversation > 1 turn (so cache hits compound).
3. End of every tool-result block > 5KB (large blocks are worth caching).
4. End of every assistant message (so the prefix on the next user-turn is fully cached).

Anthropic limits cache_control breakpoints to 4 per request. We pick the 4 with the highest expected hit rate based on per-session usage stats.

Output: every long-running session pays cache-read price (10x cheaper) for the prefix and full price only for the latest user-message-onward.

### WP3 - OpenAI Responses API chaining

`internal/promptcache/openai.go`: detects `/v1/responses` endpoint, extracts `id` from the response body, stores it on the session as `prevResponseID`. On the next outbound request, injects `previous_response_id: <stored>` into the request body so the provider chains.

If the conversation array on the next request is bit-equal to (prefix(<previous response>) + new turn), the provider can serve the cached prefix; if not, it falls back to full processing. We let the provider decide.

### WP4 - ChatGPT-Plus backend stability

`internal/promptcache/chatgpt.go`: detects `/backend-api/dev` endpoint. For sessions where Slimference applies L2 summarisation, we set a flag that disables L2 mid-session if the operator has cache-stability enabled (`[compression.cache] keep_prefix_stable_for_chatgpt = true`). This is an opt-in trade-off: lose L2 saving to keep ChatGPT-backend cache-hit.

Most users on ChatGPT-Plus will choose `keep_prefix_stable = true` because cache-hit on ChatGPT-Plus has no per-token cost (Plus subscription is flat-rate); the saving is purely latency.

### WP5 - Cache-invalidate signalling

When L2 summarisation rewrites a previous prefix segment (T111 anchor re-injection means the prefix shape changes), the tracker detects the prefix-rewritten state and emits a fresh hash chain on the next request. Anthropic-side: the next breakpoint set is fresh (no expectation of cache hit on the old prefix). OpenAI-side: drop `previous_response_id` (start a new chain). ChatGPT-side: warning logged if `keep_prefix_stable` was true.

### WP6 - Telemetry

- `slimference gain --cache` reports per-provider cache-read tokens vs cache-create tokens vs full-cost tokens.
- Per-session cache hit rate visible in `/admin/status.cache.{anthropic,openai,chatgpt}.{hit_rate, miss_rate, invalidate_count}`.

### WP7 - Tests

- Per-provider `_test.go`: fixture-driven request/response cycles with pinned hashes; assert breakpoint placement is exactly at the expected positions.
- Round-trip: turn 1 -> response with N tokens cache-create; turn 2 -> response with M tokens cache-read, M >= 0.5*N (caching took effect).
- Cache-invalidate: synthetic L2 prefix rewrite -> assert next turn drops `previous_response_id` and Anthropic breakpoints reset.

## Acceptance criteria

- [ ] Anthropic cache breakpoints maxed (up to 4 per request) on long sessions.
- [ ] OpenAI Responses API turns chain via `previous_response_id`.
- [ ] ChatGPT-Plus backend prefix stays bit-stable when `keep_prefix_stable = true`.
- [ ] Cache-invalidate fires correctly on L2 prefix rewrite.
- [ ] On a 50-turn conversation corpus, input-token saving 50-80%.
- [ ] Coverage 100%; race-clean; CI gate green.

## Out of scope

- Speculative pre-cache (precomputing likely-next-prefixes). Per the user's brief, that path is rejected.
- Cross-session cache sharing (operator may have many sessions; shared cache is rare-cache complex). Future T128b if motivated.
- Modifying the response body to inject our own `cache_control` (we annotate request only; Anthropic returns it in usage stats).

## Validation

```
go test -race ./internal/promptcache/...
slimference gain --cache
```

## Notes

The user's brief specifically said yes to "Conversational State Diffing" and yes to caching aggressiveness, but no to "Cross-Session Cache". This task respects both: aggressive **within** a session, no sharing **across** sessions.
