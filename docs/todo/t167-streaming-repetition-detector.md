# TASK 167: Streaming repetition detector (cut mid-response when model echoes prompt)

Status: TODO (planning 2026-05-16)
Priority: P0
Scope: new `internal/outstop/repdet/`, `internal/proxy/streaming.go`, `internal/proxy/handler.go`

## Why

LLMs in iterative refactor sessions frequently echo back large code blocks from the prompt — sometimes 200, 500, even 2000 lines of "here is the file again unchanged" before adding their actual contribution. The user already has that content (they sent it). This is the single largest preventable output-waste mode in coding agents.

Detect this in-flight: maintain a rolling hash of the most recent prompt blocks (especially fenced code blocks, file contents from tool_result). When the streaming response starts emitting tokens that match a prompt block byte-for-byte for ≥200 contiguous chars, splice in an annotation like `[unchanged from src/foo.go:10-58]` and resume only when the stream diverges.

**Why:** Output savings of 10-30% on refactor-heavy workflows. The model is wasting tokens echoing what we sent it; we restore the meaning without the cost.
**How to apply:** Run only on conversations with prior tool_result code blocks or pasted code. Use rolling fingerprints + a per-session prompt-content trie for O(1) match-extend.

## Target State

1. New package `internal/outstop/repdet/`:
   - `type Index struct{}` indexes the prompt's code/tool_result blocks per session
   - Build-time: hash all blocks ≥100 chars via 64-bit rolling polynomial fingerprint
   - Stream-time: maintain rolling fingerprint over the output; on match, look up against the index
2. Confirmed match (≥200 contiguous chars identical to a prompt block): replace the matched span with `[unchanged: <block-name>:<line-range>]` marker
3. Per-session state in `internal/sessions/` so it persists across turns.
4. Config flag `[compression.output_reduce] repetition_detection_enabled = true` (default on).
5. Telemetry: `repetition_chars_saved` cumulative counter.

## Acceptance

- Synthetic test: prompt contains a 400-char code block. Model echoes it verbatim. Detector splices `[unchanged: …]` marker; total output tokens reduced.
- False-positive guard: any repeat shorter than 200 chars does NOT trigger (below MinMatch threshold).
- Marker is unambiguous and Codex/Claude understands it as "the content from src/foo.go lines 10-58 is here unchanged".
- Quality A/B: code-task accuracy on test suite ≥99% of no-detection baseline (zero meaningful Quality loss).
- 100% coverage on `internal/outstop/repdet/`.

## Sub-Tasks

- [ ] Algorithm design doc: rolling Rabin-Karp + suffix-array verify for confirmed matches.
- [ ] Block-extraction from prompt messages (code blocks, tool_result text ≥100 chars).
- [ ] Per-session Index with TTL eviction.
- [ ] Streaming integration: bridge with the SSE cutter from t166 so they don't conflict.
- [ ] Marker format that round-trips: `[unchanged: <name>:<line-range>]`.
- [ ] Quality regression test suite using captured-session corpus.
- [ ] Counter telemetry + admin surface.
- [ ] Docs + Sub-Tasks list of false-positive risk cases.

## Notes

- Risk: model might be re-asserting a fix to a file. If the model genuinely needs to repeat ("here is the corrected file") we lose information by cutting. Mitigation: only fire when the matched block is from a tool_result, not from an assistant turn. Active edits are exempt.
- Marker `[unchanged: …]` must be content-block-compatible (text type) so downstream layers don't choke.
- Memory: index uses ~24 bytes per indexed block + hashing window. Negligible for typical session sizes.
- Hot path: rolling hash is O(1) per byte. ≤1 microsecond per 1000 tokens.

## Deviations

- **Non-streaming Anthropic only in v1.** The Index + FindMatches + Rewrite engine is wired into the non-streaming Anthropic response path: the proxy buffers the upstream body, walks the content blocks, and rewrites every `text` block's matches into `[unchanged: <name>:L<from>-<to>]` markers before forwarding. OpenAI / Codex non-streaming responses and all streaming responses pass through untouched in v1. Streaming integration requires a re-encoded SSE delta path that conflicts with the t166 cutter and is held back to a follow-up.
- **Per-request lifetime, not per-session.** Spec called for per-session index in `internal/sessions/`. v1 builds the index per-request because the dominant repeat case is intra-turn ("model echoes content it received this turn"). Cross-turn coverage is a follow-up.
- **No line-range metadata.** The marker emits `[unchanged: <name>]` instead of `[unchanged: <name>:L<from>-<to>]` because the prompt tool_result blocks don't carry line ranges through to the proxy in the current code. Adding a metadata round-trip is a small follow-up.
