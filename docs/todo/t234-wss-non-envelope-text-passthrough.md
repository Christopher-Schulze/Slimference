# TASK 234: WSS non-envelope text passthrough

Status: DONE
Priority: P0 before WSS certification / T226 promotion
Scope: `internal/proxy/wsmitm`, scoped Codex WSS live retry, T224 operation log

## Why

T209 Phase 3 proved the scoped WSS bridge works functionally:
`slimference codex run --transport=wss` returned the expected Codex answer.
But `/admin/state.wss` showed `parse_failures=1`, `degraded_sessions=1`, and
`frames_reencoded=0`. The bridge failed open correctly, but Phase-F mutation was
disabled for the whole session.

Code inspection found the first concrete failure class without needing a packet
capture: `Session.pump` attempted to parse every non-empty text frame as a
`wsmitm` JSON object envelope. `Parse` intentionally rejects non-object payloads
such as scalars, arrays, sentinels, or extension text. Those payloads can be
valid WebSocket traffic, but they are not Phase-F mutation candidates.

Treating a legal non-envelope text frame as parser drift is too strict. It
poisons the whole session and removes WSS savings even though byte-equal
forwarding is sufficient for that frame class.

The live retry after this fix identified the next, separate blocker: Codex
0.130 uses compressed WebSocket payloads (`permessage-deflate` / RSV1). Those
frames are now forwarded byte-equal without degrading, but Phase-F mutation
cannot fire on them until Slimference implements extension-aware
decompress/recompress. That work is split into T235.

## Target State

- Non-text frames still pass through byte-equal.
- RSV/compressed-extension frames still pass through byte-equal.
- Empty text frames still pass through byte-equal.
- Text frames whose payload does not look like a JSON object pass through
  byte-equal.
- RSV/compressed-extension frames do not increment `parse_failures`.
- RSV/compressed-extension frames do not set `degraded`.
- Non-envelope text frames do not increment `parse_failures`.
- Non-envelope text frames do not set `degraded`.
- Later valid JSON envelope frames in the same session are still parsed and can
  be mutated.
- Malformed object-shaped JSON still increments `parse_failures`, sets
  `degraded`, and byte-bridges the rest of the session.
- The fix does not relax malformed-envelope fail-open safety.

## Acceptance

- Unit tests prove non-envelope text payloads are forwarded unchanged and do not
  degrade the session.
- Unit tests prove RSV text payloads are forwarded unchanged and do not degrade
  the session.
- Unit tests prove a later valid envelope after a non-envelope text frame still
  reaches the Phase-F handler.
- Unit tests prove malformed object-shaped JSON still degrades the session and
  prevents later handler calls.
- Existing WSS Phase-F tests still pass.
- `go test ./... -count=1 -timeout 300s` passes.
- `go vet ./...` passes.
- `go run ./scripts/ci` passes with aggregate coverage >= 99.5%.
- Rebuilt installed binary and daemon use the patched code.
- Live scoped WSS retry returns the expected answer with
  `stop_seq_injections=0`.
- Live scoped WSS retry records `parse_failures=0` and `degraded_sessions=0`.
- `frames_reencoded>0` remains the T224/T226 promotion signal. A tiny prompt may
  legitimately produce no mutation candidate, so a no-degrade WSS smoke alone is
  not enough to write `codex-wss-cert.json`. The live T234 retry still recorded
  `frames_reencoded=0` because Codex 0.130 payloads are compressed; T235 owns
  that blocker.

## Sub-Tasks

- [x] Inspect `wsmitm.Session`, `wsmitm.Parse`, and WSS frame accounting.
- [x] Classify non-object text payloads as non-mutatable rather than malformed
  envelopes.
- [x] Classify RSV/compressed-extension frames as non-mutatable rather than
  malformed envelopes.
- [x] Keep malformed object-shaped JSON on the existing fail-open degrade path.
- [x] Add regression coverage for both paths.
- [x] Run focused WSS/proxy tests.
- [x] Run full verification.
- [x] Rebuild/install and restart the daemon.
- [x] Rerun scoped Codex WSS smoke and record counters.
- [x] Append operation-log result and commit.

## Notes

- This task does not replace T224. It removes the false-positive degradation so
  T224/T235 can focus on the real blocker: compressed WSS payload mutation.
- This task does not touch `/etc/hosts`, pfctl, Keychain, system proxies,
  `slimference lab`, or Claude Code.
- Do not promote `transport=auto` to WSS and do not write
  `~/.slimference/codex-wss-cert.json` from this task unless a later explicit
  T224 certification run meets all promotion criteria.
- Live result: scoped WSS `WSS_OK`, shell-tool `TOOL_OK`, and shell-tool
  `DUMP_OK` runs completed with `parse_failures=0`,
  `degraded_sessions=0`, and `stop_seq_injections=0`; all frames were still
  forwarded (`frames_reencoded=0`) because the payloads were compressed.
- A temporary env-gated frame dump was used to identify the compressed payload
  class and was removed from the product code before commit.

## Deviations

- None.
