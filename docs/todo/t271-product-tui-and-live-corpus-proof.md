# T271 - Product TUI signals and live-corpus proof gates

## Why

The user-facing product must show truth, not debug internals. Separately, no
aggressive default should be promoted from unit tests alone. This task makes the
TUI and proof corpus reflect the real product contract: route health, savings,
fallbacks, cache hits, recovery, and quality signals.

## Current reality check

- Admin state and audit tools expose many counters.
- `/admin/state` now exposes `savings.product`, a product-facing rollup for
  status, billable input savings, output-wire savings, request-side reductions,
  tool-prune input-token savings, output-reduce observed-output proof status,
  cache hit/miss counts, read/repeated/chunk hits, tool-resolution misses, and
  safety issues.
- The TUI normal view now consumes the product rollup instead of rebuilding a
  mixed savings headline from raw debug counters. Debug views still keep the
  raw counters.
- WSS parse failures, degraded sessions, compression errors, and host-budget
  attention now force product `attention` status and render concrete WSS safety
  details in the normal TUI product panel.
- Real proof matrix, workday windows, promotion criteria, and maxx corpus gates
  exist for the current product feature set.

## Product target

TUI normal view:

- route: WSS savings active, WSS bridge, HTTP fallback, direct
- savings: billable input saved, output-wire saved, provider cache saved
- cache: read/ranged/search/repeated/chunk hits
- safety: parse/degrade/compression errors, re-read canary, recovery loops
- recert: current, repairing, failed with reason
- no parser miss matrix, policy internals, or raw debug counters

Debug/audit view:

- full mechanism counters
- route attribution
- proof blockers
- capture/replay summaries
- bounded recert logs

## Technical work packages

1. [x] Define product signal schema for `/admin/state`.
2. [x] Map existing counters into product groups:
   - [x] route
   - [x] billable input savings
   - [x] output-wire savings
   - [x] provider cache read/create tokens from analytics
   - [x] cache hits
   - [x] quality/safety
   - [x] recert
3. [x] Clean TUI product surface:
   - default right panel now shows route, billable input saved, output-wire
     bytes, cache hit/miss totals, read/repeated/chunk hits, and safety/host
     budget state
   - debug-only parser/policy/cache matrices remain outside the default product
     panel
   - route labels come from `/admin/state` through the local/remote TUI adapters
   - provider-cache read/create tokens now flow through `/admin/state.savings`
     into the product panel as a separate savings class, not mixed into local
     Layer-0 input savings
   - tool-prune saved tokens are counted into billable input savings and shown as
     their own mechanism line; missing-tool retry/miss events force product
     attention
   - output-reduce is shown only as injection/observed-output status and input
     overhead; it is not rendered as a billable-input savings claim
4. [x] Define live-corpus promotion gates:
   - `benchmark-corpus --promotion-check` is the release/default-on gate, kept
     separate from the normal synthetic CI corpus gate
   - `benchmark-corpus --maxx-check` includes the promotion gate and additionally
     requires mechanism-specific live workload classes for chunk dedup,
     output-reduce, tool pruning, provider-cache long sessions, and
     host-resource workdays
   - requires at least five `codex_cli` sessions and five `codex_desktop`
     sessions
   - requires live workload classes: `repeat_read`, `ranged_read`,
     `search_loop`, `git_status`, `test_failure`, `apply_patch_edit_read`,
     `large_tool_output`, and `long_workday`
   - ignores synthetic categories for promotion
   - every real category must carry `client_family`, `workload_class`,
     `evidence_level=live_operator`, explicit zero error budget, explicit
     re-read canary budget, explicit latency budget, and a positive savings
     floor
   - category failures are promoted into the release verdict, so positive
     savings cannot mask parse/degrade/error/canary regressions
5. [x] Add release proof ceremony:
   - start clean
   - launch CLI and Desktop through product path
   - run required workloads
   - finish workday windows including host-resource snapshot
   - export proof report

## Zero product-drawdown gates

- TUI cannot label "route ready" as "savings active".
- TUI cannot hide fallback or degraded state.
- Promotion cannot rely on synthetic fixtures only.
- A feature cannot be called default-safe if live-corpus proof shows repair,
  re-read, fallback, or latency regression.

## Savings targets

- Product TUI shows only numbers that a user can act on.
- Proof reports include per-mechanism net savings and overhead.
- No single mixed "magic savings" headline.

## Verification

- TUI rendering tests for product states.
- Admin state schema tests.
- Proof matrix command tests.
- Real CLI/Desktop workday windows before default promotion.

## Done

The TUI is done when a normal user can see whether Slimference is saving, why it
is not saving, and whether it is safe, without reading debug counters. The proof
gate is done when default promotions require live corpus evidence.

## Progress

