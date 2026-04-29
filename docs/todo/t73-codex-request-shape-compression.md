# T73 - Codex Request-Shape Compression Support

Status: todo
Priority: P1
Scope: `internal/proxy/provider.go`, `internal/proxy/proxy.go`, `internal/proxy/handler.go`, `internal/types/`, `tests/fixtures/`, `docs/integration.md`
Driver: Current Codex support routes traffic, but Codex request bodies are not yet proven compressible by Layer 1-3.

---

## Problem

Current routing support is necessary but not sufficient:

- `detectProviderWithUA` can classify Codex paths / User-Agent.
- `upstreamURL` can route `types.CodexChatGPT` to `https://chatgpt.com`.
- `isCompressiblePath` only returns true for `/v1/messages` and
  `/v1/chat/completions`.
- Codex `/v1/responses` and `/backend-api/codex/*` paths therefore fall into
  passthrough, so Layer 1-3 do not reduce request-body history there.
- `extractMessages` and `reconstructBody` do not have a Codex provider branch.

Layer 0 hooks can still reduce shell output before Codex records it, but the
proxy-side post-entry compression stack is not first-class for Codex yet.

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
- Capture scrubbed examples for:
  - Codex `/v1/responses` via `openai_base_url`
  - Codex `/backend-api/codex/responses` via `chatgpt_base_url`
  - streaming and non-streaming variants if both exist
- Store under `tests/fixtures/codex/`.
- Redact Authorization, account IDs, conversation IDs, and prompt content.

### WP2 - Decide compressible paths
- Extend `isCompressiblePath` to include Codex paths only after fixtures prove
  the request body has extractable conversation history.
- If a path is operational metadata, keep passthrough.

### WP3 - Add Codex extraction
- Add a Codex provider branch in `extractMessages`.
- Map Codex input/history/tool structures into `types.Message`.
- Preserve raw blocks so reconstruction can be exact when no compression fires.
- Return a typed "unsupported shape" signal that causes passthrough rather than
  user-visible failure.

### WP4 - Add Codex reconstruction
- Add a Codex provider branch in `reconstructBody`.
- Preserve Codex-specific fields and body-level metadata.
- Golden tests:
  - all layers off -> byte/canonical JSON equal
  - layer disabled/provider disabled -> passthrough
  - known tool-result body -> shorter body when compression applies

### WP5 - Wire analytics and debug
- Tag provider as `codex_chatgpt`.
- Ensure layer breakdown, saved tokens, prompt-cache stats, and recent requests
  show Codex separately.

### WP6 - Safety gates
- Unknown Codex version/shape must emit a warning and passthrough.
- Add fuzz-ish malformed-body tests.

## Acceptance Criteria

- [ ] Codex `/v1/responses` fixture enters compression when shape is supported.
- [ ] Codex `/backend-api/codex/*` fixture enters compression only for known
      conversation-body routes.
- [ ] Unknown Codex shapes passthrough without 400.
- [ ] All auth/session headers are forwarded verbatim.
- [ ] All body fields not owned by Slimference survive reconstruction.
- [ ] Codex savings appear under provider `codex_chatgpt` in analytics/debug.
- [ ] `go test -race ./internal/proxy/... ./internal/types/...` green.

## Out of Scope

- TLS fingerprint mimicry.
- Codex auth/token refresh.
- Compressing opaque binary or encrypted payloads.

## Validation

```
go test -race ./internal/proxy/... ./internal/types/...
go test ./internal/proxy -run Codex
go run ./scripts/benchmarks session-report tests/fixtures/codex/*.jsonl
```
