# TASK 165: Output stop-sequence engineering (deterministic output-token cap)

Status: TODO (planning 2026-05-16)
Priority: P0 (highest output-token win, lowest risk)
Scope: `internal/proxy/handler.go`, `internal/proxy/openai_prompt_cache.go`, `internal/proxy/provider.go`, new `internal/outstop/`, `internal/config/config.go`, `internal/config/defaults.go`

## Why

Output tokens are 3-5× more expensive than input tokens. The dominant source of unwanted output is trailing commentary: "Let me know if you have questions…", "Hope this helps!", "Is there anything else…". Codex's API accepts a `stop` parameter (Anthropic: `stop_sequences`; OpenAI: `stop`) that tells the model to halt generation at the first occurrence of any listed string. Injecting a curated list of trailing-commentary openers stops the model **before** it emits those tokens — no Quality drawdown because the content was non-load-bearing trailing fluff.

**Why:** Single highest-leverage output-saving lever available. Deterministic at the API level (no streaming inspection needed). The model never produces the cut content, so there is no risk of mid-thought truncation.
**How to apply:** Inject a conservative phrase list into every Anthropic and OpenAI/Codex request unless the user explicitly opts out. Phrases must be high-precision (false-positive cuts cost goodwill).

## Target State

1. New package `internal/outstop/` with:
   - `Phrases() []string` returning the curated stop-sequence list (versioned constant).
   - `MergeIntoBody(provider types.Provider, body []byte) ([]byte, bool)` injecting the list into the request JSON. Idempotent: if user-supplied `stop` already contains an entry, dedup. Respects per-provider field naming (`stop_sequences` vs `stop`).
2. Proxy handler invokes `outstop.MergeIntoBody` right after `injectOpenAIPromptCache`. Telemetry: `stop_seqs_added` counter on the request.
3. Phrase list (initial conservative set):
   - `"\nLet me know"`, `"\nHope this"`, `"\nHope that"`, `"\nIs there anything"`, `"\nWould you like"`, `"\nFeel free"`, `"\nIf you have"`, `"\nDo you want"`, `"\nDon't hesitate"`, `"\nPlease let me know"`
4. Config `[compression.output_reduce] stop_sequences_enabled = true` (default on). Operators can disable via env or TOML.
5. Codex-Specific: also drop tokens like `\n[The user can…`, `\nIs there anything else I can do…`.
6. CLI flag `--no-stop-seqs` on `slimference proxy` for one-shot bypass debugging.

## Acceptance

- Anthropic request: `stop_sequences` array contains the merged list, no dupes.
- OpenAI/Codex request: `stop` field equivalent (max 4 per OpenAI contract — pick top-4 by frequency).
- User-supplied `stop` survives and is merged (not overwritten).
- A live e2e session with stop_sequences off and on shows ≥5% lower output token count on a chatty workflow.
- 100% statement coverage on `internal/outstop/`.

## Sub-Tasks

- [ ] Inventory which OpenAI/Codex endpoints accept `stop` (chat completions, responses API). Map per-provider field names.
- [ ] Implement `internal/outstop/phrases.go` (the curated list).
- [ ] Implement `internal/outstop/merge.go` with `MergeIntoBody`.
- [ ] Wire into `internal/proxy/handler.go` after compression injection.
- [ ] Config field + env override.
- [ ] Telemetry: count of requests with stop sequences injected; analytics surface.
- [ ] Tests: dedup, per-provider field naming, max-4 cap for OpenAI, user-list preservation.
- [ ] CHANGELOG + docs/documentation.md "Output Token Reduction" section.

## Notes

- OpenAI's chat-completions API caps `stop` at 4 strings. Codex responses API allows more.
- Anthropic permits up to 4 in `stop_sequences`.
- Phrases include leading `\n` to avoid mid-sentence false matches.
- Risk: phrase may legitimately appear inside content (e.g. quoted text). Mitigation: include the newline prefix so only fresh-paragraph occurrences trigger.
- Future enhancement: per-tool stop sequence sets (a `Bash` reply may legitimately end "do you want to proceed?" — exclude that path).

## Deviations

(none yet)
