# T272 - Host resource and latency budget max-out

## Why

Host overhead is a product drawdown when it makes Codex feel slower, burns CPU,
uses too much memory, writes too much disk, or destabilizes the daemon. Max
savings are not useful if the local machine becomes worse to use. This task
sets hard budgets and auto-degradation rules.

## Current reality check

- The dominant latency is upstream model inference, but local overhead still
  matters in WSS frame parsing, permessage-deflate, archive writes, readcache,
  chunking, JSON parsing, and TUI/admin polling.
- `/admin/state` now includes `host_budget`, a content-free product guard that
  reports `ok`, `unknown`, or `attention` from the daemon RSS field and WSS
  parse/degrade/compression state. The daemon state is populated from an
  in-process probe with PID, uptime, real RSS, real process CPU time, lifetime
  CPU percentage, and bounded state-directory size when platform sources are
  available, so the budget no longer depends on a loopback self-health call.
  State-size overrun now feeds the same `HostBudgetExceeded` demotion input as
  RSS and WSS parse/compression errors. CPU is reported but not used as an
  automatic demotion trigger until an idle/windowed sampler exists.
- Some performance tasks exist, but a single product budget across mechanisms is
  needed.

## Product target

The default product path must stay lightweight:

- low added latency
- bounded RSS
- near-zero idle CPU
- bounded disk writes
- no unbounded session state
- auto-disable expensive mechanisms when the host budget is exceeded

## Budgets

Initial targets for Apple Silicon macOS:

- idle CPU: <= 0.5%
- RSS: <= 200 MB normal operation
- added p95 proxy latency: <= 25 ms for normal Codex frames
- WSS mutation p95: <= 10 ms for typical tool-output frames
- disk writes: bounded and batched; no per-frame sync writes for hot state
- state: TTL/LRU bounds for readcache, tooluse, chunk store, archive metadata

## Technical work packages

1. [~] Add host-budget telemetry:
   - [x] product `host_budget` status in `/admin/state`
   - [x] process RSS source alignment for daemon/admin state
   - [x] CPU estimate
   - [x] per-mechanism latency histogram
   - [ ] disk write counters
   - [x] state sizes
2. Add pprof/benchmark ceremony:
   - real CLI session
   - real Desktop session
   - repeat read
   - search loop
   - chunk-dedup workload
   - long workday
3. Add auto-degradation:
   - skip expensive chunking when output is too small
   - disable aggressive mechanisms under latency pressure
   - reduce TUI/admin polling if idle CPU climbs
   - force async/batched flush for hot state
4. Optimize only with evidence:
   - lazy JSON parsing for hot WSS request fields
   - copy-on-write body mutation
   - avoid full-body unmarshal for unneeded frames
   - evaluate faster compression libraries only after profiling
   - keep one stripped Go binary unless evidence proves split binary needed
5. Add resource proof output:
   - included in workday-savings finish
   - included in release certification
   - red/yellow/green thresholds in product status

## Zero product-drawdown gates

- If resource budget is exceeded, the product must loosen compression rather
  than degrade Codex UX.
- No mechanism can allocate unbounded session state.
- No hot path can sync-write per frame unless measured negligible and bounded.
- Performance optimizations cannot change model-facing semantics.

## Savings targets

- No direct token target. This task protects the savings stack by keeping it
  cheap enough to stay always-on.
- Mechanisms with poor savings-per-millisecond are demoted by policy.

## Verification

- Benchmarks for WSS mutation, chunking, readcache, archive write, planner.
- Live pprof from CLI and Desktop sessions.
- Resource budget tests where possible.
- Long-session state bound tests.
- `go run ./scripts/ci`

## Progress

- 2026-05-31: Added real process CPU time and lifetime CPU percentage to daemon
  state, plus bounded state-size measurement for content archive, read-cache,
  tool-use cache, and collapsed-key persistence. `/admin/health`, HTTP daemon
  probes, in-process daemon probes, and host-budget evaluation now carry these
  fields. State-size budget overrun demotes through the existing host budget
  path; CPU remains observation-only until a windowed idle sampler is built.
- 2026-05-31: Wired the latest `/admin/state` host-budget snapshot into the
  Codex Layer-0 savings policy hot path. When the product host budget is marked
  exceeded, read-delta, repeated-output, and chunk mechanisms full-pass instead
  of spending more local work. The hot path reads an atomic snapshot rather than
  re-measuring RSS/state size per frame.
- 2026-05-31: Added content-free Layer-0 mechanism latency histograms under
  `/admin/state.savings.proxy_layer0_latency`. The reducer records total,
  read-delta, structured-filter, repeated-output, and chunk-dedup durations and
  exposes rolling p50/p95/max/avg snapshots by route. This is debug/audit
  telemetry, not normal TUI clutter.

## Done

The host budget is maxxed when savings mechanisms are not just correct but
cheap, bounded, observable, and automatically demoted before they hurt the
operator's machine or Codex workflow.
