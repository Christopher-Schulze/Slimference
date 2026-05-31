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

1. Add host-budget telemetry:
   - process RSS
   - CPU estimate
   - per-mechanism latency histogram
   - disk write counters
   - state sizes
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

## Done

The host budget is maxxed when savings mechanisms are not just correct but
cheap, bounded, observable, and automatically demoted before they hurt the
operator's machine or Codex workflow.
