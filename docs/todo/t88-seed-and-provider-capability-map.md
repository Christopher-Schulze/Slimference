# TASK 88: Seed-aware request building + provider capability map

Status: todo
Priority: P2
Scope: `internal/summarization/`, `internal/config/`, `internal/types/`
Driver: `temperature=0` is set on MiniMax for determinism, but seed is missing. Other OpenAI-style providers in the FallbackChain are not deterministic without `seed`, so a future fallback to OpenAI would silently break reproducibility.

---

## Problem

Determinism is currently assumed because temperature is forced to 0. That is enough for MiniMax in practice but it is a fragile contract. When the FallbackChain rotates to another OpenAI-compatible provider that is not strictly greedy at T=0, summaries become non-reproducible. There is no single source of truth describing which providers support which determinism levers (`seed`, `top_logprobs`, `n=1`, `tool_choice=none`).

## Target State

- A `ProviderCapabilities` struct keyed by provider id describes `supports_seed`, `supports_temperature_zero`, `supports_logprobs`, `tokenizer`, etc.
- The summarization request builder reads the capability map and:
  - sets `seed = <stable per-session>` when supported;
  - fails closed (skips the provider in the chain) when capabilities for strict determinism are not met and `[summarization] require_deterministic = true`.
- A bootstrap in `slimference doctor` verifies the active provider's capabilities are sufficient for the current config.

## Implementation Plan

### WP1 - Capability struct
- `internal/types/provider_caps.go` with the struct, JSON-tagged.
- Built-in defaults for MiniMax, OpenAI, Anthropic.

### WP2 - Request builder
- `MiniMaxClient` and any fallback client read caps from a shared registry.
- Seed is computed from a stable hash over `(session_id, summary_window_hash)`.

### WP3 - Doctor check
- `slimference doctor` adds a "summarization determinism" section.

### WP4 - Tests
- Unit: capability mismatch + `require_deterministic=true` skips the provider.
- Snapshot: request payload contains seed when supported, omits when not.

## Acceptance Criteria

- [ ] Capability map is the only place that declares per-provider determinism levers.
- [ ] Seed is set per-session for providers that support it.
- [ ] `require_deterministic=true` causes the chain to skip incapable providers.
- [ ] `slimference doctor` warns when the active provider lacks required levers.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Implementing a new provider client (this task is about contract, not new providers).
- Hashing the entire conversation; session-id + window hash is enough.

## Validation

```
go test ./internal/summarization/... ./internal/types/...
slimference doctor
```
