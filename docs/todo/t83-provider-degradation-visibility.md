# TASK 83: Provider degradation visibility

Status: todo
Priority: P1
Scope: `internal/summarization/`, `internal/proxy/proxy.go`, `internal/admin/`, `internal/tui/`, `cmd/slimference/`
Driver: When MiniMax is slow or down, FallbackChain silently disables Layer 2. The user sees nothing. Compression ratio drops, savings disappear, and there is no signal that something is wrong with an external provider.

---

## Problem

`internal/summarization/fallback.go` disables Layer 2 transparently when its primary provider fails. That is the right safety behaviour, but it produces a silent quality cliff. Operators only notice when they look at savings the next morning. Same applies to other external dependencies: prompt-cache hits dropping because Anthropic rolled out a model rotation, MiniMax rate-limit caps biting under heavier load, etc.

## Target State

A single, consolidated provider-health view:

- `/admin/status.providers` exposes per-provider state (`healthy`, `degraded`, `down`), last error, last success timestamp, current rate-limit remaining where the upstream exposes it, and circuit-breaker state.
- TUI surfaces a banner when any provider is non-healthy.
- `slimference watch` (T79) shows provider state in its compact view.
- Optional desktop notification on transition to `degraded`/`down` (`[notifications] on_degradation = true`, default off).
- Rate-limited slog warn at most once per minute per provider.

## Implementation Plan

### WP1 - Health state machine
- New `internal/providerhealth/` package: rolling success/error window per provider, circuit-breaker style state machine.
- Inputs: HTTP status codes, timeouts, parse errors, rate-limit headers.

### WP2 - Wiring
- MiniMax client, OpenAI/Codex passthrough, Anthropic prompt-cache analytics all report into the same health pipeline.
- Layer 2 fallback chain reads health to pick the active provider.

### WP3 - Surfaces
- Admin endpoint, TUI banner, `slimference watch` integration, optional `osascript`-based macOS notification.

### WP4 - Tests
- Unit tests for the state machine.
- Integration test: simulate a MiniMax 503 storm, assert `degraded` state surfaces in the admin endpoint within `[providerhealth] window_seconds`.

## Acceptance Criteria

- [ ] `/admin/status.providers` exposes per-provider health and reasons.
- [ ] TUI shows a banner when any provider is non-healthy.
- [ ] Layer-2 silent disable now includes a non-silent state in `/admin/status`.
- [ ] Notification opt-in works on macOS without forcing a dependency for users who do not want it.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- SLO dashboards / Prometheus exposition (separate track).
- Auto-failover to a different paid provider (manual today).

## Validation

```
go test ./internal/providerhealth/...
slimference watch --interval=1s
curl localhost:8990/admin/status | jq .providers
```
