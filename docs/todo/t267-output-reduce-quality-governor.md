# T267 - Output-reduce quality governor

## Why

Output tokens are expensive, but output reduction can directly hurt user
workflow if it cuts explanation, code, patch content, or final answers. The
right goal is not "shorter always"; it is task-aware reduction with automatic
rollback when the user or model needs more.

## Current reality check

- Stop sequences, streamcut, repetition detection, terse hints, and output
  profile work exist in some routes.
- WSS streamcut has known terminal-safety risk and must stay off until proven.
- Output-side savings are not the same as billable input savings and must be
  reported separately.

## Product target

Output reduction becomes a runtime-governed layer:

- never damages exact code/patch/final-answer workflows
- separates output-wire savings from billable input savings
- downgrades automatically after repair turns, "you skipped" feedback, malformed
  patches, or user re-asks
- only applies aggressive profiles to task shapes where proof says quality holds

## Technical work packages

1. Define task shapes:
   - short status
   - code review
   - patch generation
   - explanation
   - deep analysis
   - command output relay
   - final summary
2. Define per-task profile rules:
   - no aggressive output cut for patch/code/final exact content
   - conservative cut for status/boilerplate
   - repetition detector only where replacement is protocol-safe
   - WSS streamcut disabled until terminal-safe proof exists
3. Add quality signals:
   - repair-turn detection
   - user re-ask
   - malformed patch
   - missing requested detail
   - "too short" / "you skipped" patterns
4. Add automatic policy response:
   - downgrade profile for session/bucket
   - cooldown period
   - full output after negative signal
   - record reason in audit
5. Add accounting split:
   - output-wire bytes saved
   - output tokens estimated
   - billable input untouched
   - no mixed headline total

## Zero product-drawdown gates

- No output reducer may alter code blocks, patches, JSON payloads, or protocol
  terminal frames unless exact safety is proven.
- WSS streamcut remains disabled until a valid terminal sequence is captured and
  live-proven.
- Any repair signal disables aggressive output reduction for that workload
  bucket.
- User-visible truncation must not be hidden as "success".

## Savings targets

- Status/boilerplate-heavy outputs: measurable output-wire savings.
- No increase in repair turns or user re-asks in live corpus.
- No malformed patch regressions.

## Verification

- Unit tests for profile selection.
- Golden tests for code/patch preservation.
- Repair-turn feedback tests.
- HTTP streamcut tests stay green.
- WSS terminal-safe proof before WSS streamcut is enabled.
- Live corpus A/B for aggressive profiles.

## Notes

- Offline hardening completed: output-reduce now detects `final_summary` turns and
  statically caps `aggressive`, `codex_aggressive`, and `codex` profiles to
  `standard` for safety-sensitive shapes: code edits, new-file generation,
  debugging, reviews, tool-result reasoning, final summaries, read-only analysis,
  deep explanations, and planning. The cap runs in both the proxy hot path
  before tracker/cooldown selection and in `InjectBody` as defense in depth.
- Existing repair-signal plumbing remains active: "you skipped" / "too short" /
  malformed-patch style follow-ups immediately downgrade the stored
  provider/model/profile/task-shape bucket through the output-reduce tracker
  without waiting for the normal sample window. A repair signal can also create
  the cooldown bucket directly, so the next matching request is softened even if
  the prior outcome sample was not retained.
- Existing WSS guard remains active: WSS text deltas are not streamcut even when
  the global HTTP streamcut toggle is on; terminal WSS responses stay byte-equal
  to avoid corrupting code, patch, or final-answer payloads.
- 2026-06-02: Fixed the Codex Responses injection surface. Output-reduce no
  longer rewrites Responses `input` into a synthetic `system` message. It now
  appends to or creates the top-level `instructions` string, preserves `input`
  unchanged, and full-passes unsupported Codex shapes. This removes the same
  `400 System messages are not allowed` class found during the archive-recovery
  note proof and keeps output-reduce from mutating task/tool context.
- Offline verification covered profile selection, injection, task-shape
  detection, proxy hot-path profile capping, immediate repair-signal cooldown,
  and repair lifecycle. Live corpus proof for aggressive direct-answer/status
  workloads remains deferred until the capture phase.
