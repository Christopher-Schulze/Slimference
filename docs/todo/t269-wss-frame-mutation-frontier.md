# T269 - WSS frame-level mutation frontier

## Why

WSS Phase-F is the Codex product path. Expanding mutation beyond known Phase-F
tool-output shapes could unlock more savings, but frame-level mistakes can break
protocol state or model memory. The frontier must be inspect-first and
version-gated.

## Current reality check

- WSS Phase-F request mutation is proven for known tool-output shapes.
- Unknown frames fail open or bridge byte-equal.
- Per-message deflate mutation is implemented for certified tuples.
- Beyond Phase-F, no mutation is safe without real captures and fixtures.
- Offline hardening done:
  - inspect-only summaries now include top-level field types and a stable
    content-free shape hash
  - `wscompact.ShapeRegistry` stores observed shapes with count, mutation
    eligibility, and fallback behavior
  - inspected shapes are bound to the WebSocket route, so non-Codex JSON cannot
    promote Codex mutation confidence
  - generic JSON no longer counts as mutation-capable for planner gating; only
    registered Phase-F request/response shapes can mark WSS mutation shape-known
  - unknown or inspect-only shapes remain byte-equal bridge candidates

## Product target

Only mutate WSS frames whose schema is registered, version-bound, fixture-proven,
and live-certified. Everything else is byte-equal bridge. The system should gain
coverage over time without ever guessing frame semantics.

## Technical work packages

1. Build WSS frame registry:
   - route
   - Codex version tuple
   - frame kind
   - known fields
   - mutation eligibility
   - fallback behavior
   - current offline state: route, frame kind, known top-level field names/types,
     content-free shape hash, mutation eligibility, count, and fallback behavior
     are implemented; Codex version tuple promotion remains live-capture gated
2. Add inspect-only mode for new shapes:
   - content-free shape hash
   - field names/types
   - size
   - no raw payload
   - current offline state: implemented for decoded text JSON frames; raw payload
     is not stored by the registry
3. Add fixture promotion workflow:
   - captured shape to synthetic fixture
   - reducer replay
   - lost=0
   - parse/degrade/compression errors zero
4. Add version drift handling:
   - unknown tuple starts bridge
   - auto-recert tries safe proof
   - TUI shows bridge vs savings active
   - current offline state: planner now treats inspect-only/new JSON as
     shape-unknown for mutation, so drift cannot promote mutation by observation
     alone
5. Add protocol tests:
   - compressed RSV frames
   - non-envelope text
   - malformed object
   - partial frames
   - upstream close/error

## Zero product-drawdown gates

- No frame mutation without registered schema and current version proof.
- Any parse uncertainty bridges byte-equal.
- Response-terminal frames cannot be shortened unless terminal-safe proof exists.
- Voice/realtime/browser/computer-use sideband frames cannot enter compression.

## Savings targets

- No fake target for unknown frames. Savings are accepted only after shape proof.
- Existing Phase-F savings must not regress.
- Degraded sessions must remain zero on certified shapes.

## Verification

- Unit fixtures for every registered frame shape.
- Live CLI/Desktop WSS smoke for every promoted tuple.
- Per-message deflate regression.
- Recert drift simulation.
- Sideband bypass tests.
- Offline tests currently cover:
  - content-free shape hashes and top-level field-type signatures
  - inspect-only JSON staying non-mutation-capable
  - registered Phase-F request shapes becoming mutation-capable
  - planner bridge using mutation-capable registry state rather than arbitrary
    observed JSON

## Done

The WSS frontier is maxxed when new shapes can be safely discovered, shadowed,
fixture-promoted, live-certified, and auto-fallbacked without ever mutating
unknown protocol state.
