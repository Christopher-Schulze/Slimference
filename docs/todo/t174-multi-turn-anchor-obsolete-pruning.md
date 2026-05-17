# TASK 174: Multi-turn obsolete-message pruning

Status: TODO (planning 2026-05-16)
Priority: P0 (largest single-message reclamation in iterative sessions)
Scope: `internal/proxy/handler.go`, `internal/sessions/`, `internal/compression/anchor*.go` (re-use anchor infra), `internal/readcache/`

## Why

In iterative refactor sessions a typical pattern: read file → edit it → read again → edit. After the second edit, the **first read** is obsolete: it shows the file in a state that no longer exists. The second edit's pre-state is the post-state of the first edit. Keeping the first read costs tokens for content that's wrong now.

Detect obsolete messages: track file mutations from `apply_patch` / `Write` / `Edit` tool calls. When a file is mutated, every prior tool_result that read it becomes obsolete — replace with `[file edited after this read; superseded]`.

**Why:** Iterative-refactor sessions can accumulate 15-30% of input tokens in obsolete reads. This reclaims them losslessly: the model has the post-edit state in a more recent message anyway.
**How to apply:** Each turn the proxy tracks files mutated by tool calls. On the next request, walk history backward, mark stale reads.

## Target State

1. New `internal/sessions/file_mutation_tracker.go`:
   - `RecordMutation(sessionID, path, atTurn)`
   - `IsObsolete(sessionID, path, readAtTurn) bool` (true if mutated after readAtTurn)
2. Walker in handler.go after extractMessages: identify `tool_result` blocks that are file-reads (via the same heuristic as t170), check IsObsolete, replace with marker.
3. The marker uses ≤30 chars: `[obsolete: edited at turn N]`. Saves whatever the original tool_result was.
4. Telemetry: `obsolete_tokens_pruned` counter.
5. Configurable: `[compression.read_cache] prune_obsolete = true` (default on after validation).

## Acceptance

- Turn 3: `Read src/foo.go` returns 500-line content.
- Turn 5: `Write src/foo.go` succeeds.
- Turn 7: a new request includes turn 3's tool_result but the proxy marks it obsolete with a ~30-char marker.
- Synthetic session shows ≥10% input-token reduction in a 10-turn iterative refactor.
- 100% coverage on the mutation tracker.

## Sub-Tasks

- [ ] File-mutation detector: parse `apply_patch`/`Write`/`Edit` tool calls and extract the path.
- [ ] Per-session mutation log (LRU+TTL).
- [ ] Read-tool-result identifier (shared with t170).
- [ ] Walker that applies the marker.
- [ ] Tests: read-then-edit-then-pre-edit-prune; multiple edits; cross-session isolation.

## Notes

- Combines with t170 (stale aging) and t175 (PreCompact aggression). All three work on the same input history but at different "obsolescence" definitions.
- Risk: an explicit reference to the pre-edit content (rare). Marker keeps the hash so rehydration via readcache is possible.

## Deviations

(none yet)
