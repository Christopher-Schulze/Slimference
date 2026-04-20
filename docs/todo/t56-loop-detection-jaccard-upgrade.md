# T56 - Loop-Detection (T37) Regex → Jaccard-Word-Similarity-Upgrade

Status: closed (spec premise inaccurate)
Priority: n/a

## 2026-04-20 Closure Note

Code verification in `internal/compression/loop_detect.go` revealed that
T37 **already implements Jaccard word-set similarity** (functions
`wordSet` and `jaccard`, threshold `LoopDetectionThreshold = 0.75`,
streak `LoopDetectionMinStreak = 4`). The spec's "regex-only" premise
was wrong.

What could still be added (tracked separately if anyone wants it):

1. Config-exposed threshold + min-streak (currently constants).
2. Stop-word filter (currently every whitespace-split token counts).
3. Per-session opt-out via config.

None of those are high-leverage enough to keep T56 open as a blocker.
Closed as no-op; fold improvements into a dedicated future task when
field evidence motivates them.

---

# Original specification below (kept for historical reference)

Status: todo
Priority: P2
Scope: `internal/compression/loop_detection.go` (or wherever T37 landed), `internal/compression/dedup_minhash.go`, `internal/analytics/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

T37's loop-nudge detects retry loops (same user prompt repeated with
minor variation) using regex equality on the last N user turns. That
misses real loops where the user rephrases slightly
("retry the build", "try that build again", "run the build once more")
- semantically identical, lexically different.

The MinHash/LSH machinery already exists in
`internal/compression/dedup_minhash.go` for tool outputs. Re-using it
for user-turn similarity is ~50 LOC and catches substantially more
loops.

## Current State

- Loop detection compares last N user-turn **strings** by equality.
- No similarity-based matching.
- Analytics counter `loop_nudge_triggered_total` exists.

## Target State

- Loop detection uses Jaccard-word similarity (same MinHash structure
  as L1 dedup) over last N user turns.
- Threshold configurable: `loop_detection_similarity_threshold`
  (default 0.72 - looser than dedup because loops are paraphrased).
- Triggers nudge when ≥ `loop_detection_min_repeats` (default 3) of
  last N (default 5) user turns have pairwise similarity ≥ threshold.
- Nudge text configurable; default: "Noticing a repeating request
  pattern. Consider supplying more context or a different approach."
- Nudge **never** alters the user turn itself - it is added as a
  system hint or a posttool `additionalContext` (depending on hook
  mode).

## Design

### Config

`[loop_detection]`:

```toml
enabled = true
window_user_turns = 5
min_repeats = 3
similarity_threshold = 0.72
nudge_text = "Noticing a repeating request pattern. Consider supplying more context or a different approach."
nudge_position = "system"  # "system" | "posttool" | "off"
```

### Detection algorithm

```go
func detectLoop(history []UserTurn, cfg LoopCfg) (triggered bool, groups [][]int) {
    if !cfg.Enabled { return false, nil }
    window := lastN(history, cfg.WindowUserTurns)
    if len(window) < cfg.MinRepeats { return false, nil }

    sigs := make([]minhash.Signature, len(window))
    for i, t := range window {
        sigs[i] = minhash.ComputeFromWords(tokenizeWords(t.Text))
    }

    groups = clusterBySimilarity(sigs, cfg.SimilarityThreshold)
    for _, g := range groups {
        if len(g) >= cfg.MinRepeats {
            return true, groups
        }
    }
    return false, groups
}
```

`tokenizeWords`: lowercase + split on non-word + strip stop-words
(short list: "the", "a", "please", "can", "you", ...).

`clusterBySimilarity`: union-find over pairs above threshold.

### Metrics

- `loop_nudge_triggered_total` (exists).
- `loop_nudge_similarity_distribution` (histogram of max-pairwise).
- `loop_nudge_group_size` (histogram of cluster size).

TUI Stats: `Loop nudges: 2 (last: "retry the build")`.

### Nudge delivery

- `nudge_position = "system"`: append to system prompt of next request
  (won't break cache stability if T45 places system breakpoint first
  then nudge is after - so it is **after** breakpoint; nudge goes at
  end of system block).
- `nudge_position = "posttool"`: emit via `slimference posttool`
  `additionalContext` on next tool response.
- `nudge_position = "off"`: detect + log only, no user-visible change.

## Implementation Plan

### WP1 - Config surface.

### WP2 - Signature + clustering.

### WP3 - Integration into L1 (or handler)
- Detection runs pre-compression, result attached to request context.
- Injection runs in the relevant output pipeline (system-prompt
  injection is `internal/compression/prompt_mods.go` or similar).

### WP4 - Metrics + TUI.

### WP5 - Tests
- Fixture: 5 user turns "retry the build", "try again", "run build
  once more", "please retry", "build again" → expect trigger.
- Fixture with legitimately different turns → no trigger.
- Similarity threshold boundary tests.

---

## Subtasks

- [ ] Config fields + defaults + ENV.
- [ ] Word tokeniser + stop-word list.
- [ ] MinHash over user-turn words.
- [ ] Union-find cluster over threshold.
- [ ] Nudge delivery per position mode.
- [ ] Telemetry histograms.
- [ ] TUI display.
- [ ] Unit tests (positive, negative, boundary).
- [ ] `docs/tuning-inventory.md` entry.

## Risks

- False positive on "start again", "do that", "yes": catch by requiring
  min turn length (e.g. ≥ 8 tokens) and excluding tiny turns from the
  window.
- Stop-word list bias. Keep short (< 20 words) and document.
- User finds nudge annoying. Mitigation: `nudge_position = "off"` and
  honour user-set opt-out.

## Acceptance Criteria

- [ ] Positive-case fixture triggers nudge.
- [ ] Negative-case fixture does not.
- [ ] Similarity threshold configurable, boundary-tested.
- [ ] `go test -race ./...` green.
- [ ] Nudge cleanly suppressed via `nudge_position = "off"`.

## Out of Scope

- Embedding-based similarity (overkill).
- Multi-turn conversation topic modelling.

---

## Validation

```
go test -race ./internal/compression/...
bun run scripts/benchmarks/loop-detection-fixtures.ts
```
