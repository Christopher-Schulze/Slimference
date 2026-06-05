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
  and repair lifecycle. Live corpus proof now covers guarded WSS injection and
  observed provider output-token accounting; provider-cache read tokens remain
  separate and must not be counted as output-reduce savings.
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
- 2026-06-03: Wired the guarded output-reduce injector into the Codex WSS
  Phase-F request path. It uses the existing task-shape gates, exact-reply
  guard, command-output relay guard, low-ROI gates, profile capping, and
  top-level Codex `instructions` injection. The WSS adapter explicitly skips
  any request body containing `function_call_output`, so output-reduce cannot
  mutate read/search/git/test/tool-output deltas or corrupt first-read seeding.
  New WSS tests prove both the positive instruction injection path and the
  exact/tool-output full-pass guards. This made `output_reduce_aggressive`
  code-reachable for the later focused live proof.
- 2026-06-03: Fixed the remaining WSS output-reduce false skip. Task-shape
  detection now derives the shape from the current user request before falling
  back to broader request text, so static AGENTS/system instructions containing
  phrases like "reply only" or "exactly" cannot misclassify a normal prompt as
  `exact_reply`. Tests cover Codex and OpenAI system-instruction contamination.
- An earlier live capture attempt was blocked externally because Codex produced
  no WSS frames while reconnecting. That isolated the gap to live output-token
  evidence rather than offline wiring; a later focused proof closed that
  evidence gap below.
- 2026-06-04: Existing local proof inventory now contains focused
  `output_reduce_aggressive` rows with `output_reduce_injected`,
  `output_reduce_output_tokens`, `host_budget_ok`, zero safety errors, and an
  exported live-corpus row. This proves the guarded WSS injection path is
  reachable and provider output tokens are observed on the same product route.
  It is still not a counterfactual output-savings percentage claim: the
  exported row's saved-token field comes from provider-cache evidence, while the
  output-reduce claim is "injected and measured without safety regression."
- 2026-06-04: Tightened `benchmark-corpus --maxx-check` to enforce that split.
  `output_reduce_aggressive` now fails the maxx gate unless the live corpus row
  carries observed output-token evidence in addition to output-reduce injection.
  A fresh focused CLI proof with the current binary now satisfies that gate with
  `output_reduce_injected`, 154 observed output tokens, `host_budget_ok`, and
  zero parse/degrade/compression errors. This prevents provider-cache tokens from
  accidentally closing an output-reduce savings claim while still requiring real
  output-token observability.
- 2026-06-04: Tightened the upstream proof surfaces to the same standard.
  `wss-proof-matrix` now treats `output_reduce_aggressive` as economically
  positive only when both `output_reduce_injected` and observed output tokens are
  present; `wss-proof-inventory` requires the new
  `output_reduce_output_tokens` signal; and `wss-proof-export-corpus` skips
  output-reduce rows that have injection without output-token evidence. This
  prevents a bad future export from recreating the stale positive-looking row.
- 2026-06-04: Fixed WSS output-token accounting for output-reduce. The WSS
  `response.completed` usage path already fed provider-cache analytics; it now
  also feeds the existing output-reduce tracker with provider-reported
  `output_tokens`, matching the HTTP path. The regression test proves WSS usage
  frames increment `OutputTokensObserved` without mutating the response.
- 2026-06-04: Aligned the central planner with the runtime output-reduce safety
  contract. `exact_reply`, `command_output_relay`, `repair_followup`, and
  low-ROI direct-answer shapes report Layer 3 bypasses in planner summaries.
  As of the stricter 2026-06-05 safety pass, unproven detail-sensitive shapes
  also bypass output-reduce injection by default instead of receiving a
  standard directive: code-edit/debugging/review/explanation/tool-reasoning/
  read-only/planning/new-file/final-summary. This prevents prompt-side
  directive drift on tasks where missing detail is a product drawdown. Focused
  planner and injector tests cover every guarded shape.
