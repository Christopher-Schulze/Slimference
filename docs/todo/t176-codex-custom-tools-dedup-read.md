# TASK 176: Speculative — Codex custom tool definitions for dedup-Read

Status: TODO (planning 2026-05-16, speculative)
Priority: P3 (large potential, depends on Codex contract details)
Scope: `internal/hooks/codex.go`, new `internal/customtools/`, `cmd/slimference/integrate_cmd.go`

## Why

Codex 0.130 has the most modern hook surface of any agent CLI. It supports user-defined tools via configuration. Speculative idea: register Slimference-provided tools the model can call:

- `slimference_read_delta(path, since_turn)` — returns only the diff since the last full read
- `slimference_dedup_read(path, hash)` — returns "unchanged: hash matches" without re-reading

If the agent learns to call these (via the system-prompt awareness preamble) it can drop full file content from its context entirely, replaced by a tiny hash-lookup.

**Why:** Largest potential structural change. Saves not 10% but 50%+ on file-heavy workflows. **But:** requires Codex to honour custom tool registrations AND the model to learn to prefer them over `Read`. High R&D, high reward.
**How to apply:** Register the tools via Codex's MCP / custom-tool path. Wire them through `slimference rewrite` so the actual implementation lives in the proxy.

## Target State

1. New `internal/customtools/` package implementing the tool handlers.
2. Codex integration: register tools at `slimference integrate install codex --enable-custom-tools`.
3. SessionStart awareness preamble nudge: "When you need to re-read a file you read recently, prefer slimference_dedup_read."
4. End-to-end e2e on a captured-session corpus measuring adoption rate (did the model actually call them?).

## Acceptance

- The tools are registered in Codex and callable.
- A live session shows ≥10% adoption rate (i.e., 10% of file-reads use slimference_dedup_read).
- Quality A/B: no measurable regression.
- Configurable opt-in only — never default on until validated at scale.

## Sub-Tasks

- [ ] Research Codex 0.130's exact custom-tool registration mechanism (MCP? built-in tools.json?).
- [ ] Implement tool handlers.
- [ ] Codex integration patch.
- [ ] Awareness preamble update.
- [ ] Adoption-rate telemetry.
- [ ] Long-running A/B test.

## Notes

- This is the **most ambitious** task in this batch. Largest reward, highest uncertainty.
- Depends entirely on Codex's hook contract details — if they don't allow proxy-injected tools, fall back to system-prompt prompting only.
- Schedule after t165-t174 land so we have a stable baseline.

## Deviations

(none yet)
