# TASK 78: Provider server-state exploitation

Status: shipped 2026-04-30 (streaming response-id capture deferred)
Priority: P1
Scope: `internal/proxy/provider.go`, `internal/proxy/handler.go`, `internal/sessions/`, `internal/types/types.go`
Driver: OpenAI Responses API supports `previous_response_id` for server-side conversation persistence; ChatGPT-Backend (Codex) keeps server-side conversation state via `conversation_id`. Anthropic prompt-caching is the only server-state lever Slimference uses today. Skipping resends entirely is a much bigger savings than compressing the resend.

---

## Problem

For many provider conversations, the upstream already keeps state. Resending the full message history every turn is wasteful: the upstream charges for tokens it could read from its own server-side store. Today the proxy treats every provider symmetrically and applies L1/L2/L3 compression to the full body. That misses a much larger lever: don't send the body in the first place.

## Target State

For providers that support server-side context retention:

- The proxy detects upstream support (capability map), tracks the latest server-side response identifier per conversation, and on next request uses `previous_response_id` (OpenAI Responses) or equivalent (ChatGPT-Backend `conversation_id`) instead of resending the prefix.
- Anthropic stays unchanged because its prompt-cache flow already exploits the same idea differently.
- Fallback path: if the upstream rejects with "unknown previous_response_id" (model rotation, cache eviction, etc.), the proxy retries the same logical request with the full body and marks the conversation as needing a fresh anchor.

## Implementation Plan

### WP1 - Capability map
- Extend `types.Provider` (or a sibling table) with `supports_response_id`, `supports_conversation_id` flags.
- Defaults: OpenAI Responses = true, CodexChatGPT = true, Anthropic = false (uses cache instead), generic OpenAI Completions = false.

### WP2 - Conversation tracking
- Per-session, persistently key the latest upstream response identifier in `internal/sessions`.
- Identifier source: response body field `id` (OpenAI Responses), `conversation_id` (ChatGPT-Backend).

### WP3 - Request rewrite
- On request build, replace the message history with the last user turn + `previous_response_id` for supported providers.
- Counter: `server_state_skip_total` per provider.

### WP4 - Recovery path
- On upstream 4xx for unknown previous-id, mark cache invalid and resend full body in a single retry with anchor reset.
- Log a counter `server_state_recover_total`.

### WP5 - Telemetry
- `RequestSummary` gains `server_state_used` and `server_state_anchor_age` so reports can show how often this lever fires.
- Expose in `/admin/status.server_state`.

## Acceptance Criteria

- [x] OpenAI Responses / chat-completions requests use `previous_response_id` after the first turn in a session (`internal/proxy/server_state.go`, `internal/proxy/handler.go` step 8.5; integration test `TestServeHTTP_serverStateCaptureAndReuse`).
- [x] CodexChatGPT requests use server-side conversation continuity (same wire-in path keyed by top-level `conversation_id`; pure-function tests cover the rewrite).
- [x] Integration test simulates "unknown previous_response_id" → recovery path resends full body with fresh anchor (`TestServeHTTP_serverStateRecoveryOnUnknownPreviousID`).
- [x] `/admin/status.server_state` exposes the counters: `sessions`, `skip_total`, `recover_total`.
- [x] No Anthropic regression (capability map → `SupportsResponseID=false`; `TestServeHTTP_serverStateAnthropicNoRegression`).
- [x] Coverage gate green at 100%; race tests green.

## Deferred follow-up

- Streaming response-id capture: SSE responses do not feed `extractResponseID`, so the proxy still resends the full body on the next turn after a streaming reply. Add an SSE-event tap (e.g., parse `response.completed` for OpenAI Responses, `conversation.created` for ChatGPT-Backend) in a separate task — touches `streamingRelay*` which is currently a hot byte-for-byte tee.

## Out of Scope

- Multi-machine session-state sync.
- Storing full server-side response bodies on disk; identifiers are enough.

## Validation

```
go test ./internal/proxy/... ./internal/sessions/...
go test -tags=integration ./tests/integration
```