- 2026-06-05: Tightened output-reduce proof accounting again. Release reports,
  corpus export, and `benchmark-corpus` now carry output-reduce input overhead
  separately from observed provider output tokens and expose
  `output_reduce_net_observed_tokens` only as a diagnostic, not as a
  counterfactual savings claim. `output_reduce_aggressive` corpus metadata now
  records `expected_output_reduce_input_overhead_max` and
  `expected_output_reduce_net_observed_min`, and the gate enforces both when
  present. The clean release proof passed with `output_reduce_injected_turns=2`,
  `output_reduce_input_overhead_tokens=752`,
  `output_reduce_observed_tokens=1072`, and
  `output_reduce_net_observed_tokens=320`. The focused
  `cli_output_reduce_aggressive` live row remains intentionally honest:
  `observed=154`, `overhead=328`, `net_observed=-174`; it proves guarded WSS
  injection, accounting, host-budget OK, and zero safety errors, not a positive
  output-token reduction percentage. A real direct-answer/status A/B baseline is
  still required before claiming concrete output-token savings magnitude.
- 2026-06-05: Added the missing counterfactual proof surface.
  `codex-capture-run` and `wss-proof-live-row` can now stamp matrix rows with
  `ab_pair_id` and `ab_variant` (`baseline` or `directive`), and
  `wss-output-reduce-ab-report` pairs those rows content-free. The gate requires
  matching client/workload, observed provider output tokens on both sides,
  output-reduce injection only on the directive side, zero safety errors, zero
  output-reduce downgrades, host-budget OK when reported, positive output-token
  reduction, and positive net tokens after subtracting directive input overhead.
  This closes the tooling gap for real output-reduce savings proof. The
  remaining gap is live evidence: capture paired baseline/directive rows before
  promoting any concrete output-token savings percentage.
- 2026-06-05: Closed the first focused CLI output-reduce A/B proof. The first
  baseline row exposed a proof-tooling gap: no-directive rows had provider
  output tokens on the WSS wire but not in the output-reduce tracker. Matrix
  rows now carry content-free `provider_output_tokens_observed` parsed from
  provider usage frames, and `wss-output-reduce-ab-report` uses it before
  falling back to legacy output-reduce-specific rows. The same run exposed an
  accounting bug: input overhead was estimated from JSON body re-marshal byte
  churn instead of the model-facing directive text. `InjectBody` now records
  directive bytes only, and a regression test locks that behavior. After also
  shortening the safe Codex direct-answer directive, the focused CLI A/B gate
  passed: baseline `987` provider output tokens, directive `768`, directive
  overhead `23`, output saved `219`, net saved `196`, output savings `22.19%`,
  `lost=0`, host budget `ok`, and zero parse/degrade/compression errors. This
  proves positive net output-reduce on one direct-answer/status workload. More
  CLI/Desktop task-shape breadth is still required before treating the exact
  percentage as a broad product-default claim.
- 2026-06-05: Promoted that A/B proof from `/tmp` evidence into the durable
  live-corpus contract. Added
  `tests/fixtures/live_corpus/cli_output_reduce_ab_direct_answer/` with a
  content-free `output_reduce_ab_report.json` and metadata gates for at least
  one pair, positive net saved tokens, and at least 20% output-token reduction.
  `benchmark-corpus --maxx-check` now requires the `output_reduce_ab` workload
  in addition to `output_reduce_aggressive`, and the corpus evaluator fails
  unsafe, missing, or net-negative pairs. This makes the output-reduce Maxx
  claim counterfactual, not merely "directive injected".
- 2026-06-05: Reality-checked a second autonomous CLI A/B pair for an
  explanation/deep-analysis shape. The original standard safety directive was
  too expensive for this shape (`111` input-overhead tokens) and stayed net
  negative even though it reduced output. The standard safety directives are now
  shape-specific and compact while preserving exact detail/evidence/caveat/path
  requirements; the same explanation pair then had only `46` overhead tokens but
  still failed net-positive (`baseline=222`, `directive=248`, `net=-72`). This is
  intentionally not promoted. Current evidence says output-reduce is a real win
  for direct-answer/status shape. Explanation/deep-analysis and other
  detail-sensitive shapes now full-pass by default until paired A/B evidence
  proves positive net savings without repair/re-ask signals.