- 2026-05-31: Added the opt-in live-corpus promotion gate to
  `benchmark-corpus`. The normal corpus gate remains CI-friendly for synthetic
  smoke data, while `--promotion-check` fails closed unless real CLI/Desktop
  sessions cover the required workload classes and each category declares
  explicit safety, latency, re-read, and savings expectations.
- 2026-05-31: Added `go run ./scripts/verify -mode release-proof-plan` as the
  deterministic release/default-on ceremony. It prints the clean CI baseline,
  real workday window, scoped CLI/Desktop launch paths, all required
  live-corpus category plans for both clients, WSS proof-matrix command, and the
  promotion corpus gate. It remains content-free and operator-driven; no live
  capture is automated.
- 2026-05-31: Product TUI route line now includes fallback reason and recert
  status in the normal product surface, closing the remaining route/safety
  visibility gap without exposing parser or policy debug internals.
- 2026-06-02: Release proof ceremony now explicitly requires host-resource
  measurement and host-resource budget pass alongside CI, WSS replay, workday
  savings, and promotion corpus gates. This keeps "savings proven" separate
  from "safe and cheap enough for product operation."
- 2026-06-02: Release proof ceremony now calls
  `wss-proof-matrix --require-live-token-delta`, so replay byte savings cannot
  satisfy the product gate without real live billable-token deltas.
- 2026-06-02: Added two automatic scoped CLI release-style proof windows without
  Desktop operator input. Search breadth:
  `/Users/christopher/.slimference/captures/auto-proof-search-clean-20260602T004340Z.jsonl`
  replayed with `lost=0`, `gate_passed=true`, `mutated_requests=13`, and
  `bytes_saved=146507`; the matching workday window saved 45273 WSS input
  tokens with zero parse/degrade/compression errors. Git status:
  `/Users/christopher/.slimference/captures/auto-proof-git-status-20260602T004545Z.jsonl`
  replayed with `lost=0`, `gate_passed=true`, `mutated_requests=3`, and
  `bytes_saved=4128`; its workday window saved 1518 WSS input tokens and ended
  with host budget `ok`. Desktop breadth still remains an operator-driven live
  proof requirement, but CLI search/git breadth is no longer speculative.
- 2026-06-02: Product safety status now includes WSS parse/degrade/compression
  failures through the `/admin/state` build path and TUI adapter fallback. The
  normal product panel renders those concrete WSS counters, so a degraded WSS
  path cannot be presented as "safety ok."
- 2026-06-02: Existing release matrix
  `/Users/christopher/.slimference/captures/release-proof-20260602_112516-cli-desktop-v2.jsonl`
  now passes `go run ./scripts/utils wss-proof-matrix ... --require-live-token-delta --json`
  with `captures=15`, `cli=9`, `desktop=6`, all required release workload
  classes present, `positive_token_savings_captures=12`,
  `expected_zero_captures=3`, `captures_with_issues=0`, and
  `gate_passed=true`. This proves the base CLI+Desktop WSS release matrix, but
  it does not close the separate chunk-dedup, tool-schema pruning,
  output-reduce aggressive-profile, or host-resource proof gates.
- 2026-06-02: `codex-capture-run` now accepts `--transport` with default
  `auto`. Focused mechanism proofs can force `wss`; release proof should keep
  `auto` to test the product route. The README examples now include the required
  `exec` Codex subcommand and call out forced-WSS mechanism proof usage.
- 2026-06-02: Product TUI now separates billable input tokens, request-side byte
  reductions, and output-wire byte savings in the default product panel. This
  keeps the user-facing surface honest: token savings are the money claim,
  request bytes are local reducer efficiency, and output-wire bytes are UX/stream
  savings rather than billable input savings.
- 2026-06-03: Removed the remaining legacy right-panel fallback that rebuilt a
  mixed percent/snapshot savings headline when product status was empty. The
  default product panel now always renders the product rollup shape, including
  explicit zero values for output-wire and provider-cache savings, so the TUI
  cannot blur `0` with "metric absent" and cannot invent a second headline from
  raw debug counters.
- 2026-06-03: Added a separate `benchmark-corpus --maxx-check` gate. The normal
  CI corpus gate stays synthetic-friendly, `--promotion-check` proves the base
  CLI/Desktop release matrix, and `--maxx-check` fails closed until the
  mechanism-specific workload classes for chunk dedup, output-reduce, tool
  pruning, provider-cache long sessions, and host-resource workdays are all
  present as live operator evidence.
