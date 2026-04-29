# TASK 82: `slimference compress-preview` CLI

Status: todo
Priority: P1
Scope: `cmd/slimference/`, `internal/proxy/handler.go`, `internal/compression/`
Driver: There is no way to ask "what would the proxy do to this body?" without sending the body upstream. Debugging a compression complaint or comparing two compression configs requires a dry-run that produces the rewritten body without making a paid API call.

---

## Problem

When a user reports "the model gave a wrong answer after Slimference was installed", today there is no way to reproduce the exact compressed body the proxy sent. Operators have to pull `RequestSummary` JSONL, reconstruct what was compressed, and compare. There is also no way to A/B two compression configs against the same input without two real upstream calls.

## Target State

`slimference compress-preview [--config <path>] [--provider claude|openai|codex_chatgpt] [--diff] [-]` reads a request body from stdin or a path, runs the full Layer 0 (when applicable) + Layer 1 + Layer 2 + Layer 3 pipeline against it locally, and prints either:

- the rewritten body verbatim,
- a unified diff against the original,
- or a JSON envelope with per-layer attributions.

No upstream call is made. No analytics are written. This is purely a local transform.

## Implementation Plan

### WP1 - Pipeline harness
- Factor the request rewrite path in `internal/proxy/handler.go` into a function that accepts a body + provider and returns the rewritten body + per-layer attribution. The handler keeps using it for live traffic.
- Layer 2 in preview mode uses the `nop` summarizer by default unless `--with-l2-live` is passed (which would call MiniMax).

### WP2 - CLI
- `cmd/slimference/preview_cmd.go` with `--diff`, `--json`, `--provider`, `--with-l2-live`.
- Stdin or path; default stdin.
- `--config <path>` lets the operator A/B configs by pointing at a different TOML.

### WP3 - Tests
- Golden-file tests: known body + known config -> known output.

### WP4 - Docs
- Add a "Debugging compression complaints" section in `docs/integration.md`.

## Acceptance Criteria

- [ ] `slimference compress-preview` rewrites a body from stdin without calling upstream.
- [ ] `--diff` emits a unified diff that matches what the live proxy would produce.
- [ ] `--json` envelope contains per-layer attribution and token counts.
- [ ] No analytics records are written during preview.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Sending the preview to upstream (use `--with-l2-live` only if explicitly requested; never auto-call upstream).
- Editing the body interactively (would be a separate UI tool).

## Validation

```
cat fixture.json | slimference compress-preview --provider=claude --diff
slimference compress-preview --provider=codex_chatgpt --json < tests/fixtures/codex/v1-responses-input.json
```
