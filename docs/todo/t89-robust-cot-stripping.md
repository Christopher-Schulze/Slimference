# TASK 89: Robust CoT stripping for varied reasoner-tag families

Status: todo
Priority: P2
Scope: `internal/summarization/minimax.go`
Driver: `coTRegex` only matches `<think>...</think>`. Newer reasoner families emit `<reasoning>`, `<thinking>`, `<analysis>`, `<scratchpad>`. None of these are stripped today, polluting summaries with chain-of-thought and reducing effective token-budget headroom.

---

## Problem

The current regex `(?s)<think[^>]*>.*?</think\s*>` strips one tag family. Other families slip through. Worse, the function strips by string match; if a tag family ever gets renamed mid-flight by an upstream model, Slimference silently regresses. There is no whitelist of "tags we explicitly want to keep" (e.g. `<list>`, `<table>`).

## Target State

- A configurable `[summarization.cot] strip_tags = ["think", "thinking", "reasoning", "analysis", "scratchpad", ...]` driven from the same prompt-store mechanism (T86).
- Implementation strips matching tag pairs at the boundary of the response (multi-pass) until a fixed point is reached.
- A `keep_tags` whitelist short-circuits stripping.
- Counter `cot_strip_tag_<name>` per tag so we can see which families are actually emitted.

## Implementation Plan

### WP1 - Strip engine
- New `internal/summarization/cotstrip.go` with a tag-pair finder and a fixed-point loop.
- Returns stripped text + per-tag count.

### WP2 - Config + defaults
- `[summarization.cot] strip_tags`, `keep_tags`.
- Defaults include all known reasoner-tag families.

### WP3 - Wire into MiniMax client
- Replace the single regex call with the new engine.
- Update tests against fixtures containing each family.

### WP4 - Telemetry
- Per-tag counters in `/admin/status.summarization.cot`.

## Acceptance Criteria

- [ ] All listed tag families are stripped from summaries.
- [ ] `keep_tags` whitelist is honoured even when nested inside a stripped tag.
- [ ] Counters expose per-tag occurrences.
- [ ] No regression in existing MiniMax tests.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Heuristic stripping of free-form reasoning that is not tag-wrapped (separate research).
- Tag normalisation across providers.

## Validation

```
go test ./internal/summarization/...
```
