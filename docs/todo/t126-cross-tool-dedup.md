# TASK 126: Cross-tool result deduplication

Status: PENDING (planned 2026-05-01)
Priority: P2
Scope: `internal/crosstool/` (new package), `internal/filter/pipeline.go`, `internal/proxy/handler.go`.
Driver: a typical LLM coding turn fires 3-8 tool calls in sequence. Many of those return overlapping data. After `git status` the agent fires `git diff`, and the diff output starts with the same modified-file list. After `find . -name "*.go"` the agent fires `grep "TODO" *.go`, and grep's output repeats every file path find already returned. After `kubectl get pods` the agent fires `kubectl describe pod <name>` and the pod-name set is already in the context. Today the proxy ships every tool result whole. T126 ships a state-aware deduplication layer that elides the redundant repeats so the agent sees only the new information.

This is the smallest of the Phase R levers in absolute saving (typically 5-15% on multi-tool turns) but the highest-correctness-risk: getting it wrong drops information the agent needs. The design is deliberately conservative: we elide only redundancy we can prove the agent has already seen *in the same conversation*.

---

## Problem (current state)

Tool results flow through `applyLayer0Filters` independently. Each tool result's compaction has zero awareness of what the previous tool result returned. Concrete redundancy patterns we observe in real Codex / Claude sessions:

- **File path lists**: `find`, `git status`, `git ls-files`, `ls -R`, `gh pr files` all emit file paths. After the first one, the agent has them in context. The next tool result repeats them.
- **Resource lists**: `kubectl get pods`, `kubectl get services`, `docker ps` repeat the same resource names that were just shown.
- **Status digests**: `git status` after `git pull` already shows the same "modified" set the diff shows.
- **Stack traces**: `pytest` failure followed by `pytest --pdb` shows the same trace twice.
- **Build error duplication**: `go build` followed by `go test` (which itself runs `go build`) shows the same compile error twice.

## Target state

A `crosstool.State` per session tracks "tool-result fingerprints" the agent has seen this turn. When a new tool result arrives, the L0 pipeline compares its content against the state and elides exact-or-near-exact repeats with a marker:

```
[Slimference: 12 file paths from previous `git status` tool result elided]
```

The marker tells the agent the data is intentionally not re-emitted; the agent can ask for it by name if needed.

## Implementation plan

### WP1 - State machine

`internal/crosstool/state.go`:

```go
type State struct {
    // Per-session state. Cleared at session boundary.
    sessions map[string]*sessionState
    mu       sync.RWMutex
}

type sessionState struct {
    // Fingerprints of tool results we have seen, in turn.
    fingerprints []resultFingerprint
    // Per-shape buckets: file-path-list, resource-list, error-block.
    paths       map[string]struct{}    // unique paths the agent has been shown
    resources   map[string]struct{}    // unique k8s/docker/etc. resource names
    errorBlocks map[string]struct{}    // canonicalised error block hashes
    lastUpdate  time.Time
}
```

State is mutated atomically per tool result; reads are snapshot-style.

### WP2 - Shape detectors

`internal/crosstool/shapes.go`:

- `extractPaths(toolResult []byte) []string`: regex + filesystem-shape sniffing. Recognises `<path>:<lineno>` patterns, `M  filename` git markers, bare path lists.
- `extractResources(argv []string, toolResult []byte) []string`: argv tells us "this is a kubectl/docker/etc tool result"; we then parse the tabular format to extract resource names.
- `extractErrorBlocks(toolResult []byte) []string`: canonicalises error messages (strip line numbers, hashes, etc.) so "same error from two tools" hashes equal.

### WP3 - Dedup operator

`internal/crosstool/dedup.go`:

```go
func Apply(state *sessionState, toolResult []byte) (compacted []byte, elided int) {
    // For each shape detected in toolResult, elide the parts already in state.
    // Update state with new entries.
}
```

Conservative rules:

1. **Only elide entries that are byte-equal** (or canonicalised-equal for error blocks). Fuzzy matches are too risky.
2. **Always emit the per-shape elision marker** so the agent knows data was held back.
3. **Never elide more than 60% of a single tool result** - if more than 60% of paths/resources are repeats, we suspect the agent intentionally re-listed and we keep the result whole (raise a `crosstool_skipped_high_overlap` counter).
4. **Per-turn, not per-conversation**: state is cleared when a new user message arrives. A user re-asking "show me the pods" should get the full list again.

### WP4 - Pipeline integration

- New pipeline entry: `cross_tool_dedup` runs **after** all per-tool L0 filters but **before** the request leaves Slimference.
- Bypass: when the tool call has `dedup=false` flag (operator-tunable per session).
- Configuration knob: `[compression.crosstool] enabled = true`, `max_elision_ratio = 0.6`.

### WP5 - Telemetry

- `slimference gain --crosstool` reports per-shape elision over the rolling window.
- `/admin/status.crosstool.{paths,resources,errors}_elided_total` and `_skipped_high_overlap_total`.

### WP6 - Reset on session boundary

- New user-message detection (existing `internal/sessions/`) clears `crosstool.State` for that session.
- Slimference daemon restart clears all state (cross-process sharing is out of scope).

### WP7 - Tests

- Unit tests per shape detector: golden-file fixtures for `git status`, `find`, `kubectl get pods`, `docker ps`, etc.
- Integration tests: simulated 5-tool-call turn; assert per-tool elision correctly applied.
- Negative test: high-overlap (>60%) bypass.
- Edge case: same path emitted by `find` then `git ls-files` - second one elides; if a third tool emits with subtly different path (with vs without leading `./`), our canonicaliser should match.

## Acceptance criteria

- [ ] Shape detectors fire on real `git status`, `find`, `kubectl get`, `docker ps`, `pytest` output.
- [ ] Elision markers emitted with accurate count.
- [ ] Per-session state cleared at user-message boundary.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] On Slimference's own corpus, multi-tool turns show 5-15% per-turn token saving with no measurable agent confusion.

## Out of scope

- Cross-session deduplication: T126 is per-session only. Cross-session memory is a different problem (see future T131-knowledge-graph if ever scoped).
- Cross-conversation stores beyond a single TUI lifetime: same as above.
- Fuzzy elision: the false-positive risk outweighs the saving.

## Validation

```
go test -race ./internal/crosstool/...
slimference gain --crosstool   # post-corpus measurement
```

## Notes

The user's stated preference is conservative: "manchmal nur Probleme". So the default settings prioritise *not* eliding when in doubt. The elision marker is mandatory so an agent that *needs* the elided data can request it by following the marker text.