- 2026-06-03: Added `wss-proof-inventory`, a content-free inventory command for
  local proof-matrix rows. It ignores raw WSS frame payloads, aggregates clients,
  workload classes, expected reducers, live reducer hits, host-budget-ok rows,
  positive-token rows, and missing release/maxx workload classes. The earlier
  local capture inventory reported 13 matrix files, 65 rows, 48 CLI rows,
  17 Desktop rows, 49 positive-token rows, zero safety-issue rows, complete base
  release workload coverage, and these maxx gaps:
  `chunk_dedup_log_output`, `chunk_dedup_test_output`,
  `output_reduce_aggressive`, `tool_heavy`, `provider_cache_long_session`, and
  `host_resource_long_workday`.
- 2026-06-03: Extended proof live signals for Layer 3. `codex-capture-run`
  matrix rows now carry provider-cache read/create token deltas, and
  `wss-proof-matrix` / `wss-proof-inventory` can check
  `provider_cache_read` and `provider_cache_create` separately from local
  billable-input reducer savings. This makes the future
  `provider_cache_long_session` maxx category proofable by actual provider-cache
  counters instead of workload name alone.
- 2026-06-03: Extended `wss-proof-inventory` from a presence checklist into a
  maxx workload status gate. It now reports, for each maxx workload, row count,
  positive-token rows, host-budget-ok rows, safety issues, live reducer hits,
  missing required signals, and a `complete` boolean. At that point, local
  captures showed `chunk_dedup_similar_outputs` complete and the remaining maxx
  gaps precisely:
  `chunk_dedup_log_output` and `chunk_dedup_test_output` have no live chunk-ref
  rows yet, `output_reduce_aggressive` has no `output_reduce_injected` row,
  `tool_heavy` has no `tool_prune` row, `provider_cache_long_session` has no
  `provider_cache_read` row, and `host_resource_long_workday` has no
  `host_budget_ok` row.
- 2026-06-03: Corrected the maxx inventory completion semantics so each workload
  is judged against its own economic signal instead of a single local Layer-0
  billable-input counter. Chunk workloads still require positive billable-input
  savings, provider-cache long sessions require `provider_cache_read`,
  tool-heavy workloads require `tool_prune_tokens_saved`, output-reduce
  aggressive rows require `output_reduce_injected`, and host-resource workdays
  require `host_budget_ok`.
- 2026-06-03: Refreshed the inventory after the WSS provider-cache accounting
  fix. At that point, local captures reported 15 matrix files, 67 rows, 50 CLI rows,
  17 Desktop rows, 49 positive-token rows, zero safety-issue rows,
  `provider_cache_long_session` complete with `provider_cache_read=3456` and
  `host_budget_ok`, and the remaining maxx gaps are now exactly:
  `chunk_dedup_log_output`, `chunk_dedup_test_output`,
  `output_reduce_aggressive`, `tool_heavy`, and
  `host_resource_long_workday`.
- 2026-06-03: Provider-cache long-session CLI proof is now complete in the local
  inventory. After WSS `response.completed` usage accounting was wired into
  analytics, the fixed capture matrix row reports `provider_cache_read=3456`,
  `host_budget_ok=1`, zero safety issues, and a passing focused matrix gate.
  Current remaining maxx gaps are now `chunk_dedup_log_output`,
  `chunk_dedup_test_output`, `output_reduce_aggressive`, `tool_heavy`, and
  `host_resource_long_workday`.
- 2026-06-03: Corrected `wss-proof-inventory` maxx semantics for large log/test
  outputs. These rows now complete when the product path saves through either
  the stricter deterministic `captured_output` reducer or recoverable
  `chunk_dedup_refs`, plus `host_budget_ok`, positive live token savings, and
  zero safety issues. This keeps the gate aligned with the no-drawdown policy:
  the proof must show the safest productive reducer won, not that a lower-priority
  fallback displaced it.
- 2026-06-03: Codex WSS output-reduce is now offline-verified and code-reachable
  for the `output_reduce_aggressive` maxx gate. The WSS adapter injects only into
  top-level Codex `instructions`, never into `input`, and only on prompt/user-turn
  bodies. Tool-output deltas containing `function_call_output` remain byte-equal
  unless a dedicated tool-output reducer changes them. The inventory gap is now a
  real live-capture gap, not missing WSS wiring.
- 2026-06-03: Codex WSS tool-schema pruning is now offline-verified and
  code-reachable for the `tool_heavy` maxx gate. Tool-call frames observe
  session activity; `tools[]` pruning runs only on prompt/user turns with known
  Codex schema, and unknown schemas full-pass. The remaining `tool_heavy` proof
  gap is a real focused live-capture gap.
- 2026-06-03: Tightened maxx inventory semantics for the host-resource long
  workday gate. `host_resource_long_workday` no longer completes from
  `host_budget_ok` alone; it must also carry positive live billable input-token
  savings. A host that stays green while nothing is saved is useful telemetry,
  but it is not a max-out proof.
