# T340 Codex User-ID Attribution Guard

## Why

T334 made Codex HTTP/WSS attribution strong enough to split real Codex threads,
but the shared extractor still accepted `user_id` as a strong session key. That
is not a conversation boundary. On one account with multiple CLI/Desktop threads,
it can merge unrelated sessions into one savings bucket and falsely report full
attribution.

## Acceptance

- Codex HTTP strong attribution accepts only `thread_id`, `conversation_id`,
  `session_id`, and nested `x-codex-turn-metadata` thread/session keys.
- Codex WSS strong attribution uses the same rule and keeps
  `prompt_cache_key` only as the existing WSS fallback after strong metadata is
  absent.
- `user_id` at top level, in `metadata`, or in `client_metadata` never produces
  `codex-http:<id>` or `codex-wss:<id>`.
- User-only Codex HTTP rows fall back to non-thread session keys instead of
  being counted as strongly attributed.
- No prompt/body/model/cache mutation; this is attribution-accounting only.

## Sub-Tasks

- [x] Remove `user_id` from the shared strong Codex thread extractor.
- [x] Add HTTP regression coverage for top-level, metadata, and client-metadata
  `user_id`.
- [x] Add WSS regression coverage for top-level, metadata, and client-metadata
  `user_id`.
- [x] Update product docs and historical task wording.

## Notes

- Product impact: cleaner per-thread savings and attribution health. False
  merge risk goes down; payload behavior is unchanged.
- Drawdown risk: none. The change only affects analytics/session bucketing and
  uses conservative fallback when a true thread id is absent.

## Verification

- `go test ./internal/proxy -run 'TestExtractSessionIDCodexHTTP|TestWSCodexSessionID|TestWSSRequestMetaFromRawMatchesBodyHelpers' -count=1`
