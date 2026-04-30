# TASK 111: Layer 2 anchor verbatim re-injection in ApplyToMessages

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P0
Scope: `internal/summarization/layer2.go`, `internal/summarization/anchor.go`, `internal/summarization/progressive.go`, `internal/summarization/validator.go`
Driver: `AnchorDetector` already identifies critical messages (edits, errors, decisions, config touches) and `RunCompressionJobContext` correctly excludes them from MiniMax input. But `ApplyToMessages` (`layer2.go:96-136`) collapses the entire `[0..coveredEnd]` range into a **single synthetic assistant message**: the anchor messages are gone from the request the upstream actually sees. The model gets a summary that may say "edit_file applied to handler.go" but loses the verbatim diff content of the edit and the verbatim error text. This is the root cause of "L2 forgets things that mattered" complaints and a real correctness gap, not just a tuning concern.

---

## Problem

```go
// layer2.go:96-136 (current)
func (l *Layer2) ApplyToMessages(messages []types.Message) (...) {
    cached, coveredRange := l.cache.GetCurrent()
    end := coveredRange[1]
    summaryText := fmt.Sprintf("[Conversation summary covering messages 0-%d: %s]", end, cached.Summary)
    synthetic := types.Message{Index: 0, Role: "assistant", Content: ...}
    tail := messages[end+1:]
    result := []types.Message{synthetic}
    result = append(result, tail...)  // anchors at indices 0..end are gone
    return result, ...
}
```

`cached.AnchorsInlined` records which indices were excluded from summarisation, but those messages are **dropped from the output**. The progressive path (`progressive.go:194-200`) handles this correctly; the default `ApplyToMessages` path does not.

## Target State

`ApplyToMessages` produces:

```
[synthetic summary covering 0..end excluding anchors] +
[anchor message at idx i1 verbatim] +
[anchor message at idx i2 verbatim] +
...
[tail messages from end+1..]
```

Order preservation: anchors are inserted in their original conversation order so the model still sees the chronological flow. The synthetic summary explicitly references the anchor indices it does NOT cover (`[Conversation summary covering messages 0-N excluding anchors at 3, 7, 11: ...]`) so the model knows where to stitch.

Anchor budget: at most `[compression.summary] max_anchors_inlined` anchors (default 8) are preserved verbatim. If the anchor count exceeds the budget, the lowest-priority anchors are inlined as one-line digests instead of full text. Priority order from `anchor.go`:

1. Errors / panics / FAIL (highest)
2. Edits / writes / creates / deletes
3. User decisions (yes/no/approved)
4. Config-file touches
5. Architecture-level assistant explanations (lowest)

Validator extension: a new check confirms that the post-`ApplyToMessages` slice contains at least `min(anchor_count, budget)` of the original anchor messages verbatim.

## Implementation Plan

### WP1 - Anchor capture
- `Layer2.RunCompressionJobContext` already detects `allAnchorIndices` via `anchor.Detect`.
- Persist these in `CachedSummary.AnchorIndices []int` (already a field) AND a new `CachedSummary.AnchorMessages []types.Message` snapshot of the raw messages at those indices, deep-copied so later mutations cannot tamper.

### WP2 - ApplyToMessages rewrite
- After computing `synthetic`, build `out` as: `[synthetic, anchor_messages_in_order..., tail...]`.
- Re-index every `out[i].Index = i` (test-relevant invariant).
- Compute the new compressed-token total = synthetic + anchors + tail; the savings number must reflect this (smaller savings than today's "everything is collapsed" math, but the math is now honest).

### WP3 - Anchor budget + priority demotion
- New `internal/summarization/anchor_priority.go` ranks anchors by category.
- When `len(anchors) > maxBudget`, the bottom-priority overflow is rendered as a single-line `[anchor: <category> at msg N - <one-line digest>]` synthetic message instead of the full verbatim block.

### WP4 - Summary template change
- Update `summaryText` to enumerate the excluded indices: `[Conversation summary covering messages 0-N excluding anchors at i1, i2, ...: <summary>]`.
- Few-shot examples in `minimax.go` updated so the model knows to expect anchor stubs interleaved.

### WP5 - Validator extension
- `CompressionValidator.ValidateApply(originalMsgs, postApplyMsgs, anchorIndices)` confirms each anchor index appears verbatim in the post-apply slice (or as a properly-formed digest when budget exceeded).
- Failure logs `WARN layer2.anchor_loss` and falls back to NOT applying the summary (defensive).

### WP6 - Telemetry
- `/admin/status.layer2.anchors.{detected_total, inlined_verbatim_total, demoted_to_digest_total, dropped_total}`.
- `RequestSummary.AnchorsInlined int` field added so session reports surface anchor coverage.

### WP7 - Tests
- Fixture: 20-message conversation with 5 anchors. Assert post-apply slice contains all 5 verbatim.
- Budget overflow: 10 anchors with budget=3. Assert 3 verbatim + 7 digests.
- Validator failure path: forge a missing anchor in mocked apply; assert fallback to original messages.
- Index re-numbering invariant.

## Acceptance Criteria

- [ ] Every anchor detected by `AnchorDetector` survives into the post-`ApplyToMessages` slice (verbatim or digest).
- [ ] Synthetic summary text enumerates the excluded indices.
- [ ] Validator catches anchor drop and triggers safe fallback.
- [ ] Token-savings counters reflect the honest math (anchors retained -> smaller savings number than the misleading current value).
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Anchor de-duplication (if the same anchor is referenced twice in the prefix the digest path will mention it once - acceptable).
- Cross-summary anchor stitching (anchors from previous summary entry surfaced again when current summary supersedes - tracked separately if needed).

## Validation

```
go test -race ./internal/summarization/... ./internal/proxy/...
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
```