- 2026-06-03: Added `wss-proof-export-corpus`, which converts local
  proof-matrix rows into content-free `benchmark-corpus` categories. The export
  path now maps `large_tool_output`, chunk log/test, provider-cache, and
  host-resource rows into scrubbed live-corpus categories without raw WSS frames,
  auth, prompts, command output, or file paths.
- 2026-06-03: Refreshed the local proof inventory and exported corpus after the
  WSS command-inference, log/test inventory, and large-output export fixes.
  At that point, inventory reported 16 matrix files, 81 rows, 64 CLI rows,
  17 Desktop rows, 54 positive-token rows, 0 safety-issue rows. The exported corpus had
  49 real rows across 17 categories. `benchmark-corpus --promotion-check`
  passed. `benchmark-corpus --maxx-check` still failed only on
  `output_reduce_aggressive` and `tool_heavy`, both of which are now code-ready
  but still require fresh live captures.
- 2026-06-03: Hardened the focused proof tooling. `codex-capture-run
  --expected-reducer` validates the requested reducer against the live
  admin-state delta before returning PASS. Failed expected-reducer runs now
  still append the matrix row first, so negative evidence such as host-budget
  attention or a missing reducer hit remains auditable instead of disappearing.
  This closes the previous honesty hole where a focused proof could carry an
  expected reducer in metadata without proving that it fired.
- 2026-06-03: Extended `benchmark-corpus --maxx-check` with host-budget
  evidence. Session reports now aggregate `host_budget` / flat
  `host_budget_*` fields, render ok/issue counts, and support a
  `host_budget_ok` scenario validator. The `host_resource_long_workday`
  category therefore requires both real savings and a green resource guard in
  the corpus gate, not just workload presence.
- 2026-06-04: Refreshed the exported live corpus after the focused Desktop
  tool-heavy proof. `benchmark-corpus --promotion-check` passes on
  `tests/fixtures/live_corpus` with 54 total requests, 51 real live operator
  rows, 34 Codex CLI rows, 17 Codex Desktop rows, all release/maxx workload
  classes present, zero error rows, and `desktop_tool_heavy` proving
  `tool_prune.applied=true`, one pruned tool, 26 saved tokens, and host budget
  `ok`. Then hardened `benchmark-corpus --maxx-check` so
  `output_reduce_aggressive` must carry observed output-token evidence, not just
  WSS instruction injection plus provider-cache tokens. At that point the
  stricter gate correctly failed only on the missing observed output-token row;
  the later focused proof below closed that gap.
- 2026-06-04: Unified proof inventory token-evidence semantics with
  `wss-proof-matrix`: tool-prune saved tokens, provider-cache read tokens, and
  guarded output-reduce observed-output evidence now all count as positive
  economic token rows for inventory visibility. The current local inventory has
  57 positive token rows, `tool_heavy.positive_token_rows=1`, and zero safety
  issue rows.
- 2026-06-04: Hardened matrix, inventory, and export semantics so that
  `output_reduce_aggressive` requires observed output tokens, not only WSS
  instruction injection. Existing stale corpus rows still fail the maxx gate,
  while future `wss-proof-export-corpus` runs will not export such rows as
  economic evidence.
- 2026-06-04: Product TUI rollup now includes the maxx mechanism signals that
  were previously missing from the default surface. Tool-prune saved tokens are
  counted into `/admin/state.savings.product.billable_input_tokens_saved`, shown
  as a short product mechanism line, and miss/retry recovery events force product
  attention. Output-reduce appears only as `inj`, observed output tokens, and
  added input overhead; injection-only sessions render as proof-pending instead
  of a savings claim. Focused tests cover the control rollup, proxy probe,
  TUI adapter, and main-view rendering.
- 2026-06-04: Closed the remaining maxx corpus blocker. WSS
  `response.completed` usage now feeds the output-reduce tracker with observed
  output tokens, and a fresh focused CLI proof exported into
  `cli_output_reduce_aggressive` carries `output_reduce_injected`, 154 observed
  output tokens, `host_budget_ok`, zero safety errors, and no re-read signal.
  `benchmark-corpus --promotion-check` and `benchmark-corpus --maxx-check` now
  both pass on `tests/fixtures/live_corpus` with 51 real live operator rows
  across the release and maxx workload classes.
- 2026-06-05: Refreshed the corpus after the autonomous Python `unittest`
  Layer-0 proof. `cli_test_failure` now carries three real rows and 13691 saved
  tokens, including a stdlib `python3 -m unittest discover -s tests -v` WSS
  capture with `codex_exec_envelope=1`, `host_budget_ok`, `lost=0`, zero safety
  counters, and 1610 billable input tokens saved. `benchmark-corpus
  --promotion-check` and `benchmark-corpus --maxx-check` pass on
  `tests/fixtures/live_corpus` with 53 real live sessions / 55 total requests.
