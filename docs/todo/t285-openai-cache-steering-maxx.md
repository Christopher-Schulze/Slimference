# TASK 285: OpenAI cache steering max-out

## Why

OpenAI Prompt Caching is server-side and automatic for long repeated prefixes,
but documented `prompt_cache_key` routing lets clients improve hit probability
when many requests share the same stable prefix. The existing Slimference
implementation was safe but too session-shaped: `session` and `model_session`
keys limited reuse to one local session even when the model, instructions, tool
schema, and stable history prefix were identical. The max-safe improvement is
not semantic summarization. It is deterministic provider-cache steering that
never removes or rewrites model-visible context.

## Acceptance

- Generated prompt-cache keys can reuse identical stable prefixes across
  sessions without containing raw prompt text, full paths, or raw session IDs.
- Default generic OpenAI API config uses the strongest safe key cardinality:
  model-bound stable-prefix keys, 1024-token stable-prefix gate, retention off,
  and 15 requests/key/minute cap.
- CodexChatGPT backend routes remain untouched until live request acceptance is
  proven.
- Caller-owned `prompt_cache_key` and `prompt_cache_retention` are preserved.
- A 4xx rejection mentioning prompt-cache fields retries once without hints and
  activates a per-provider/model cooldown to avoid repeated retry latency.
- Docs/spec/config/test surfaces describe the current behavior.
- Focused tests, `go test ./...`, and `go run ./scripts/ci` pass.

## Sub-Tasks

- [x] Add `stable_prefix` and `model_stable_prefix` strategies to the OpenAI
  prompt-cache key builder and config validator.
- [x] Make generic OpenAI prompt-cache steering default-on with
  `model_stable_prefix` and retention off.
- [x] Add per-provider/model rejection cooldown after prompt-cache-field 4xx
  retry.
- [x] Extend tests for cross-session stable-prefix keys, model/prefix rotation,
  legacy session scoping, rate limiting, and rejection cooldown.
- [x] Flush `spec+.md`, `docs/documentation.md`,
  `docs/savings-assessment.md`, and historical T136 notes.
- [x] Run full gates and record results.

## Notes

- This task changes provider-cache routing hints only. It does not summarize,
  paraphrase, capsule, ledger-insert, or otherwise replace model-visible
  context.
- OpenAI server-side prompt-cache hits are provider-reported savings, not local
  token deletion. They must remain accounted separately from Layer 0/1/4 local
  reducer savings.
- Extended `24h` retention remains operator-controlled. Default retention stays
  off so provider default/in-memory behavior is used unless explicitly changed.
- CodexChatGPT stays out of prompt-cache-key injection because backend request
  acceptance is live-proof-only.
- Verification:
  - Focused proxy/config tests passed.
  - `go test ./...` passed.
  - `go run ./scripts/ci` passed all 8 steps with total statement coverage
    96.6%, Codex smoke PASS, live corpus PASS, and leaf audit PASS.

## Deviations

- None.
