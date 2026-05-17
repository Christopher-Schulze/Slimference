# TASK 181: Per-tool output-token budget injection

Status: TODO (planning 2026-05-16)
Priority: P1 (large output-token win, novel)
Scope: `internal/proxy/handler.go`, `internal/proxy/provider.go`, `internal/config/config.go`

## Why

Different tools warrant different output budgets. A reply that follows a `Bash(ls)` should be ≤200 tokens (model has nothing to add). A reply that follows `Bash(cat src/foo.go)` may need 500-2000 tokens (model is analysing). A reply after `apply_patch` should be ≤50 tokens (acknowledgement).

Currently the model has no instruction about output length; it produces however much it wants. Injecting a per-tool `max_tokens` hint or post-tool system message constrains output proportionally.

**Why:** Tool-heavy workflows generate 30-50% of output tokens in "acknowledgement / analysis" replies after each tool. Per-tool budget caps this losslessly.
**How to apply:** Track the last tool call in the conversation. When the next request is the assistant's reply, inject either `max_tokens` (Anthropic) or system-message guidance ("Reply ≤N tokens after this tool result").

## Target State

1. New `internal/proxy/output_budget.go` with a per-tool budget table:
   ```
   ls / pwd / tree / find        -> 100 tokens
   git status / git diff (short) -> 200 tokens
   Read short file               -> 200 tokens
   Read long file                -> 500 tokens
   apply_patch / Write / Edit    -> 50 tokens
   Bash (other)                  -> 500 tokens
   default                       -> unset (model's choice)
   ```
2. Detect the most-recent tool call from the conversation.
3. Inject `max_tokens` for Anthropic responses or a system-message hint for OpenAI/Codex.
4. Telemetry: per-tool average output token; auto-tune the budget table from observation.

## Acceptance

- After `Bash(ls)`: model's next reply is ≤120 tokens (5% margin over budget).
- After `Bash(cat large.go)`: model's next reply is ≤600 tokens.
- After `apply_patch`: model's next reply is ≤60 tokens.
- Quality A/B: task-completion accuracy ≥95% of no-budget baseline.
- Live session shows ≥15% output-token reduction in tool-heavy workflows.

## Sub-Tasks

- [ ] Initial budget table (curated).
- [ ] Last-tool detector.
- [ ] Per-provider budget injection.
- [ ] Auto-tune: observe actual output, adjust budgets if 95th percentile of useful output < budget.
- [ ] Quality A/B harness reuse from t169.
- [ ] CLI: `slimference budget show|edit|reset`.

## Notes

- High win, real risk: cap can amputate useful content if the model genuinely needs more. Mitigation: only cap for tools where short reply is the norm.
- The budget table is the **opinion** layer: needs operator tuning per workflow.

## Deviations

(none yet)
