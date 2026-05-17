# TASK 168: Streaming markdown overhead normalizer

Status: TODO (planning 2026-05-16)
Priority: P2 (small win, very low risk)
Scope: `internal/proxy/streaming.go`, new `internal/outstop/mdnorm/`

## Why

LLM outputs frequently contain redundant markdown decoration: triple-blank-line separators, `---` horizontal rules between every section, repeated heading hierarchy collapses. None of it carries semantic load — it's pure presentation overhead. Normalising in-stream costs ~5% reduction with zero quality impact.

**Why:** Cheap, deterministic, additive on top of t165/t166/t167. Saves 2-5% output tokens on long-form replies without any prompt-side intervention.
**How to apply:** In-stream regex normalisation: collapse `\n{3,}` to `\n\n`, drop standalone `---` lines surrounded by blank lines, normalise multiple consecutive ATX headers of the same level.

## Target State

1. New package `internal/outstop/mdnorm/` with a `Normalize(text string) string` and a streaming variant.
2. Streaming wrapper integrated into the SSE-response pipeline (shared with t166/t167).
3. Idempotent: running twice over the same text yields identical output.
4. Telemetry: count of bytes saved per response.

## Acceptance

- `\n\n\n\n` → `\n\n`
- `\n\n---\n\n` standalone → removed
- `# H1\n\n# H1\n\n# H1` collapsed by removing duplicate-level redundancy where applicable.
- Code-fenced blocks are NEVER altered (markdown semantics inside code).
- 100% coverage.

## Sub-Tasks

- [ ] Algorithm: list normalisation rules with examples.
- [ ] Streaming-safe state machine (rules that span boundaries handled).
- [ ] Tests: code-fence protection, idempotence, byte-count savings.
- [ ] Wire into SSE pipeline with t166/t167.

## Notes

- This is the **smallest** of the output-reduction tasks in this batch. Schedule alongside larger ones for the test/CI cost.
- Code blocks (` ``` `…` ``` `) must pass through verbatim.
- Risk of breaking a model's intentional formatting: low (these are visual gimmicks, no info content).

## Deviations

(none yet)
