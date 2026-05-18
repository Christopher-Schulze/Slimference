# TASK 236: WSS terminal-safe streamcut

Status: QUEUED
Priority: P1 after T235 codec gates, before any WSS streamcut enablement
Scope: `internal/proxy/wsmitm_phasef.go`, Codex WSS response stream,
T224/T226 live certification

## Why

HTTP streamcut is safe because the HTTP/SSE relay can close the upstream stream
and emit the provider's synthetic SSE terminator. The first live WSS mutation
proof showed that applying the same idea to Codex WSS by blanking
`response.output_text.delta` frames is not safe: Slimference re-encoded frames,
but Codex CLI did not return cleanly.

That is an unacceptable savings/drift trade-off. A few saved tail tokens are not
worth a hanging Codex turn, and a hanging turn is worse than byte-equal
passthrough. WSS streamcut therefore stays disabled in the WSS Phase-F adapter
until this task proves a protocol-correct terminal strategy.

## Target State

- WSS streamcut is off by default in `wsPhaseFAdapter`.
- HTTP/SSE streamcut remains unchanged and covered by the existing relay tests.
- A future WSS streamcut implementation must not "blank deltas and hope".
- If WSS streamcut fires, Slimference must either:
  - emit the exact valid Codex WSS terminal frame sequence needed by Codex CLI /
    Codex Desktop for a completed response, then close only the upstream half
    cleanly; or
  - use a different early-cut mechanism that preserves all terminal semantics
    and proves no client hang across live runs.
- The implementation must track the current response identifiers and sequence
  metadata required to synthesize terminal events without schema drift.
- Unknown or incomplete terminal state must fail open to byte-equal forwarding.
- WSS streamcut must remain separately observable from generic
  `frames_reencoded` so operators can distinguish terminal early-cut from
  request-side or repdet mutations.

## Engineering Plan

1. Capture the native Codex WSS response sequence around normal completion:
   - `response.created`
   - output item creation / deltas / item done events
   - `response.completed`
   - any Codex-specific metadata / rate-limit / control frames after terminal
2. Build a fixture from the capture with sensitive text redacted but all schema
   fields, IDs, sequence numbers, and event ordering preserved.
3. Define a `wssTerminalState` tracker in the Phase-F adapter:
   - response ID, output item ID, content index, sequence number;
   - seen terminal flag;
   - whether enough data exists to synthesize a completion safely.
4. Replace the old delta-blanking concept with one of two safe strategies:
   - **Synthetic terminal:** when trailing commentary begins and state is
     complete, stop forwarding later text deltas, emit legal terminal frames,
     and close upstream cleanly.
   - **Suppression-until-natural-terminal:** keep reading upstream until its
     natural terminal frame, suppress only the trailing commentary text, and
     forward all required terminal/control frames. This saves client-visible
     content but not upstream tokens, so it is only acceptable if synthetic
     terminal cannot be made reliable.
5. Add a kill switch independent from HTTP `streamcut` if WSS needs a longer
   rollout gate.
6. Add tests for every frame-order edge:
   - normal completion with no streamcut;
   - streamcut with complete terminal state;
   - streamcut before enough IDs are known, must fail open;
   - interleaved ping/pong;
   - compressed WSS frames with context takeover;
   - malformed terminal candidate, must not degrade the whole session;
   - two independent live-shaped responses in one session.

## Acceptance

- Unit tests prove the old unsafe blank-delta behavior is not used.
- Unit tests prove the new strategy emits a legal terminal sequence or fails
  open.
- Two live Codex CLI WSS runs with streamcut-triggering prompts:
  - exit 0;
  - return the expected sentinel or a valid shortened answer;
  - record no `parse_failures`;
  - record no `degraded_sessions`;
  - record no `compression_errors`;
  - do not leave child Codex processes running;
  - preserve `~/.codex/config.toml` bit-identical after disable.
- Codex Desktop is not enabled for WSS streamcut until CLI passes first.
- T226 WSS auto-promotion does not depend on WSS streamcut. WSS can become
  first transport with WSS streamcut still off.

## Sub-Tasks

- [ ] Capture and redact a normal native/scoped Codex WSS completion sequence.
- [ ] Add terminal-state fixture and unit tests.
- [ ] Implement `wssTerminalState` tracking.
- [ ] Implement synthetic-terminal or suppression-until-natural-terminal
  strategy.
- [ ] Add WSS-specific streamcut telemetry and kill switch.
- [ ] Run focused WSS tests and full gates.
- [ ] Run two live CLI WSS streamcut proofs.
- [ ] Only after live proof, decide whether WSS streamcut may join the default
  WSS savings set.

## Notes

- T235 may certify permessage-deflate, request-side mutations, stale/obsolete
  read pruning, BeTerse, and repdet without this task. T236 is only about the
  early-cut terminal behavior.
- A strategy that saves no upstream tokens is not enough to count as real
  streamcut product value. It may still be useful as a display cleanup, but that
  belongs behind a separate quality gate.
- If Codex changes WSS event names, this task must fail open and leave WSS
  streamcut disabled.

## Deviations

- None.
