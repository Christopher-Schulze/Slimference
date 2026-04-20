# T62 - Anthropic-Version-Header Negotiation + Conservative-Mode-Fallback

Status: todo
Priority: P2
Scope: `internal/proxy/provider.go`, `internal/proxy/handler.go`, `internal/config/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Anthropic requests carry `anthropic-version: 2023-06-01` (or newer).
Slimference does not inspect the header; it decodes request bodies
based on format assumptions that are valid for current versions. If
Anthropic ships a new `anthropic-version` with a format change
(content-block rework, tool-use redesign), Slimference's L1 pipeline
silently mis-parses and the silent-drop cases documented in T41 become
the norm.

Fix: **version-negotiation** at request ingress. Known-good versions
run full compression; unknown/newer versions run **conservative mode**
(passthrough with minimal L1 steps that are format-agnostic: ANSI
strip, image stubbing by byte-size, whitespace trim). Log an
`slog.Warn` once per unknown version per hour so we notice the drift
and can ship an updated parser.

## Current State

- Header value is forwarded upstream untouched.
- No version-aware dispatch.

## Target State

- Whitelist of known-supported versions in config (default:
  `["2023-06-01"]` plus the currently-tested newer ones).
- Unknown version → `conservative` parser path:
  - ANSI strip (format-agnostic)
  - image stubbing by content-type sniff (format-agnostic)
  - no tool-result dedup, no structure-extract, no delta
- Metric: `slim_anthropic_version_unknown_total` per version value.
- Config override to force-enable full pipeline on an "unknown" version
  (user explicitly opts into risk).

## Design

### Config

`[proxy.anthropic_version]`:

```toml
supported = ["2023-06-01"]
conservative_on_unknown = true
warn_on_unknown_interval = "1h"
unsupported_behavior = "conservative"  # "conservative" | "passthrough" | "error"
```

`unsupported_behavior`:

| Mode | Behaviour |
|------|-----------|
| `conservative` | run format-agnostic L1 steps only (default) |
| `passthrough`  | no L1 at all, straight to upstream |
| `error`        | return 412 Precondition Failed (defensive) |

### Dispatch

```go
func (h *Handler) dispatch(req *http.Request) PipelineMode {
    v := req.Header.Get("anthropic-version")
    for _, s := range h.cfg.AnthropicVersionSupported {
        if v == s { return PipelineFull }
    }
    if h.cfg.ConservativeOnUnknown {
        h.warnOnce(v)
        return PipelineConservative
    }
    return PipelineFull  // user opted into risk
}
```

### Conservative pipeline

`internal/compression/conservative.go`:

- ANSI strip on text nodes identified by JSON path pattern (schema-agnostic).
- Image block identified by `"type":"image"` key existence without
  assuming deeper schema.
- No union-type aware compression.

### Warn-once rate-limited

Re-use `internal/slogutil` rate-limited logger (same utility as T42).

### Metrics

- `anthropic_version_total{version="..."}` counter per observed
  version.
- `anthropic_version_unknown` counter.

### OpenAI parity

Same design applied to OpenAI's `openai-version` / `api-version` if
present. Covered in a follow-up TASK; this one focuses on Anthropic.

## Implementation Plan

### WP1 - Version whitelist config + ENV override.
### WP2 - Dispatch function in handler.
### WP3 - Conservative pipeline stub + tests.
### WP4 - Rate-limited warn + metrics.
### WP5 - Docs: spec+.md + `docs/documentation.md` §16 Provider-Invisibility.
### WP6 - Tests
- Known version → full pipeline.
- Unknown version → conservative.
- `unsupported_behavior = error` returns 412.
- Warn rate-limited to once per hour per version.

---

## Subtasks

- [ ] Config fields + defaults + ENV.
- [ ] Dispatch + PipelineMode enum.
- [ ] Conservative pipeline.
- [ ] Metrics + rate-limited warn.
- [ ] Unit tests per mode.
- [ ] Integration test with synthetic unknown version header.
- [ ] Spec + docs updates.

## Risks

- Conservative mode still breaks on radical format change. Accepted:
  better to compress less than to mis-compress.
- Version string typos from clients: treat as unknown, warn once.

## Acceptance Criteria

- [ ] Unknown version defaults to conservative pipeline.
- [ ] Warn fires once per hour per unique unknown version.
- [ ] Metric counter reflects each observed version.
- [ ] `unsupported_behavior = error` returns 412.
- [ ] `go test -race ./internal/proxy/...` green.

## Out of Scope

- OpenAI header negotiation (separate TASK).
- Auto-adding new versions to whitelist based on observed success.

---

## Validation

```
go test -race ./internal/proxy/...
curl -H 'anthropic-version: 2030-01-01' http://127.0.0.1:8990/v1/messages ...
# expect conservative pipeline + warn log
```
