# T341 Codex Responses Input Fallback Attribution

## Why

Live decision logs showed new Codex HTTP rows still landing in `session_id:
"empty"` when Codex did not send strong thread metadata. The fallback session
hash only understood Chat Completions `messages`, while current Codex HTTP uses
Responses API `input`. That collapsed unrelated anonymous requests into the same
bucket and made attribution health noisier than necessary.

## Acceptance

- Strong Codex thread IDs still win over every fallback.
- OpenAI `previous_response_id` still wins before content hashing.
- Codex/OpenAI content hashing reads Chat Completions `messages` and Responses
  API `input`.
- Responses API string input, user message input, and user content arrays all
  produce stable `fh:<hash>` fallback IDs instead of `empty`.
- No request payload, prompt, model, cache key, or savings mechanism changes.

## Sub-Tasks

- [x] Extend fallback input extraction to Responses API `input`.
- [x] Keep assistant-only entries from becoming user fallback text.
- [x] Add regression coverage for string input, user message input, and content
  array input.
- [x] Update product documentation.

## Notes

- Product impact: fewer anonymous `no-session:proxy` rows when true thread
  metadata is absent. This does not claim exact thread identity, but it prevents
  unrelated fallback rows from being merged into one empty bucket.
- Drawdown risk: none. This affects only local accounting keys after stronger
  identities are unavailable.

## Verification

- `go test ./internal/proxy -run 'TestExtractSessionIDCodexHTTP|TestWSCodexSessionID|TestWSSRequestMetaFromRawMatchesBodyHelpers' -count=1`
