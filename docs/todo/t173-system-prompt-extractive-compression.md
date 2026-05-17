# TASK 173: System-prompt extractive compression

Status: TODO (planning 2026-05-16)
Priority: P3 (small win in absolute terms, but stacked with prompt-caching)
Scope: `internal/proxy/handler.go`, `internal/extract/`, `internal/proxy/openai_prompt_cache.go`

## Why

Codex CLI ships a sizable system prompt (~5-8k tokens for the agent-instruction block). Every request carries it. If Anthropic prompt-caching is active, this is largely free after first request — but the first request still pays, and OpenAI doesn't always cache the way Anthropic does.

Apply our `internal/extract.Compactor` to the system prompt itself: drop boilerplate sentences, keep load-bearing instructions verbatim. With prompt caching, the compressed system prompt becomes the cached prefix → its smaller size saves 90% of its (already cheap) cached cost on every future turn.

**Why:** Small per-request win (~3-5% absolute), but it stacks with prompt-cache discount and is structural — every turn benefits.
**How to apply:** Extract-compactor the system prompt. Cache the compacted form. Re-emit on every turn unchanged for max prompt-cache hits.

## Target State

1. New `internal/extract/system_prompt.go` with a `CompactSystemPrompt(text) string` helper that:
   - Preserves all imperative instructions (`Always do X`, `Never do Y`)
   - Preserves all named entities (tools, files, paths)
   - Compresses background prose
2. Per-config-key cache so the same system prompt yields the same compacted form across processes.
3. Apply only when `[compression.output_reduce] system_prompt_compact = true` (default off until validated).
4. Quality A/B: run a captured benchmark with/without; release as default only when no regression.

## Acceptance

- 5-8k token system prompt compacts to 3-5k tokens.
- All explicit imperatives ("Use the Read tool…", "Never run rm -rf…") preserved.
- Compaction is byte-stable: same input → same output, every process.
- Quality A/B passes.

## Sub-Tasks

- [ ] Survey real Codex / Claude system prompts to identify load-bearing patterns.
- [ ] Extractor with strict imperative-preservation rules.
- [ ] Compacted-form cache keyed on input hash.
- [ ] Tests: imperative preservation, byte-stability, idempotence.
- [ ] Quality A/B harness.

## Notes

- Lowest absolute win in this batch (per token), but **highest reliability** because each turn benefits.
- Pair with t165-t167 — output-side wins on every turn add up.

## Deviations

(none yet)
