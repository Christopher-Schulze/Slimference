# TASK 248: Unified Codex savings engine for WSS and HTTP

Status: ACTIVE - shared attribution, opportunity telemetry, route attribution, report/status hygiene, tool-shape fixtures, proof gates, repeated-output planner classification, and WSS body planner telemetry implemented
Priority: P0 after T247 proof, before T240 release seal
Scope: Codex CLI and Codex Desktop savings engine, telemetry, cache strategy, and
proof-gated safe expansion across WSS and HTTP fallback paths

## Why

T247 proved the important part: Codex CLI and Codex Desktop can both ride the
same scoped no-CA WSS Phase-F route, and repeated tool-output reads produce real
mutation and input-token savings. The next bottleneck is no longer "does WSS
work?" It is "how do we make every safe savings mechanism available to both WSS
and HTTP paths, measure which mechanism fired, keep WSS as the default, and
repair Codex-update drift automatically enough that the user does not notice?"

The product target is strict:

- WSS remains the standard path for Codex CLI and Codex Desktop.
- HTTP remains a compatibility fallback and a useful hook/filter surface, not
  the preferred Codex conversation path.
- Desktop launched normally from Finder/Spotlight remains direct.
- Slimference mode is scoped to `slimference codex run` or the TUI/launcher.
- No model degradation, no intelligence loss, no context loss, no memory loss,
  no voice/realtime mutation, no global proxy/hosts/CA product dependency.
- Savings claims require measured mutation/counter evidence.
- Cache work must improve cost/hit-rate without breaking OpenAI's
  `prompt_cache_key`, server-side conversation state, or Codex Responses API
  semantics.

## Acceptance

- One shared Codex reducer core serves WSS and HTTP request shapes where the
  semantics are identical: tool-output recognition, remembered tool-use
  resolution, read-delta, Codex exec-envelope handling, F01-F24 captured-output
  filters, and token-decreasing safety guards.
- WSS and HTTP record the same reducer-mechanism attribution: read-delta blocks,
  captured-output blocks, Codex exec-envelope blocks, tokens saved, and blocks
  modified.
- `aggregate-savings` reports live WSS savings with mechanism breakdown plus
  HTTP-path Layer-0/filter savings without double counting, and includes the
  current Codex route / auto-recert snapshot.
- Codex-update drift stays automatic as far as safely possible:
  `wss_phasef -> wss_bridge -> http -> direct`, background recert, per-tuple
  lock/backoff, bounded logs, exact status reason, and no blind mutation after
  an unproven update.
- Cache strategy is proof-gated:
  - do not mutate `instructions` / `tools` prompt-cache blocks;
  - preserve `prompt_cache_key` and server-side conversation state;
  - only cache or replace content when correctness is reconstructable and
    schema-safe;
  - cache hit-rate improvements are measured against real Codex workloads.
- L2/L3 expansion into WSS is allowed only after a fixture and live proof show
  no quality/context regression. Until then, L2/L3 remain HTTP/legacy-safe
  surfaces and measurement candidates, not WSS defaults.
- A real workday measurement records CLI and Desktop sessions, WSS mutation
  counters, mechanism attribution, fallback/recert events, and aggregate savings.

## Sub-Tasks

- [x] Add shared reducer-mechanism attribution for the existing Codex
  proxy-Layer-0 core instead of one undifferentiated `proxy_layer0_tokens_saved`
  counter. First slice records total modified blocks, read-delta blocks,
  captured-output filter blocks, and Codex exec-envelope blocks for both HTTP
  and WSS callers.
- [x] Surface the new attribution through `OutputReduceTelemetry`,
  `/admin/state` savings, and `scripts/utils aggregate-savings` text/JSON.
- [x] Add opportunity/miss telemetry before broadening mutation: tool-result
  blocks seen, command-resolved blocks, and read-delta attempts are counted for
  both HTTP and WSS, while modified-block/mechanism success counters remain
  gated on positive token savings.