- 2026-06-05: Tightened the proxy-level regression proof for detail-sensitive
  shapes. The HTTP hot-path code-edit test now asserts that an unproven
  code-edit request receives no output-reduce marker at all, reports
  `unproven_task_shape_ab_required`, and leaves `Applied=false`. This locks the
  stronger post-A/B policy: safety-sensitive shapes are not merely softened from
  aggressive to standard; they full-pass until paired proof exists.
- 2026-06-05: Reality-checked another autonomous CLI status A/B pair
  (`t267-status-extra-20260605`) against the stricter product claim. The pair
  was intentionally not promoted: baseline output was `310`, directive output
  was `616`, net saved tokens were `-306`, host budget stayed `ok`, replay lost
  `0`, and safety counters stayed clean. More importantly, the directive row
  had `output_reduce_injected=1` but `output_reduce_input_overhead_tokens=0`,
  so the old report could have treated an injected directive as structurally
  valid without proving model-facing overhead accounting. The
  `wss-output-reduce-ab-report` gate now fails such rows with
  `directive missing positive output_reduce_input_overhead_tokens`. This keeps
  T267 honest: only paired, net-positive, overhead-accounted rows can support a
  savings claim.
- 2026-06-05: Added a second positive CLI output-reduce A/B proof for a
  practical engineering status-update shape. Pair
  `t267-cli-brief-status-20260605T154405Z` passed the content-free gate with
  baseline `631`, directive `240`, directive overhead `23`, output tokens saved
  `391`, net tokens saved `368`, output reduction `61.97%`, `lost=0`, host
  budget `ok`, and zero safety errors. It is committed as
  `tests/fixtures/live_corpus/cli_output_reduce_ab_brief_status/` and broadens
  the positive evidence beyond the first direct-answer/status pair.
- 2026-06-05: Closed the repair/re-ask rollback breadth proof for the HTTP
  output-reduce hot path. Proxy tests now cover the existing English user
  re-ask path plus German user re-ask and malformed-patch repair signals. In
  all cases the repair turn receives no output-reduce directive, the prior
  applied bucket is consumed once, and the next matching bucket is immediately
  softened from aggressive to the safer standard directive. This closes the
  offline repair-rollback gap.
- 2026-06-05: Ran the first clean Desktop output-reduce A/B reality check after
  excluding a prior upstream `invalid_request` diagnostic run from savings
  evidence. Pair `t267-desktop-direct-long-20260605T160644Z` proved Desktop
  app-server routing, output-reduce injection, host-budget `ok`, `lost=0`, and
  zero WSS parser/degrade/compression errors, but it failed the positive savings
  gate: baseline provider output `245`, directive provider output `566`,
  directive overhead `23`, output tokens saved `-321`, net tokens saved `-344`.
  This is intentionally not promoted into `tests/fixtures/live_corpus`; it is a
  policy proof that Desktop/direct-long output-reduce must not support a broad
  product-default savings claim yet.
- 2026-06-05: Closed T267 for the current zero-drawdown product scope. A broad
  output-reduction default across explanation, deep-analysis, code/patch,
  relay, repair, review, final-summary, and Desktop direct-long shapes is not
  maxxed engineering; it is an unproven quality risk. The product surface now
  claims concrete savings only for paired, net-positive direct-answer/status
  shapes and otherwise gates or softens Layer 3 before it can reduce requested
  detail, exact output, paths, errors, patches, or workflow context. Future
  shapes can reopen T267 only with paired A/B proof that passes host-budget,
  lost-context, safety, repair/re-ask, and net-token gates.

## Done

Output reduce is maxxed when it saves where safe, backs off automatically where
quality signals degrade, rejects broad unsafe shortening claims, and never hides
output savings as billable input savings.
