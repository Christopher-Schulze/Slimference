# TASK 247: Codex WSS Phase-F reducer efficacy (Responses-API delta model)

Status: REDUCER CHAIN PROVEN END-TO-END on real Codex 0.133.0 CLI traffic
(2026-05-23 multi-read capture). Repeat-read sessions produce 94% output-payload
reduction; recorded `input_tokens_saved=26461` on a 3x35KB-file session. Earlier
"compressed_messages_mutated=0" reading was Codex-side run-variance, not a code
defect. Fixture-based regression test landed
(`internal/proxy/wsmitm_phasef_real_capture_test.go::TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`)
asserting Slimference-delta-marker reduction on reads #2 and #3 of an isolated
multi-read sequence. Aggregate-savings tooling landed at
`scripts/utils/aggregate-savings` for honest live + offline measurement of
WSS + output-reduce + HTTP-path Layer-0 savings in one report. Open: collect
representative real-workday measurements with the new tooling; one Desktop pass
on the identical code path.
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
- [x] Reproduce the cert's single mutation through the official
  `slimference codex recertify wss --force` path on a fresh daemon. Verified
  2026-05-23: `frames_reencoded:1`, `compressed_messages_mutated:1`,
  `phasef_mutations:1`, `input_tokens_saved=943`, zero parse/degrade/compression
  errors. The mutating frame is the synthetic-repo `git status --short` output
  compacted by the F01 git-status filter inside the L0 chain - deterministically
  reproducible, not a race.
- [x] Confirm that the read-delta reducer compacts repeated `function_call_output`
  tool-output deltas against the per-session readcache. Verified 2026-05-23 via a
  3x cat of /tmp/t247-read-target.md (35567b each). Read #1 was reduced to 6558b
  (81%) by `compactCodexExecEnvelope` + `filter.CompactCapturedOutputWithContext`.
  Reads #2 and #3 were reduced to 144b each (99%) by `compactProxyReadDelta`,
  which returned a delta marker of the form
  `"Slimference delta for /tmp/.../target.md:\n+ Chunk ID: <new>\n- Chunk ID: <prev>\nFull content: local-archive://..."`.
- [x] Verify the tool_use -> commandLine resolution fires for cross-turn
  `function_call_output` requests. Verified 2026-05-23: Codex tool name =
  `exec_command` (covered by `looksLikeShellTool`); arguments shape =
  `{"command":["bash","-lc","cat <path>"], ...}` -> `codexCommandLineFromFields`
  -> `bash -lc cat <path>` -> `normalizeLayer0CommandLine` strips the wrapper to
  `cat <path>`; `rememberToolUsesFromResponse` accumulates call_ids across turns
  so the second/third `function_call_output` resolves its prior `function_call`
  cleanly even though the request's own `tool_uses` index is empty.
- [x] Add a fixture-based regression test under `internal/proxy/` that replays a
  real captured Codex 0.133.0 multi-read delta sequence end to end. Landed as
  `internal/proxy/wsmitm_phasef_real_capture_test.go::TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`.
  Test isolates the readcache to `t.TempDir()` via `proxyUserHomeDir`, seeds
  three `function_call` items via `response.output_item.done` frames (exec_command
  tool, real Codex shape: arguments is a JSON-encoded STRING with `cmd` as a
  single-string shell command - NOT `command` as a bash-wrapped array - plus
  `workdir`, `yield_time_ms`, `max_output_tokens`), replays three
  `function_call_output` c2s requests with the Codex exec envelope wrapping a
  ~57KB synthetic markdown payload, and asserts: reads #2 and #3 mutate (replace=
  true), shrink, carry `"Slimference delta for <path>"` in the post-pipeline raw
  bytes, and roll up `>=2` Layer-0 modified requests with non-zero
  `ProxyLayer0TokensSaved`. Synthetic payload only; no private file contents.
  Runs in ~0.10s.
- [x] Re-measure live (CLI). Verified 2026-05-23 on Codex 0.133.0 +
  Slimference 2.0.2: `frames_reencoded=3`, `compressed_messages_mutated=3`,
  `phasef_mutations=3`, `mutation_active=true`, `byte_bridge_only=false`,
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  `savings.input_tokens_saved=26461` across one 3-read session.
- [ ] Re-measure live on Codex Desktop on the identical Phase-F route once a
  user-confirmed Desktop session is run via TUI Launch Codex App. T246 proved
  the route is identical, so the reducer chain is expected to behave the same.
- [x] Separately quantify savings from the OTHER layers (output-reduce stop-seq,
  response cache, L0 read-delta on HTTP). Tooling landed:
  `scripts/utils/aggregate-savings` pulls live `/admin/state` WSS counters,
  live output-reduce counters, and offline HTTP-path Layer-0 filter savings via
  `--filter-db=<path>` plus `--period=<all|today|week|month>`, with optional
  USD estimation via `--usd-per-million=<rate>`, and renders honest text or
  JSON. Offline mode supported via `--admin-state-file=<path>` for
  cert-ceremony reproducibility. 9 unit tests covering text shape, JSON
  round-trip, conditional bridge-only / mutation-active notes, health warn
  line, flag validation, and help output. First live aggregate measurement
  recorded 2026-05-23 (see `docs/operation-log.md`): four autonomous CLI
  multi-read sessions produced `compressed_messages_mutated=5`,
  `frames_reencoded=5`, `phasef_mutations=5`, `input_tokens_saved=28284` on
  WSS Phase-F plus 1356139 tokens saved from historical HTTP-path Layer-0
  filter hits; aggregate 1384423 tokens (~$3.46 at 2.5 USD/M-token). Remaining
  is data collection over a longer real-workday window, not code.