- [x] Split the current package-local helper into an explicit shared Codex
  reducer entry point. `reduceCodexLayer0` accepts parsed messages, session id,
  remembered tool uses, and route label (`http`, `wss_phasef`) and returns
  rewritten messages plus typed stats. Existing wrappers remain for old tests
  and callers.
- [x] Add second-level miss telemetry for hit-rate optimization:
  unresolved tool-use references, unresolved commands, and read-delta misses.
  This makes the next cache/tool-shape bottleneck visible without broadening
  mutation.
- [x] Normalize shell command arrays before Layer-0 classification, so
  `["bash","-lc","cat <path>"]` / `["sh","-c","git status --short"]` unlock the
  same read-delta and captured-output filters as string commands.
- [x] Add route-specific attribution for the shared reducer under
  `proxy_layer0_routes.http` and `proxy_layer0_routes.wss_phasef`, so future
  hit-rate work can prove whether misses and savings came from HTTP fallback or
  the primary WSS Phase-F route.
- [x] Preserve Codex tool `workdir`/`cwd` metadata and resolve relative
  single-file read commands against it before readcache evaluation. This raises
  repeat-read hit probability and prevents same-relative-path cache collisions
  across repositories without touching non-read commands.
- [x] Add safe single-text-part output-array extraction for Codex tool outputs.
  `output` / `content` arrays with exactly one `output_text` / `text` /
  `input_text` item now feed the same shared reducer and reconstruct the changed
  text in place, preserving sibling non-text parts. Multi-text or ambiguous
  arrays fail open.
- [x] Add the same unique-text-part handling for nested MCP-style output objects,
  such as `result.content[0].text`, preserving result metadata like `isError`
  and failing open on multi-text nested arrays.
- [x] Smooth `aggregate-savings` workday-measurement flags. The utility now
  accepts both `--flag=value` and `--flag value` for file/URL/period/cost inputs
  and reports missing flag values explicitly.
- [x] Expand Codex tool-shape coverage for the current known safe Responses-API
  read-output shapes. End-to-end WSS Phase-F fixtures now prove repeated-read
  delta mutation for `exec_command`, `local_shell_call`, `shell_call`, direct
  `read_file` tools, single-text output arrays, and MCP-style `result.content`
  objects, while preserving metadata and failing open on ambiguous shapes.
- [ ] Keep future Codex tool-shape expansion capture-driven only. Every new
  unknown variant still needs a real captured frame or equivalent high-fidelity
  fixture, a reconstruction test, and fail-open behavior before mutation.
- [x] Improve session/turn-aware cache policy for repeated tool outputs:
  maximize readcache hit-rate across turns without touching prompt-cache blocks,
  recently-edited files, voice/realtime, or non-reconstructable content.
- [x] Design and prove a "cache frontier" for WSS:
  local archive references, read-delta markers, and exact repeated output
  suppression are allowed; semantic summaries and response cache substitutions
  require separate proof because Codex WSS uses `previous_response_id` server
  state.
- [x] Add recert UX hardening if gaps remain after live observation:
  auto-recert status in TUI, last attempt age, bounded log link, reason for
  bridge/fallback, and explicit "repair now" action that calls the shared T241
  recert core.
- [x] Add Workday measurement ceremony:
  start snapshot, run normal CLI + Desktop Slimference sessions, close sessions
  for WSS flush, run `aggregate-savings --period=today`, record WSS/HTTP
  attribution, fallback events, recert events, and qualitative no-drawdown notes.
- [x] Make Workday measurement route-aware. `aggregate-savings` and
  `workday-savings finish` now preserve the current Codex auto-route and
  auto-recert snapshot, including mode, transport, WSS cert/bridge state,
  `needs_recert`, fallback reason, recert status, attempt id, timestamps, last
  error, and bounded log path. Delta notes call out route, recert, and repair
  state changes.
