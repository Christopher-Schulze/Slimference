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
2. Add inspect-only mode for new shapes:
   - content-free shape hash
   - field names/types
   - size
   - no raw payload
3. Add fixture promotion workflow:
   - captured shape to synthetic fixture
   - reducer replay
   - lost=0
   - parse/degrade/compression errors zero
4. Add version drift handling:
   - unknown tuple starts bridge
   - auto-recert tries safe proof
   - TUI shows bridge vs savings active
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

## Done

The WSS frontier is maxxed when new shapes can be safely discovered, shadowed,
fixture-promoted, live-certified, and auto-fallbacked without ever mutating
unknown protocol state.
