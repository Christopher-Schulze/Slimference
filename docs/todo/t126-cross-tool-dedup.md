# TASK 126: Cross-tool result deduplication (mini-scope)

Status: LIBRARY-COMPLETE / HOT-PATH INTEGRATION DEFERRED (2026-05-02)
Priority: P3
Scope: `internal/crosstool/` (new package), `internal/filter/pipeline.go`, git-specific path-list compaction only.
Driver: a typical LLM coding turn can repeat the same changed-file list across `git status`, `git diff --stat`, `git diff --name-only`, and `git ls-files`. Broad cross-tool elision is dangerous because tool output may be consumed by scripts or by agent reasoning that expects exact text. T126 is therefore reduced to one safe case: exact git path-list duplication where the replacement marker cannot break command output semantics.

This is no longer a broad 5-15% saving task. Expected saving is 1-3% in git-heavy turns. That is acceptable only because the blast radius is now tiny.

## Rejected broad scope

Do **not** implement generic resource-list, error-block, stack-trace, kubectl/docker, grep/find, or fuzzy canonicalisation elision in this task. Those variants can break scripts, confuse agents, and cost more in recovery than they save.

---

## Problem (current state)

Tool results flow through `applyLayer0Filters` independently. The only pattern this task handles is exact changed-file path repetition between git commands inside the same user turn.

2026-05-02 reality check: the current `git status` built-in already compresses porcelain status to counts, not path lists. That means the original hot-path saving premise is smaller than expected. A safe detector/state library is still useful, but wiring it into the pipeline without session/user-turn ownership would be fake precision.

## Target state

A `crosstool.State` per session tracks git path lists the agent has seen this turn. When a later git command emits the same path list as metadata, the L0 pipeline can elide that metadata with a marker:

```
[Slimference: 12 git paths already shown by previous `git status`; diff body unchanged]
```

The marker is allowed only when the actual diff/error/body content remains unchanged.

## Implementation plan

### WP1 - State machine

Completed in `internal/crosstool/state.go`:

```go
type State struct {
    // Per-session state. Cleared at session boundary.
    sessions map[string]*sessionState
    mu       sync.RWMutex
}

type sessionState struct {
    // Git path lists seen in the current user turn.
    gitPathLists []gitPathListFingerprint
    lastUpdate  time.Time
}
```

State is mutated atomically per tool result; reads are snapshot-style.

### WP2 - Git-only detectors

Completed in `internal/crosstool/state.go`:

- `extractGitStatusPaths(toolResult []byte) []string`
- `ExtractGitStatusPaths(toolResult []byte) []string`
- `ExtractGitNameOnlyPaths(toolResult []byte) []string`

No generic path regex. No filesystem sniffing. No fuzzy matching.

### WP3 - Dedup operator

Completed as `State.ApplyGitNameOnly`:

```go
func Apply(state *sessionState, toolResult []byte) (compacted []byte, elided int) {
    // Elide only git metadata path lists that exactly match previous git state.
    // Never touch diff hunks, stderr, script output, or non-git tools.
}
```

Conservative rules:

1. Only git commands.
2. Only exact path-list equality after stable git-path normalisation (`./x` == `x`; case preserved).
3. Never elide diff hunks, file contents, build output, stack traces, resource tables, or arbitrary command output.
4. Per-turn only: state clears when a new user message arrives.
5. Disabled by default until T118b live corpus shows positive net saving.

### WP4 - Pipeline integration

Deferred. Reason: pipeline currently has no session/user-turn boundary in `internal/filter`, and `git status` no longer emits the full path list after its existing compactor. Integrating now would either be global-state wrong or mostly no-op. Correct integration belongs after session-aware tool-result ownership is available.

### WP5 - Telemetry

Pending with hot-path integration.

### WP6 - Reset on session boundary

Library supports `ResetSession(sessionID)`. Automatic user-message boundary reset is pending because the filter pipeline does not yet own session state.

### WP7 - Tests

- Unit tests cover git status extraction, git name-only extraction, non-status rejection, first/different list passthrough, repeated list elision, session reset, path normalisation, and fingerprint stability.

## Acceptance criteria

- [x] Detectors fire only on real git path-list shapes.
- [x] Elision markers emitted with accurate count and source command.
- [x] Library exposes per-session reset.
- [ ] Hot path clears per-session state at user-message boundary.
- [x] Coverage 100%; race-clean; CI gate green after the full Phase R batch.
- [ ] On Slimference's own corpus, git-heavy turns show positive net saving with zero non-git output changes.

## Out of scope

- Cross-session deduplication: T126 is per-session only.
- Cross-conversation stores beyond a single TUI lifetime: same as above.
- Fuzzy elision: the false-positive risk outweighs the saving.

## Validation

```
go test ./internal/crosstool
go test -race ./internal/crosstool
slimference gain --crosstool   # post-corpus measurement
```

## Notes

The user's stated preference is conservative: "radikal minimieren wenn". So the default settings prioritise *not* eliding when in doubt. The elision marker is mandatory and the actual diff/body content remains untouched.
