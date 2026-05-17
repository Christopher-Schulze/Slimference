# TASK 179: In-flight JSON canonicalize on streaming output

Status: TODO (planning 2026-05-16)
Priority: P3 (small but free)
Scope: `internal/proxy/streaming.go`, `internal/outstop/jsonnorm/` (new)

## Why

LLM outputs containing JSON-in-code-fences often have redundant whitespace, trailing commas, formatted with newlines that the consumer (Codex parser) doesn't need. Canonicalising as the stream flows reduces bytes by 5-15% on tool-call-heavy interactions.

**Why:** Free, deterministic, on hot path. Saves output tokens with zero quality loss when the JSON is structurally valid.
**How to apply:** Detect ``` ```json ``` blocks in the stream; canonicalise their content via `json.Compact` as they flow.

## Target State

1. New `internal/outstop/jsonnorm/` with a streaming canonicaliser.
2. Triggered only when the model is mid-`json` code block in the response stream.
3. Falls back to passthrough if JSON is malformed (don't break the stream).
4. Telemetry: bytes-saved counter.

## Acceptance

- `{ "key": "value" }` (with spaces) → `{"key":"value"}` in output.
- Malformed JSON inside the block → passthrough unchanged.
- Code blocks in non-JSON languages unchanged.

## Sub-Tasks

- [ ] State machine to detect json fence open/close.
- [ ] Buffered canonicalisation per block.
- [ ] Tests.

## Notes

- Quality risk: zero (canonicalised JSON is byte-equivalent to original for any parser).

## Deviations

(none yet)
