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
  cache hit/miss counts, read/repeated/chunk hits, tool-resolution misses, and
  safety issues.
- The TUI normal view now consumes the product rollup instead of rebuilding a
  mixed savings headline from raw debug counters. Debug views still keep the
  raw counters.
- WSS parse failures, degraded sessions, compression errors, and host-budget
  attention now force product `attention` status and render concrete WSS safety
  details in the normal TUI product panel.
- Real proof matrix and workday windows exist, but promotion criteria need to
  be explicit for every max-out feature.

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
4. [x] Define live-corpus promotion gates:
   - `benchmark-corpus --promotion-check` is the release/default-on gate, kept
     separate from the normal synthetic CI corpus gate
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
