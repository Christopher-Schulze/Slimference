# T334 Strong Codex Thread Attribution And Proxy Cache Net Proof

## Why

T333 fixed the first Codex HTTP attribution gap, but the product path still
needed a stricter shared extractor for all strong Codex thread identities and a
proxy-level cache-net proof surface. Savings reports must split parallel
CLI/Desktop threads by real Codex thread id where Codex provides it, and cache
claims must show read/create/net so a cache strategy regression cannot be hidden
behind gross cached-token counts.

## Acceptance

- Codex HTTP and WSS use the same strong thread extractor for top-level,
  `metadata`, and `client_metadata` session fields.
- Strong thread ids include `thread_id`, `conversation_id`, `session_id`,
  `user_id`, and `x-codex-turn-metadata` where present.
- WSS keeps its historical `prompt_cache_key` fallback only after stronger
  thread metadata is absent; HTTP does not add that weak fallback.
- `slimference savings` keeps parallel Codex threads separate and enriches both
  CLI and Desktop/App rows from the local Codex thread store.
- `slimference gain --proxy` reports provider cache read, create, net, and
  negative-net request counts in text, JSON, and CSV.
- Tests cover strong HTTP/WSS extraction, parallel Codex thread attribution,
  positive cache net, and negative cache net.

## Sub-Tasks

- [x] Consolidate Codex strong thread extraction across HTTP and WSS.
- [x] Extend extraction coverage for top-level, metadata, client metadata, and
  nested Codex turn metadata.
- [x] Add parallel CLI/Desktop savings attribution regression coverage.
- [x] Add proxy flight cache read/create/net fields and negative-net counting.
- [x] Render/export cache-net proof in `gain --proxy`.
- [x] Update product documentation and focused tests.

## Notes

- This is attribution and accounting hardening only. It does not mutate prompt
  bodies, cache keys, model metadata, or Desktop UI internals.
- The `codex-wss:` namespace remains the compatibility namespace for Codex
  thread sessions; route/source/client-family fields carry transport and UI
  surface truth.
- Weak prompt-cache-key grouping remains WSS-only because it is historical WSS
  recovery behavior. HTTP attribution stays conservative to avoid merging
  unrelated sessions that merely share a cacheable prefix.

## Verification

- `go test ./internal/proxy ./internal/analytics ./cmd/slimference`
