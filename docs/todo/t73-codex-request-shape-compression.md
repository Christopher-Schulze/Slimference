# T73 - Codex Request-Shape Compression Support

Status: done
Priority: P1
Scope: `internal/proxy/provider.go`, `internal/proxy/proxy.go`, `internal/proxy/handler.go`, `internal/types/`, `tests/fixtures/`, `docs/integration.md`
Driver: Current Codex support routes traffic; T73 adds offline-ready request-body compression without wiring the user's live Codex installation.

---

## Problem

Current routing support was necessary but not sufficient:

- `detectProviderWithUA` can classify Codex paths / User-Agent.
- `upstreamURL` can route `types.CodexChatGPT` to `https://chatgpt.com`.
- Before T73, `isCompressiblePath` only returned true for `/v1/messages` and
  `/v1/chat/completions`.
- Before T73, Codex `/v1/responses` and `/backend-api/codex/*` paths therefore
  fell into passthrough, so Layer 1-3 did not reduce request-body history there.
- Before T73, `extractMessages` and `reconstructBody` did not have a Codex
  provider branch.

Layer 0 hooks can still reduce shell output before Codex records it. T73 adds
the proxy-side post-entry compression stack for known Codex request bodies.

## Target State

Codex traffic has a safe, schema-aware path:

- Known Codex request shapes are identified from captured/scrubbed fixtures.
- Compressible Codex paths enter the compression pipeline only when their body
  shape is understood.
- Unknown Codex body shapes route passthrough, not 400.
- Reconstructing a Codex body preserves all unknown fields, ordering where
  practical, and auth/session metadata.
- Zero-downside token and semantic fallback applies exactly as for Anthropic and OpenAI.

## Implementation Plan

### WP1 - Capture/synthesize fixtures
- [x] Capture/synthesize scrubbed examples for:
  - Codex `/v1/responses` via `openai_base_url`
  - Codex `/backend-api/codex/responses` via `chatgpt_base_url`
  - streaming and non-streaming variants if both exist
- [x] Store under `tests/fixtures/codex/`.
- [x] Redact Authorization, account IDs, conversation IDs, and prompt content.

### WP2 - Decide compressible paths
- [x] Extend `isCompressiblePath` to include Codex paths only after fixtures prove
  the request body has extractable conversation history.
- [x] If a path is operational metadata or unknown body shape, keep passthrough.

### WP3 - Add Codex extraction
- [x] Add a Codex provider branch in `extractMessages`.
- [x] Map Codex `messages`, Responses `input`, `function_call`, and
  `function_call_output` structures into `types.Message`.
- [x] Preserve raw blocks so reconstruction can be exact when no compression fires.
- [x] Unknown/unsupported shape returns no messages and falls back to passthrough.

### WP4 - Add Codex reconstruction
- [x] Add a Codex provider branch in `reconstructBody`.
- [x] Preserve Codex-specific fields and body-level metadata.
- [x] Golden tests:
  - all layers off -> byte/canonical JSON equal
  - layer disabled/provider disabled -> passthrough
  - known tool-result body -> shorter body when compression applies

### WP5 - Wire analytics and debug
- [x] Tag provider as `codex_chatgpt`.
- [x] Ensure layer breakdown, saved tokens, prompt-cache stats, and recent requests
  show Codex separately.

### WP6 - Safety gates
- [x] Unknown Codex shape passthroughs without 400.
- [x] Add malformed/unknown-body tests through the proxy handler.

## Acceptance Criteria

- [x] Codex `/v1/responses` fixture enters compression when shape is supported.
- [x] Codex `/backend-api/codex/*` fixture enters compression only for known
      conversation-body routes.
- [x] Unknown Codex shapes passthrough without 400.
- [x] All auth/session headers are forwarded verbatim.
- [x] All body fields not owned by Slimference survive reconstruction.
- [x] Codex savings appear under provider `codex_chatgpt` in analytics/debug.
- [x] `go test -race ./internal/proxy/... ./internal/types/...` green.

## Out of Scope

- TLS fingerprint mimicry.
- Codex auth/token refresh.
- Compressing opaque binary or encrypted payloads.

## Validation

```
go test -race ./internal/proxy/... ./internal/types/...
go test ./internal/proxy -run Codex
go run ./scripts/ci
go test -race ./...
bun test tests/ts
go test -tags=integration ./tests/integration
```

## Closure Notes

- Live Codex wiring and live E2E certification are intentionally not part of
  T73; they remain T71 and must not mutate the operator's active `~/.codex`
  unless explicitly requested.
- `internal/proxy/provider.go` now has a `CodexChatGPT` branch for OpenAI-style
  `messages` and Responses-style `input`.
- `internal/proxy/proxy.go` now treats `/v1/responses` and
  `/backend-api/codex/*` as potential compression paths, then applies a
  provider-specific guard so generic OpenAI Responses traffic stays passthrough.
- `internal/proxy/handler.go` returns unknown/no-message bodies to passthrough
  instead of failing or reconstructing an empty request.
- Fixtures:
  - `tests/fixtures/codex/v1-responses-input.json`
  - `tests/fixtures/codex/backend-api-codex-responses.json`
- Final verification on 2026-04-29: `go run ./scripts/ci`,
  `go test -race ./...`, `bun test tests/ts`,
  `go test -tags=integration ./tests/integration`, and
  `go run ./scripts/benchmarks` passed.