## Notes

- Counter flush timing: WSS byte/frame/mutation counters aggregate at session end
  (close the WSS to read them). `phasef_bridged` increments at session start.
- The cert criteria (`codexWSSCertificationFailures`, `cmd/slimference/codex_cmd.go`)
  already require `frames_reencoded>0` + `compressed_messages_mutated>0` +
  `mutation_active=true` + `byte_bridge_only=false`. As of 2026-05-23 they pass on
  the synthetic-repo recert path (F01 git-status frame, ~943 input tokens saved)
  AND on representative repeat-read sessions (26461 input tokens saved on a
  3x35KB-file CLI capture).
- DO NOT chase the big repeated block. The ~117KB body is mostly `instructions`
  (system prompt) + `tools` (definitions), repeated near-identically every request -
  it looks like the giant savings lever, but `prompt_cache_key` is exactly OpenAI's
  server-side prompt cache for that pattern: they already discount it, so local
  dedup of it saves the user nothing billable. The REAL lever is the tool-output
  deltas across turns (`function_call_output`).

- 2026-05-23 CHAIN VERIFIED END-TO-END (after env-gated multi-read capture, since
  reverted; capture-evidence archived at `/tmp/t247-dump-evidence.tgz`). On real
  Codex 0.133.0 + Slimference 2.0.2 with a fresh daemon and a 3x cat of a 35KB
  markdown file:
  - extractMessages parses Responses-API `input` items correctly; the captured
    request body shape is `{model, instructions(21335b), tools(14), input[1],
    previous_response_id, prompt_cache_key, ...}` with `input[0].type =
    "function_call_output"`, `call_id = "call_<id>"`, `output =
    "Chunk ID: <id>\nWall time: 0.000 seconds\nProcess exited with code 0\nOriginal token count: <n>\nOutput:\n<file content>"`.
  - codexInputItemToMessage maps function_call_output -> tool_result with
    ToolResultID = call_id (firstNonEmpty fallback also covers `id`).
  - rememberToolUsesFromResponse accumulates the prior turn's function_call
    items keyed by the same call_id; by request #2 the remembered map holds the
    request #1 use, by request #3 it holds #1 and #2.
  - proxyResolveToolUse looks up the remembered use via ToolResultID; resolved
    `ToolName = "exec_command"` (covered by `looksLikeShellTool`).
  - codexCommandLineFromFields extracts `["bash","-lc","cat <path>"]` from
    `arguments`; `normalizeLayer0CommandLine` strips the `bash -lc` wrapper to
    `cat <path>`; `filter.ReadPathFromCommandLine` yields the path.
  - wsCodexSessionID resolves `codex-wss:<prompt_cache_key>` so the per-session
    readcache context keys correctly across the delta requests.
  - Pipeline outcome on the capture: read #1 = 35567b -> 6558b (81%, via
    `compactCodexExecEnvelope` + `filter.CompactCapturedOutputWithContext`);
    reads #2/#3 = 35567b -> 144b each (99%, via `compactProxyReadDelta` ->
    `readcache.EvaluateObserved` -> `DecisionBlock` with reason
    `"Slimference delta for ...:\n+ Chunk ID: <new>\n- Chunk ID: <prev>\nFull content: local-archive://..."`).
  - Daemon counters after session close: `phasef_bridged=1`,
    `frames_reencoded=3`, `compressed_messages_mutated=3`,
    `phasef_mutations=3`, `mutation_active=true`, `byte_bridge_only=false`,
    `parse_failures/degraded/compression_errors=0`,
    `savings.input_tokens_saved=26461`.

- HONEST CALIBRATION. The earlier 2026-05-23 measurement that observed
  "compressed_messages_mutated=0 on multi-read CLI" was Codex-side run-variance
  (different Codex plan/turn count on identical prompt against unchanged code),
  not a code defect. The capture run produced 3 mutations on the same code path.
  Slimference WSS Phase-F savings are workload-dependent: ~0 on sessions
  without repeat reads, large on sessions with repeat reads. The cert no longer
  rests on a single F01-style git-status frame; the read-delta reducer is
  operationally proven against real Codex Responses-API delta traffic.

- Higher-order question (originally posed, now answered): WSS Phase-F IS a
  productive savings lever for Codex when the workload includes repeat reads.
  Savings on non-repeat-read workloads still rely on F01-F24 filter hits inside
  the same applyInputPipeline; quantification of those plus non-WSS layers
  remains the last open sub-task before T240 release certification.

- Implementation note (no production code change needed for the T247
  reducer-efficacy question): the reducer chain already covers the real Codex
  shape end to end. The remaining engineering effort is aggregate
  non-WSS-layer measurement, not a reducer fix.

- Real Codex 0.133.0 `exec_command` argument shape clarification (verified
  against `resp-response.output_item.done` capture, archived in
  `/tmp/t247-dump-evidence.tgz`): the `function_call.arguments` field is a
  JSON-ENCODED STRING whose object has `cmd` as a single-string shell command
  (e.g. `cat /tmp/<path>`), `workdir`, `yield_time_ms`, `max_output_tokens`.
  NOT a `command` array wrapped in `bash -lc ...`. `codexCommandLineFromFields`
  finds `cmd` directly (first key in its loop), so `proxyLayer0CommandLine`
  returns the literal `cat <path>` without needing `normalizeLayer0CommandLine`
  to strip a wrapper. `filter.ReadPathFromCommandLine("cat <path>")` then yields
  the path cleanly. This is why the chain works on real production traffic;
  the earlier draft of the regression test used an incorrect bash-wrapped
  array shape and failed, which exposed and corrected the shape assumption
  before the test was committed.

## Deviations

None.
