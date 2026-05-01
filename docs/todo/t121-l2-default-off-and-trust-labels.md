# TASK 121: Layer 2 default-off + opt-in flow + provider trust labelling

Status: DONE 2026-05-01
Priority: P0
Scope: `internal/config/defaults.go`, `internal/summarization/`, `internal/proxy/`, `cmd/slimference/`, `internal/types/provider_caps.go`, `docs/data-policy.md` (new)
Driver: Layer 2 by default ships full conversation prefixes (containing source code, tool outputs, paths, error messages, potentially auth tokens) to `api.minimax.io` (a third-party provider hosted in PRC). The current default is `enabled=true`. T109 adds redaction; T121 makes the default explicit-opt-in and exposes a provider trust model so the operator knows what they're agreeing to. Together with T109 these are the prerequisites for shipping Layer 2 in any production deployment with a data-policy obligation.

---

## Problem

`internal/config/defaults.go` (current):
```toml
[compression]
layer2_enabled = true
```

`internal/summarization/minimax.go` (current):
- Default model `minimax-m2.7`
- Default base URL `https://api.minimax.io/v1`
- API key from `MINIMAX_API_KEY` env var
- No provider trust label visible anywhere
- No `slimference doctor` warning that this is an external provider
- No explicit opt-in checkpoint during install

A user installing Slimference with default config + no awareness of MiniMax has no idea their next 100 coding sessions will be transmitted to a third-party API. This is a data-policy and trust failure independent of whether the redaction (T109) catches everything.

## Target State

Three deliverables:

1. **Default-off**: `layer2_enabled = false` becomes the new default. Existing operators with explicit `layer2_enabled = true` are unaffected. New installs need an explicit opt-in step.
2. **Provider trust labels**: `internal/types/provider_caps.go` gains a `TrustClass` field with values `upstream_provider` (the model the user is talking to - Anthropic, OpenAI), `external_third_party` (a side-channel optimisation provider the user did not ask to talk to - MiniMax). `slimference doctor` warns when any `external_third_party` provider is enabled.
3. **Explicit opt-in flow**: `slimference layer2 enable [--provider=minimax] --acknowledge-data-policy` is the only way to flip the flag from the default. The CLI prints a one-screen data policy explanation, requires `--acknowledge-data-policy` to confirm, then writes the config update. `slimference layer2 status` reports current state.

Companion artifacts:

- `docs/data-policy.md` - human-readable description of what gets sent where, when, and how to disable / replace.
- TUI clearly labels Layer 2 as "external" when a `external_third_party` provider is configured.

## Implementation Plan

### WP1 - Default flip
- `internal/config/defaults.go` -> `layer2_enabled = false`.
- Migration: when an existing operator's config does NOT contain `layer2_enabled` AND prior runtime state shows L2 was used, write a migration warning to logs but **do NOT auto-flip**. They explicitly opted in via prior config; respect that.

### WP2 - TrustClass on capability registry
- `types.ProviderCapabilities` extended with `TrustClass string` (`"upstream_provider"` | `"external_third_party"` | `"unknown"`).
- Anthropic / OpenAI / Codex marked as `upstream_provider`.
- MiniMax marked as `external_third_party`.
- Future-proofing: any newly-added provider must declare its TrustClass or the registry rejects it.

### WP3 - Doctor warnings
- `slimference doctor` walks active providers, prints a coloured WARN block for each `external_third_party` that is enabled.
- The warning text names the provider, the data flow, and the disable path: `slimference layer2 disable`.
- Exit code unchanged (warning, not error) unless `--strict` flag passed.

### WP4 - Opt-in CLI flow
- `cmd/slimference/layer2_cmd.go` (new): subcommands `enable`, `disable`, `status`, `provider list`, `provider set <name>`.
- `enable` prints data-policy explanation, requires `--acknowledge-data-policy`, then writes config.
- Without ack, exits with the explanation + a one-line how-to.

### WP5 - Data policy doc
- `docs/data-policy.md` covers:
  - What gets sent where (per-layer breakdown).
  - Redaction guarantees (links to T109 design).
  - How to disable each layer.
  - How to swap MiniMax for a self-hosted alternative (model + endpoint config).
  - Pointer to upstream provider terms.

### WP6 - TUI labels
- TUI Stats / Layer view annotates each `external_third_party` provider with `[external]` and a tooltip linking to `slimference layer2 status`.
- Master switch / bypass affordances unchanged in mechanic; only the labels change.

### WP7 - Optional self-host hooks
- `[compression.minimax] base_url` already configurable; document an example for self-hosted MiniMax / OpenAI-compatible model.
- Trust label per `base_url` regex match: a self-hosted endpoint becomes `upstream_provider` once the operator labels it via `[compression.minimax] trust_class = "upstream_provider"`.

### WP8 - Tests
- Default-config check: `Layer2Enabled == false`.
- Doctor output matrix: external + enabled -> WARN; external + disabled -> OK; upstream + enabled -> OK.
- Opt-in CLI: missing ack flag -> exit 2 + explanation; with ack -> config written.
- Migration: pre-existing `layer2_enabled = true` in config preserved.

## Acceptance Criteria

- [x] `layer2_enabled = false` is the new default.
- [x] `TrustClass` declared on every registered provider.
- [x] `slimference doctor` warns on enabled `external_third_party` providers.
- [x] `slimference layer2 enable --acknowledge-data-policy` is the only path to enable from default.
- [x] `docs/data-policy.md` shipped + linked from README.
- [x] TUI labels external providers.
- [x] Coverage 100%; race tests green.

## Out of Scope

- Removing MiniMax support entirely (operator may have a relationship).
- Encrypting the body to MiniMax beyond TLS (TLS already in place; deeper E2E encryption is a separate spec discussion).
- Auto-redirecting to a self-hosted alternative (operator chooses).

## Validation

```
go test -race ./internal/config/... ./internal/summarization/... ./internal/proxy/... ./cmd/slimference/...
slimference doctor   # manual verification of warning surface
slimference layer2 enable    # without ack -> should refuse
slimference layer2 enable --acknowledge-data-policy   # should succeed and update config
```