- [x] Harden measurement/status hygiene around the route-aware slice:
  zero-value recert timestamps are omitted from JSON/text reports instead of
  leaking `0001-01-01`, help text and runbook examples use the canonical
  `~/.slimference/filter.db`, and the TUI/Launch Center accepts both persisted
  `desktop_app_server_proven` and raw `desktop_app_server_phasef_proven` Desktop
  proof modes while still distinguishing "WSS savings active" from "WSS route
  ready".
- [ ] Only after measured proof, evaluate L2/L3 WSS candidates. Required before
  default-on: fixture, live proof, no schema drift, no model-quality loss, no
  prompt-cache breakage, and clear rollback/fail-open path.
- [x] Add proof-gated planner candidates for Codex WSS L2/L3. Even when L2 is
  enabled and Codex WSS carries repeated tool output or `previous_response_id`,
  the planner now reports L2/L3 as `shadow` candidates with explicit
  fixture/live-proof reasons, not `run`. HTTP behavior and provider-reported
  prompt-cache accounting remain unchanged.
- [x] Derive `repeated_tool_output` planner content class from real repeated
  tool commands/read keys in parsed messages. This makes adaptive cache/L2
  candidate visibility come from actual tool-output structure instead of only
  hand-authored planner facts, while keeping Codex WSS L2/L3 proof-gated as
  `shadow`.
- [x] Add WSS request-body planner summaries to decisions logs. In addition to
  the existing upgrade-level route record, each parsed Codex WSS client request
  now records content-free body facts: route `websocket_phasef`, model,
  session key, previous-response state, message count, token delta,
  `content_classes`, output-reduce reason, and the exact L2/L3/WebSocket
  planner decisions.
- [x] Replace readcache changed-read set diffs with position-aware line hunks.
  Changed-file rereads now preserve indentation-only changes, duplicate lines,
  and moved context instead of relying on trimmed whole-line membership.
- [x] Feed WSS-observed edit/write/apply_patch operations into the existing
  recently-edited file guard. Codex WSS no longer depends only on external hook
  state before deciding whether a reread may be safely compacted.
- [x] Keep WSS terminal `response.completed` payloads byte-equal for repdet.
  Streaming text deltas still use repdet where safe; terminal aggregates no
  longer double-count streaming savings or risk corrupting final code/patch
  output.
- [x] Harden deterministic filter quality before broadening savings:
  build/test/lint compactors run before generic log compaction, container
  tables preserve unhealthy rows, and diagnostic JSON keeps error/message
  values instead of collapsing to schema-only summaries.
- [x] Reconcile Layer 2 default truth across runtime defaults, generated TOML,
  CLI copy, and data-policy docs. Fresh configs keep Layer 2 disabled; explicit
  opt-in configs remain honored.
- [x] Add `scripts/utils wss-audit` for content-free Codex WSS decisions-log
  audits. It reports route coverage, Phase-F request counts, session-key
  continuity, `previous_response_id` usage, content classes, and positive input
  savings without raw frame dumps.
- [x] Add hard gates and time windows to `wss-audit`. Operators can now isolate
  a fresh measurement window with `--since=<rfc3339>` and fail the run if
  distinct session ids, Phase-F request summaries, or positive savings are
  missing.
- [x] Harden `wss-audit` as an automation gate. JSON output now returns the
  same non-zero gate failure code as text output, and `--since` windows exclude
  untimed records so stale legacy summaries cannot pollute fresh measurements.
- [x] Wire WSS BeTerse treatment/control outcomes into the existing
  `qualityab` rollback harness on terminal frames. Successful completed frames
  record success; incomplete/failed terminal frames record upstream failure.
  This brings the live output-reduce safety net onto the Codex WSS path without
  broadening mutation.
- [x] Split billable input savings from output-wire telemetry in operator
  reporting. `aggregate-savings` now explicitly states that Output-Reduce
  counters are not included in billable input-token totals.
- [x] Add WSS re-read canary telemetry to the product path. Each parsed
  Codex WSS request now records repeated read/tool keys in the same
  content-free request summary used by `wss-audit`, so future drift analysis
  can distinguish useful repeat-read savings from possible context-recall
  pressure without logging raw tool output.
