# TASK 248: Unified Codex savings engine for WSS and HTTP

Status: ACTIVE - shared attribution plus opportunity telemetry implemented
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
  HTTP-path Layer-0/filter savings without double counting.
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
- [ ] Split the current package-local helper into an explicit shared Codex
  reducer API once the attribution fields prove stable. The API should accept
  parsed messages, session id, remembered tool uses, and route label
  (`wss_phasef`, `http`, `wss_bridge` no-mutation) and return rewritten
  messages plus typed stats.
- [ ] Expand Codex tool-shape coverage based on real captured frames only:
  `exec_command`, `local_shell_call`, `shell_call`, direct read tools, MCP-style
  outputs, nested output arrays, and future Codex tool variants. Every new shape
  needs a fixture and fail-open behavior for unknown input.
- [ ] Improve session/turn-aware cache policy for repeated tool outputs:
  maximize readcache hit-rate across turns without touching prompt-cache blocks,
  recently-edited files, voice/realtime, or non-reconstructable content.
- [ ] Design and prove a "cache frontier" for WSS:
  local archive references, read-delta markers, and exact repeated output
  suppression are allowed; semantic summaries and response cache substitutions
  require separate proof because Codex WSS uses `previous_response_id` server
  state.
- [ ] Add recert UX hardening if gaps remain after live observation:
  auto-recert status in TUI, last attempt age, bounded log link, reason for
  bridge/fallback, and explicit "repair now" action that calls the shared T241
  recert core.
- [ ] Add Workday measurement ceremony:
  start snapshot, run normal CLI + Desktop Slimference sessions, close sessions
  for WSS flush, run `aggregate-savings --period=today`, record WSS/HTTP
  attribution, fallback events, recert events, and qualitative no-drawdown notes.
- [ ] Only after measured proof, evaluate L2/L3 WSS candidates. Required before
  default-on: fixture, live proof, no schema drift, no model-quality loss, no
  prompt-cache breakage, and clear rollback/fail-open path.

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

## Deviations

None.
