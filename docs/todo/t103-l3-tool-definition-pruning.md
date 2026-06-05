# TASK 103: Layer 3 - Tool-Definition Pruning

Status: FORWARD-PATH SHIPPED (2026-04-30); reattach path tracked as T103b. Default off via `[compression.tuning] tool_prune_enabled`.
Priority: P1
Scope: `internal/proxy/handler.go`, `internal/compression/`, new `internal/toolprune/`
Driver: Claude tool schemas in the system block are routinely 5-10k tokens. After 20 turns of an interactive session, only a handful of tools have actually been used. A lazy-load model that drops idle tool schemas and reattaches them on demand is a brand-new compression axis no current layer touches.

---

## Problem

Tools are sent on every request. Even tools that have never been used in the current session ride along, paying full token cost. A 7k-token tool block costs more per turn than many full Layer 1 rewrites save.

T103 is gated on T76 because tool removal must be reversible: the model may invoke a tool that has been pruned, and the proxy needs to reattach the definition transparently.

## Target State

Layer 3 (Tool-Definition Pruning) runs after L1 and before final body assembly:

1. Per-session tool-usage tracker counts how often each tool has been used in the last N turns.
2. Tools idle for `[toolprune] idle_threshold_turns` (default 20) are removed from the request's tool list.
3. The pruned definitions are kept in T76 archive keyed by `(session_id, tool_name)`.
4. If the model emits a `tool_use` for a pruned tool, the proxy:
   - Detects the tool name in the upstream response (or pre-emptively when the model's prompt mentions the tool).
   - Reattaches the definition for the next request.
   - Retries the model turn if the upstream rejected the tool name.

## Implementation Plan

### WP1 - Usage tracker
- `internal/toolprune/usage.go` tracks per-session tool invocations with a sliding window.

### WP2 - Pruner
- New L3 pass on the final body that drops tool definitions exceeding the idle threshold.
- Storage: T76 content archive.

### WP3 - Reattach path
- On upstream response, detect tool_use blocks for known-pruned tools.
- On 4xx for unknown tool, retry with reattached definitions.

### WP4 - Telemetry
- `toolprune_pruned_tools_total`, `toolprune_reattach_total`, `toolprune_tokens_saved_total`.

### WP5 - Config knobs
- `idle_threshold_turns`, `min_tool_set` (always-on tools), `max_pruned_tools_per_request`.

### WP6 - Tests
- Multi-turn session with a never-used tool; assert pruning fires and reattach works.

## Acceptance Criteria

- [x] Tool definitions for tools idle beyond threshold are removed from the request.
- [x] No regression on tools that are always used (fail-open for unseen tools).
- [x] Counters surface in `/admin/status.tool_prune`.
- [x] Coverage 100%; race tests green.
- [x] T103b (2026-04-30): heuristic-mention reattach path shipped (`UsageTracker.RememberPrunedDef` + `MentionedTools` + `ReattachToolDefinitions`). Upstream 4xx-on-unknown-tool detection remains a design alternative; reopen as T103d only if heuristic reattach proves too eager in real traffic.
- [ ] **Tracked as T103c** (separate task): T77 quality signals show no spike in re-read rate after L3 enables. Requires a soak window with the flag on against real traffic; not measurable in unit tests.

## Out of Scope

- Predictive pruning based on user intent.
- Cross-session tool usage learning.

## Validation

```
go test ./internal/toolprune/... ./internal/proxy/...
go test -tags=integration ./tests/integration
```
