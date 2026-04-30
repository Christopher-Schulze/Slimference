# TASK 88: Seed-aware request building + provider capability map

Status: SHIPPED 2026-04-30 — capability struct + registry + seed/min_tokens wiring + require_deterministic chain skip + doctor warning all live.
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

- [x] Capability map is the only place that declares per-provider determinism levers (`internal/types/provider_caps.go`).
- [x] Seed is set per-session for providers that support it (T91 + T88 wire-up commit `eec9ec6`).
- [x] `require_deterministic=true` causes the chain to skip incapable providers (`FallbackChain.SetRequireDeterministic`, `IsDeterministic`).
- [x] `slimference doctor` warns when the active provider lacks required levers (Determinism gate check).
- [x] Coverage 100%; race tests green.

## Out of Scope

- Implementing a new provider client (this task is about contract, not new providers).
- Hashing the entire conversation; session-id + window hash is enough.

## Validation

```
go test ./internal/summarization/... ./internal/types/...
slimference doctor
```

## Closure Notes (2026-04-30)

Landed:

- `types.ProviderCapabilities` struct: `SupportsSeed`,
  `SupportsTemperatureZero`, `SupportsLogprobs`,
  `SupportsMinCompletionTokens`, `SupportsStopConditions`,
  `SupportsResponseID`, `SupportsCachedPrefix`. Designed additive so
  later releases can extend without breaking older configs.
- Built-in registry with defaults for `Anthropic`, `OpenAI`, and
  `CodexChatGPT`.
- `CapabilitiesFor(p)` returns a copy. Unknown providers return the
  zero value so call sites fail closed.
- `SetProviderCapabilities(p, caps)` returns a `restore` closure for
  test ergonomics.
- 100% coverage; CI green.

Deferred follow-ups:

- Wire `SupportsSeed` into the MiniMax client request builder so
  `seed` is included when the active provider supports it. Requires a
  per-session seed derivation (stable hash of session-id + window).
- Doctor check that warns when the active provider lacks the
  capabilities the active config requires
  (`[summarization] require_deterministic = true`).
- T91 (`min_completion_tokens`) becomes safely shippable now that
  `SupportsMinCompletionTokens` exists; gate the request-side wiring
  on that flag.
- T78 (provider server-state) is similarly unblocked by
  `SupportsResponseID`.
