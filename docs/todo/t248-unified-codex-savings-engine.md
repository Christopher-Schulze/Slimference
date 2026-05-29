# TASK 248: Unified Codex savings engine for WSS and HTTP

Status: ACTIVE - shared attribution, opportunity telemetry, route attribution, report/status hygiene, tool-shape fixtures, and proof gates implemented
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

## Deviations

None.
