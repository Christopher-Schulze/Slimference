# TASK 247: Codex WSS Phase-F reducer efficacy (Responses-API delta model)

Status: OPEN - root cause found, session-key prerequisite fixed (`b5213e8`)
Priority: P0 - this is whether Slimference delivers real Codex token savings at all
Scope: WSS Phase-F request reducers for Codex (CLI + Desktop, same route)

## Why

Measured 2026-05-23: Codex WSS Phase-F INSPECTS real traffic (inflates
permessage-deflate, examines messages, zero errors) but MUTATES almost nothing -
`frames_reencoded=0`, `compressed_messages_mutated=0` on real CLI and Desktop
sessions. The persisted cert rests on a single mutated frame (`frames_reencoded:1`).
So Slimference currently delivers marginal-to-zero WSS token savings for BOTH
transports. This is not Desktop-specific; T246 proved Desktop rides the identical
route.

Root cause (evidence in `docs/operation-log.md`, 2026-05-23 entries; request bodies
were captured to `/tmp/wsphasef-body-*.json` via a temporary env-gated dump, since
reverted): Codex WSS is the OpenAI Responses API with `previous_response_id`
(server-side conversation state). Each request carries only the DELTA (new input
items), e.g. `input=[function_call_output]`, not the accumulated history. Slimference's
L1/L0 dedup reducers are built for the Chat-Completions shape where the full history
(with repeated tool outputs) is in every request, so they find no repetition to
reduce in a single delta request.

## Acceptance

- A real Codex WSS session (CLI is fine; Desktop transfers since the route is
  identical) that re-reads files / produces repeated tool outputs records, after
  session close (counters flush at session end): `frames_reencoded>0`,
  `compressed_messages_mutated>0`, `phasef_mutations>0`, with `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`.
- The reduction is lossless/safe (read-delta / dedup semantics already used on the
  HTTP path), reconstructable, and never corrupts the conversation.
- Measurement uses the lag-free + flush-aware methodology (fresh daemon, close the
  session before reading; `phasef_bridged` is the only reliable mid-session signal).
- No regression to CLI HTTP-path savings or to the byte-equal WSS bridge fallback.

## Sub-Tasks

- [x] Diagnose why mutation is ~0 (Responses-API delta model; empty session key).
- [x] Fix `wsCodexSessionID` to extract `prompt_cache_key` /
  `client_metadata.x-codex-turn-metadata` so the per-session read context keys
  correctly (`b5213e8`). Necessary but not sufficient.
- [ ] FIRST (cheapest, highest-certainty): reproduce the cert's single mutation.
  The persisted cert was issued 2026-05-22 with `frames_reencoded: 1` on this same
  delta model. Re-run the exact recert/cert path (`slimference codex certify wss` /
  the recert trigger in `codex_recert.go`) on a fresh daemon, close cleanly, and
  check the flushed counters. If `frames_reencoded:1` reproduces, trace WHICH frame
  mutated and why - that is the one thing that currently works and anchors the fix.
  If it does NOT reproduce, the cert was issued under non-reproducible conditions
  and that itself is the finding.
- [ ] Make the read-delta reducer compact a `function_call_output` tool-output
  delta against the prior tool output of the same command/file remembered in the
  per-session context (resolve the tool_use via `rememberToolUsesFromResponse`,
  which holds the function_call from the prior response referenced by
  `previous_response_id`).
- [ ] Verify the tool_use -> commandLine resolution actually fires for Codex
  `function_call` items (the `function_call_output` request has `tool_uses=0`; the
  use must come from remembered responses).
- [ ] Add a fixture-based test using a real captured Codex WSS delta sequence so
  the reducer is exercised against the true request shape, not the
  Chat-Completions shape.
- [ ] Re-measure live (CLI then Desktop) and record the flushed mutation counters.
- [ ] Separately quantify savings from the OTHER layers (output-reduce stop-seq,
  response cache, L0 read-delta on HTTP) so the product savings claim is grounded
  in measurement, not in `wss_certified=true`.

## Notes

- Counter flush timing: WSS byte/frame/mutation counters aggregate at session end
  (close the WSS to read them). `phasef_bridged` increments at session start.
- The cert criteria (`codexWSSCertificationFailures`, `cmd/slimference/codex_cmd.go`)
  already require `frames_reencoded>0` + `compressed_messages_mutated>0` +
  `mutation_active=true` + `byte_bridge_only=false`; today they pass only on a
  single mutated frame. T247 should make them pass on representative real sessions.
- DO NOT chase the big repeated block. The ~117KB body is mostly `instructions`
  (system prompt) + `tools` (definitions), repeated near-identically every request -
  it looks like the giant savings lever, but `prompt_cache_key` is exactly OpenAI's
  server-side prompt cache for that pattern: they already discount it, so local
  dedup of it saves the user nothing billable. The REAL lever is the tool-output
  deltas across turns (`function_call_output`), which prompt cache does not help -
  that is where T247 must focus.
- Open higher-order question: if even content-rich sessions never mutate after the
  reducer work, then WSS Phase-F is the wrong lever for Codex and savings must come
  from other layers - decide and document that honestly for T240 release certification.

## Deviations

None.
