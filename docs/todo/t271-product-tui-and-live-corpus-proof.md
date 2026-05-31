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
2. [~] Map existing counters into product groups:
   - route
   - billable input savings
   - output-wire savings
   - provider cache (pending source alignment)
   - cache hits
   - quality/safety
   - recert
3. [x] Clean TUI product surface:
   - default right panel now shows route, billable input saved, output-wire
     bytes, cache hit/miss totals, read/repeated/chunk hits, and safety/host
     budget state
   - debug-only parser/policy/cache matrices remain outside the default product
     panel
   - route labels come from `/admin/state` through the local/remote TUI adapters
4. [ ] Define live-corpus promotion gates:
   - minimum CLI captures
   - minimum Desktop captures
   - required workload classes
   - zero lost context
   - zero parse/degrade/compression errors
   - no canary spikes
   - positive net savings after overhead
5. [ ] Add release proof ceremony:
   - start clean
   - launch CLI and Desktop through product path
   - run required workloads
   - finish workday windows
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
