# T41 - `extractMessages` Union-Type Hardening

Status: closed (spec premise inaccurate)
Priority: n/a

## 2026-04-20 Closure Note

Code verification after writing this spec revealed that the premise was wrong.
`internal/proxy/handler.go::handleCompressibleRequest` already surfaces
extraction errors as `http.StatusBadRequest` with `slog.Error`:

```go
messages, rawBody, err := extractMessages(provider, body)
if err != nil {
    slog.Error("extract messages", "error", err)
    p.proxyError(w, http.StatusBadRequest, fmt.Sprintf("parse request: %v", err))
    return
}
```

That is the **opposite of silent-drop** - it is loud and visible. There is no
silent-drop to fix.

The genuine risk in `extractMessages` is **round-trip fidelity** when
Anthropic adds new content-block variants that are not in
`anthropicContentBlock` struct: unknown fields are lost on reconstruction via
`json.Marshal(ab)`. That risk is best addressed by T62
(Anthropic-Version-Header Negotiation + Conservative-Mode-Fallback), which
already plans to short-circuit the L1 pipeline on unknown versions.

Decision: close T41 as no-op, fold the round-trip concern into T62's
conservative-pipeline implementation so unknown formats pass through
byte-equal.

Lesson for later specs: verify each premise against the code before writing a
task. Audit-derived specs without code-grep are unreliable.

---

# Original specification below (kept for historical reference)

Status: todo
Priority: P0
Scope: `internal/proxy/provider.go`, `internal/proxy/handler.go`, `tests/integration/`, `internal/slogutil`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`extractMessages` in `internal/proxy/provider.go` decodes Anthropic/OpenAI
request bodies where `message.content` is a JSON union: either a raw string or
an array of typed content blocks (`text`, `tool_result`, `tool_use`, `image`,
`document`). Today a parse failure on a single malformed block causes the
whole message to be silently skipped from Layer 1 processing. The request is
still forwarded upstream (good), but:

1. No log record explains why compression did nothing.
2. Analytics reports a compressed count that is lower than reality, which
   corrupts `docs/benchmarks.md` baselines.
3. If Anthropic introduces a new content-block variant, Slimference will
   appear to "work" while actually degrading to passthrough for every
   affected message - invisible regression.

The silent-drop is a violation of the ABSOLUTE rule "Anti-Sycophancy / no
silent failures in the hot path" from `~/.claude/CLAUDE.md`.

## Current State

- `extractMessages` returns `([]Message, error)` but the handler ignores the
  error for non-critical parse failures and continues with partial data.
- No slog event is emitted for partial extraction.
- `testExtractMessages_malformedBlock` does not exist.

## Target State

- Every union-type parse failure is logged at `slog.Warn` with
  `event=extract_message_partial`, fields: `provider`, `message_index`,
  `block_index`, `block_type`, `err`, `raw_snippet` (truncated to 240 bytes,
  secrets-redacted through `internal/security.Redact`).
- If more than `N` messages (default `N=1`) fail to extract in a single
  request, handler switches to **explicit passthrough** (no L1/L2), emits
  `slog.Error` with `event=extract_message_degrade`, and increments a
  prom-style counter `slim_extract_degrade_total`.
- Analytics snapshot gains a new field `extract_partial_count` and
  `extract_degrade_count`.
- TUI Stats view renders a red `!` next to "L1 Compression" when the degrade
  counter is non-zero in the current session.

## Design

### Config surface

Add `[proxy.extraction]` section to `internal/config/config.go`:

| Field | Type | Default | Semantic |
|-------|------|---------|----------|
| `max_partial_per_request` | int | 1 | trigger threshold for degrade |
| `log_raw_snippet_bytes`   | int | 240 | snippet cap in log |
| `redact_in_logs`          | bool | true | run through security.Redact |

ENV overrides: `SLIMFERENCE_EXTRACTION_MAX_PARTIAL`,
`SLIMFERENCE_EXTRACTION_LOG_SNIPPET_BYTES`.

### Code changes

1. `extractMessages` returns new struct `ExtractionResult { Messages,
   PartialCount, PartialDetails []PartialBlock }`.
2. `handler.processRequest`:
   - counts partials
   - if `>= max_partial_per_request`: bypass L1 queue, set
     `ctx.compressPolicy = passthrough`, continue upstream.
3. `internal/analytics/collector.go`: two new `atomic.Int64` counters,
   snapshotted every tick.
4. `internal/tui/model.go`: subscribe to counter, render in `renderStats`.

### Error semantics

| Scenario | Level | Fallback |
|----------|-------|----------|
| 1 partial block | Warn | compress rest, include valid blocks |
| >= max_partial  | Error | full passthrough, no L1/L2 |
| Root JSON parse fails | Error | 502 to client (unchanged) |

## Implementation Plan

### WP1 - Extraction surface refactor
- Introduce `ExtractionResult` struct in `internal/proxy/provider.go`.
- Migrate callers (`handler.go`, `compress_job.go`).

### WP2 - Logging + metrics
- Add slog events `extract_message_partial` and `extract_message_degrade`.
- Wire counters into analytics snapshot.

### WP3 - Degrade path
- Policy flag on request context; compressors check flag and no-op.

### WP4 - TUI + admin surface
- Add badge in Stats view and JSON field in `/admin/status`.

### WP5 - Tests
- Unit: malformed `tool_result`, unknown block type, nested `content` field.
- Integration: synthetic Anthropic request with 3 malformed blocks, assert
  degrade path engaged and upstream body unchanged.

---

## Subtasks

- [ ] Introduce `ExtractionResult` struct and migrate all call sites.
- [ ] Emit `extract_message_partial` warn with redacted snippet.
- [ ] Implement `max_partial_per_request` threshold + degrade policy.
- [ ] Add analytics counters `extract_partial_count`, `extract_degrade_count`.
- [ ] Wire counters into TUI Stats + `/admin/status`.
- [ ] Unit tests for unknown/malformed block types.
- [ ] Integration test verifying passthrough on degrade.
- [ ] Update `docs/documentation.md` §5 L1 extraction semantics.

## Risks

- Threshold too aggressive -> legit-but-quirky requests degrade unnecessarily.
  Mitigation: configurable + default 1 matches "first broken block is a
  signal", raise to 2 if field-noise is seen.
- Log spam if upstream begins sending a new block type on every request.
  Mitigation: add per-hour rate-limit on `extract_message_partial` slog
  calls (re-use `internal/slogutil` rate-limiter helper).

## Acceptance Criteria

- [ ] `go test -race ./...` green including new integration test.
- [ ] Logging snapshot shows `event=extract_message_partial` on a crafted
      fixture with 1 malformed block.
- [ ] `event=extract_message_degrade` fires at threshold and upstream body
      byte-equal original.
- [ ] TUI Stats shows red `!` when counter > 0.
- [ ] `docs/documentation.md` §5 updated.

## Out of Scope

- Auto-retry with alternative decoders.
- Learning new block types at runtime.
- Client-visible error messages (client still sees upstream response).

---

## Validation

```
go test -race -count=1 ./internal/proxy/... ./internal/analytics/...
go test ./tests/integration/...
./slimference doctor
```