- [x] Neutralize model-facing readcache marker wording. Read-delta and
  unchanged-read replacements no longer inject the product name into tool
  output, while preserving archive URI patterns and fail-open reconstruction.
- [x] Harden WSS session identity preference. Codex turn metadata now wins over
  `prompt_cache_key` when both are present; `prompt_cache_key` remains a last
  resort for frames without a stronger per-thread/per-session identifier.
- [x] Make operator cost estimates billable-input based. The savings probe now
  estimates cost from input tokens saved, not output-wire byte telemetry.
- [x] Add exact cross-turn non-file tool-output dedup. For non-read commands,
  the shared Codex reducer now records the exact output text it would actually
  send upstream after deterministic filters. If the same session later produces
  the same resolved command and the same emitted output, it replaces the repeat
  with a neutral archive-backed "unchanged since previous emitted output" note.
  Changed outputs, short outputs, archive failures, read commands, and
  unresolved commands fail open.
- [x] Surface repeated-output attribution through Layer-0 telemetry,
  `/admin/state`, `aggregate-savings`, and `workday-savings` route deltas via
  `proxy_layer0_repeated_output_blocks` / `repeated_output_blocks`.

## Notes

- Update guard truth: Codex updates cannot be prevented, but unsafe mutation
  after an update is prevented. The best user experience is automatic repair:
  detect drift, keep native WSS bridge when safe, run background recert, restore
  `wss_phasef` after proof, and expose only exact reasons if repair fails.
- WSS is the primary Codex product route because it preserves native Codex
  conversation semantics and now has both CLI and Desktop mutation proof.
- HTTP is still valuable: fallback, hook/filter accounting, and older
  compatibility surfaces. It should reuse the same safe reducer core where the
  payload semantics match.
- Do not chase the repeated `instructions` / `tools` block. OpenAI's
  `prompt_cache_key` is already the billing/cache mechanism for that stable
  prefix. Local mutation there can reduce quality or break cache semantics.
- Voice/realtime remains passthrough until a future explicit task proves a safe
  non-semantic optimization. Current expectation: no useful savings there.
- Current slices are intentionally attribution/observability-only. They do not
  add new mutation surfaces, so they cannot make the model weaker; they make the
  next optimization decisions measurable.
- Opportunity counters are separate from success counters by design. A
  tool-result block, resolved command, or read-delta attempt can be counted even
  when no mutation happens. `proxy_layer0_blocks` and the mechanism-hit counters
  still require positive token savings, so `aggregate-savings` cannot inflate
  results from misses.
- Route attribution is diagnostic only. It does not enable new mutation, but it
  lets the next optimization loop distinguish HTTP fallback behavior from WSS
  Phase-F behavior without reading raw frames again.
- Command-array normalization is a safe shape expansion, not semantic
  summarization: it only recovers the actual shell command that Codex already
  executed, then routes the captured output through existing deterministic
  filters and token-decreasing guards.
- Workdir-aware readcache keys are also a safety improvement: `cat shared.txt`
  in two different repos no longer shares one session cache entry, while repeat
  reads in the same repo become easier to identify.
- Single-text-part output-array support is a safe shape expansion: it only
  mutates a uniquely addressable text field and never converts multimodal or
  multi-text arrays into strings.
- Nested MCP-style output support follows the same rule: only a single
  reconstructable text part is eligible, and object metadata remains intact.
- Large observed read outputs are now archive-backed instead of inlined into
  readcache session JSON. The cache stores hash/archive URI, expands the archive
  only to build exact unchanged/delta replacements, and fails open if the
  archive is missing or the delta is not shorter. This increases repeat-read
  hit-rate without touching prompt-cache blocks or semantic content.
- Recert UX is no longer a black box: `/admin/state`, `codex status`, and the
  TUI surface attempt id, started/finished/last-success/retry times, last error,
  and the bounded log path. The existing TUI repair action continues to call the
  shared `slimference codex recertify wss` core.