- 2026-06-02: `codex-capture-run` now preserves output-reduce live counters in
  proof-matrix rows: injected/skipped turns, input overhead, observed output
  tokens, downgrade count, stop-sequence modifications, streamcut fires,
  repdet rewrites, stale-read blocks, obsolete-prune blocks, and be-terse
  injections. `wss-proof-matrix` can now require focused expected signals such
  as `output_reduce_injected`, `output_reduce_skipped`, `output_reduce_downgraded`,
  `stop_seq`, `streamcut`, `repdet`, `stale_read`, `obsolete_prune`, `beterse`,
  and `host_budget_ok`. This makes aggressive-profile promotion a hard
  live-evidence gate instead of a manual interpretation of admin counters.
- 2026-06-02: Fixed HTTP/SSE streamcut holdback accounting. When streamcut is
  armed but the stream ends naturally, delayed text-delta lines flushed from the
  holdback queue now still feed output-token and provider-usage accounting. This
  keeps output-reduce quality and savings telemetry honest without changing
  client-visible bytes or model-facing context.
- 2026-06-02: Hardened streaming output-token accounting. Text-delta estimates
  are used only until the provider reports a final output-token total; once an
  Anthropic or OpenAI/Codex usage total appears, that total wins for the request
  instead of being added on top of earlier estimates. This keeps output-reduce
  telemetry conservative and avoids fake output-token savings.
- 2026-06-03: Added an `explanation_deep_analysis` task shape. Explicit deep
  explanation requests now cap aggressive/Codex-aggressive profiles to
  `standard`, preserving reasoning steps, caveats, concrete evidence, and
  requested detail instead of treating the turn as a short direct answer.
- 2026-06-03: Hardened exact-output and repair detection. `reply/respond/answer/
  output/return only`, `json only`, `only json`, and German `gib/antworte/sage
  nur` prompts are now classified as `exact_reply`, so output-reduce injection
  full-passes instead of adding any directive to exact-format turns. German
  re-ask phrases such as `da fehlt ...` and `nochmal ausführlicher/genauer`
  now trigger the same immediate repair cooldown as English "you skipped" style
  feedback.
- 2026-06-03: Added a dedicated `command_output_relay` task shape for explicit
  output relay prompts such as "show the output", "full terminal output", and
  German `gib die komplette Terminal-Ausgabe`. These turns now full-pass
  output-reduce injection with reason `command_output_relay_exact_output`, even
  for large requests and aggressive profiles, so exact requested command output,
  paths, errors, exit codes, and line order cannot be shortened by a terse
  directive. Repair complaints such as "you skipped" / `fehlt` still classify as
  repair follow-ups and keep the cooldown behavior.
- 2026-06-03: Tightened task-shape input extraction to current instruction text.
  The classifier now reads user/system/developer messages, top-level
  `instructions`, top-level `system`, and top-level prompt/input text, but
  ignores prior Codex `function_call`, `function_call_output`, tool stdout,
  stderr, `output`, and `arguments`. This prevents historical terminal text such
  as `apply_patch failed` or `show full output` from falsely classifying the next
  turn as code-edit, repair, or command-output relay. The result is more output
  savings where safe, with no added product drawdown because exact/repair/relay
  guards still trigger from the actual user instruction.
- 2026-06-03: Offline proof scan over existing local WSS captures found no
  positive live `output_reduce_injected`, `output_reduce_skipped`, or
  `output_reduce_downgraded` rows. This is not a code failure; it means the
  aggressive-output proof cannot be closed from the existing capture corpus and
  must be run as a focused live workload before any stronger product claim.
- 2026-06-03: Added scoped env overrides for focused output-reduce proof runs:
  `SLIMFERENCE_OUTPUT_REDUCE_PROFILE` and
  `SLIMFERENCE_OUTPUT_REDUCE_MIN_INPUT_TOKENS`. These do not change defaults;
  they make managed `codex-capture-run` sessions reproducible without editing
  config files or leaving product state behind.

## Done

Output reduce is maxxed when it saves where safe, backs off automatically where
quality signals degrade, and never hides output savings as billable input
savings.
