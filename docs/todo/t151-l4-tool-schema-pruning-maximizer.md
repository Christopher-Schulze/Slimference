# TASK 151: L4 tool-schema pruning maximizer

Status: DONE
Priority: P0
Parent: T103
Scope: `internal/toolprune/`, `internal/proxy/handler.go`, `internal/quality/`, `internal/planner/`, `cmd/slimference/gain_cmd.go`, `docs/documentation.md`

## Why

Tool schemas are a persistent per-request tax. T103 shipped the forward pruning and heuristic reattach path, but the remaining win is turning it into a soak-safe optimizer: prune aggressively where safe, detect misses early, reattach without user-visible failure, and prove no re-read or repair-turn spike before default-on.

## Target State

Layer 4 removes idle tool definitions with almost-zero drawdown:

1. Always keep core tools required for shell/edit/read safety.
2. Prune unused or cold tool definitions after a configurable idle window.
3. Archive pruned definitions by session and tool name.
4. Reattach when the user/model mentions a pruned tool.
5. Retry once with full tools when upstream/tool errors prove a miss.
6. Track miss, reattach, retry, and repair-turn signals.
7. Let T149 decide when pruning is allowed for the current task shape.

## Implementation Plan

### WP1 - Always-on classes

- Define default keep classes for shell, file edit, file read, safety, and active MCP tools.
- Keep an explicit config override for project-specific must-keep tools.

### WP2 - Miss telemetry

- Record pruned tool count, saved token estimate, reattach count, retry count, and suspected miss count.
- Surface in `/admin/status.tool_prune`, TUI, and `gain --proxy`.

### WP3 - Retry fallback

- Detect provider/tool errors that imply a missing definition.
- Rebuild request with archived full tool set and retry once.
- Mark the session bucket as softened if fallback fires.

### WP4 - Quality gate

- Feed T77/T149 with re-read and repair-turn changes.
- Auto-disable pruning for provider/model/session buckets that exceed failure thresholds.

### WP5 - Tests

- Pruned idle tool reattaches by mention.
- Missing-tool error triggers one full-tool retry.
- Always-on tools are never pruned.
- Failure bucket disables future pruning.

## Acceptance

- [x] No active/safety-critical tool class is pruned.
- [x] Full-tool fallback works exactly once on missing-tool errors.
- [x] Tool-prune miss telemetry is visible.
- [x] T149 can veto pruning from quality cooldown.
- [x] `go test ./...` passes.

## Implementation Notes

- `internal/toolprune` now has a conservative always-keep class for shell,
  edit, read, safety, browser, and MCP tools, plus `tool_prune_always_keep`
  for exact project-specific tool names.
- The proxy now keys pruning by real session id with request-id fallback,
  archives removed definitions, reattaches mentioned tools, and retries once
  with the pre-prune full schema on conservative missing-tool 4xx errors.
- A missing-tool fallback records miss/retry telemetry and disables future
  pruning for that session bucket.
- `/admin/status.tool_prune`, debug/flight summaries, and `slimference gain
  --proxy` now expose pruned-tool, saved-token, reattach, miss, retry,
  always-keep, and disabled-session counters.
- The T149 planner receives `ToolPruneCooldown` and reports
  `quality_cooldown_soften_layer4` when Layer 4 is in cooldown.

## Verification

- `go test ./internal/toolprune ./internal/planner ./internal/debug ./internal/analytics ./internal/proxy -count=1`
- `go test ./cmd/slimference -run 'TestHandleSubcommand_gain_proxy|TestHandleSubcommand_gain' -count=1`
- `go test ./...`

## Non-Goals

- No predictive semantic tool router before live corpus proof.
- No cross-session user profiling.
- No pruning of unknown tool schemas without archive-backed restoration.