- `workday-savings start|finish` records a baseline/current counter window and
  prints honest deltas. It explicitly reminds operators to close Codex sessions
  before finish so WSS counters flush. The finish report carries the current
  Codex route/recert snapshot and notes route or repair-state changes, so
  fallback/recert events are measured alongside token savings instead of kept as
  external operator memory.
- Report hygiene matters because these files are operator evidence. Optional
  recert times stay absent until real values exist, the canonical filter DB path
  remains `~/.slimference/filter.db`, and Desktop status wording never collapses
  route-ready into savings-proven.
- Current WSS fixture expansion is still deterministic L0/read-delta work, not
  semantic compression. It proves more tool shapes can use the same safe reducer
  core; it does not enable L2 summaries, response-cache substitutions, prompt-cache
  block mutation, or voice/realtime mutation.
- L2/L3 WSS candidates are intentionally visible but non-mutating. The planner
  can now show where future value might exist, while the runtime remains protected
  until a separate fixture plus live proof upgrades a candidate from `shadow` to
  `run`.
- Repeated tool-output classification is conservative: it keys only on resolved
  Layer-0 command/read identities from current parsed messages and remembered
  same-request tool uses. It does not infer repeats from raw text alone, and it
  does not make WSS L2/L3 mutate anything.
- WSS request-body planner telemetry is content-free. It logs labels and
  counters, not frame payloads, headers, tool output text, or auth data. This
  closes the old observability gap where upgrade-level WSS records had
  `total_messages=0` and could not show real repeat candidates or L2/L3 proof
  gates.
- Position-aware read deltas deliberately prefer safety over maximum brevity.
  If a changed-read hunk would not be shorter than the original output, the
  existing token-decreasing guard fails open to the full content.
- The follow-up newline regression in changed-read hunks is fixed: the delta
  builder preserves existing line terminators instead of adding a second blank
  line between each diff row.
- WSS request-body summaries now carry `re_read_count`. This is a drift canary,
  not an automatic failure verdict: deliberate repeat-read workloads are also
  the highest-value savings workloads, so the counter must be interpreted
  alongside positive savings, recent-edit state, and future comprehension
  harness results.
- `wss-audit` reports re-read request/count totals. A non-zero value says the
  model requested the same resolved read/tool key again in the same WSS session;
  it does not log the file content, command output, or headers.
- Model-facing readcache markers now use neutral read notes instead of naming
  Slimference. This reduces prompt-contamination risk while keeping the marker
  mechanically parseable and shorter than the original content.
- Exact repeated-output dedup is intentionally last in the deterministic
  Layer-0 chain. It observes the candidate text after safe captured-output
  filters, and only if that candidate is the text that would be sent upstream.
  This prevents a marker from referring to a full raw output that the server
  never saw.
- The repeated-output cache is exact-hash only. It does not summarize, diff, or
  infer semantic equivalence for non-file commands. A changed `git status`,
  `rg`, `go test`, or custom command output is sent normally and becomes the new
  observed baseline.
- Partial file reads are no longer treated as full-file read-delta snapshots.
  Only full-file `cat` commands use read-delta. `head` / `tail` range outputs
  can still save through exact repeated-output dedup when the same emitted range
  repeats.
- The archive-reinjection system prompt contract remains proof-gated and is not
  default-on. It is a real recovery candidate, but injecting any new persistent
  instruction into Codex WSS requires the future comprehension harness before it
  can be promoted safely.
- The Desktop proof produced two defensible savings windows, not a
  contradiction: `wss-audit --since=...` reported the fresh decisions-log window
  (`tokens_saved=3151`), while `/admin/state` reported the daemon lifetime /
  dispatcher counter at read time (`input_tokens_saved=5966`). Proof language
  must name which window it cites.
- WSS edit observation is a guard, not a new mutation surface. It records only
  path-level edit evidence derived from reconstructable tool metadata or patch
  headers, then lets the existing readcache recent-edit policy decide.
- `wss-audit` is the cheap first check before any future cache frontier work:
  missing session ids, no `previous_response_id`, or no positive savings can be
  seen from content-free decisions logs before reaching for capture dumps.
- `wss-audit --since=<timestamp>` is mandatory for current session-key and
  socket-lifecycle checks because legacy debug logs can contain stale
  `upstream`/missing-session WSS records from older binaries.
- WSS quality rollback is currently scoped to the gated BeTerse lever, matching
  the existing HTTP harness semantics. Deterministic Layer-0 read/filter
  mutations stay protected by schema reconstruction, token-decrease checks,
  recent-edit guards, and byte-equal fail-open behavior rather than cohort
  rollout.
- Savings-proven and comprehension-preserved are separate claims. Current WSS
  proofs establish route, mutation, and input-token savings for repeat-read
  workloads. Comprehension preservation is supported by conservative reducers
  and fail-open guards, but the stronger claim requires the future re-read
  canary interpretation plus an offline A/B harness before broader semantic
  compression or archive-instruction recovery can be default-on.
- Fresh 2026-05-30 CLI session-key audit: two separate scoped Codex CLI
  conversations produced two distinct non-empty WSS session ids
  (`019e7649-2076-7030-a341-6b3ea00ae448` and
  `019e7649-4315-7c62-a272-c45f1aafdd24`) with
  `--expect-distinct-sessions=2 --min-phasef=2` passing. This does not prove
  every Desktop/App lifecycle, but it rejects the current CLI prompt-cache-key
  collision hypothesis for fresh conversations.
- Fresh 2026-05-30 CLI resume audit: `codex exec` followed by
  `codex exec resume --last` kept one stable WSS session id, used
  `previous_response_id` four times, and produced real positive WSS savings:
  `positive_savings_requests=1`, `tokens_saved=2815`,
  admin/state `frames_reencoded=1`, `compressed_messages_mutated=1`,
  `phasef_mutations=1`, `parse_failures=0`, `degraded_sessions=0`, and
  `compression_errors=0`. The mutation was attributed to the WSS Phase-F
  captured-output Layer-0 route.
- The resume audit also showed two upgrade-level records without session ids.
  Those records did not carry positive savings; the body-level request summaries
  did carry the stable session id. Keep the missing-session warning because it
  remains useful evidence when old or upgrade-only records are mixed into a
  measurement window.
- Fresh 2026-05-30 Desktop app audit: Codex.app launched through
  `slimference codex launch-desktop --replace-existing`, then three separate
  Desktop prompts reread `docs/todo/t248-unified-codex-savings-engine.md`.
  `wss-audit --since=2026-05-30T00:40:07Z --min-phasef=3 --require-savings`
  passed with `phasef_requests=23`, `unique_sessions=6`,
  `previous_response_id_used=8`, `positive_savings_requests=1`, and
  `tokens_saved=3151`. Admin state after flush showed
  `phasef_bridged=11`, `frames_reencoded=2`,
  `compressed_messages_mutated=2`, `phasef_mutations=2`,
  `input_tokens_saved=5966`, zero parse/degrade/compression errors, and
  all billable savings attributed to `proxy_layer0_routes.wss_phasef`.
  The scoped Desktop helper processes were terminated after the proof so
  Finder/Spotlight launches return to normal direct routing.
- Open max-out plan after T256:
  - T252 closes the remaining low-risk precision gap by auditing every
    diagnostic cap in `internal/filter` and proving late diagnostics survive.
  - T257 supplies the real workload proof matrix: 10 CLI/Desktop captures plus
    workday windows, all replayed through the A/B gate before stronger claims.
  - T258 makes the auto policy a full route/workload/risk/recovery/recency
    autopilot instead of scattered mechanism checks.
  - T253 adds high-upside first-read/predictive/patch/reasoning candidates only
    in shadow/proof mode until T257/T258 promote them.
  - T254 is design-first and shadow-first; it cannot mutate until no-false-elision
    is proven against captured `previous_response_id` chains.
  - T259 keeps HTTP honest: either prove route-specific archive recovery or keep
    archive-reference mechanisms WSS-only.

## Deviations

None.
