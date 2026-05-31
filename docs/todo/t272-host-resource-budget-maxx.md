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
  CPU percentage, windowed CPU percentage, OS-reported lifetime and windowed
  disk read/write operation counters, and bounded state-directory size when
  platform sources are available, so the budget no longer depends on a loopback
  self-health call. State-size, CPU-window, and disk-write-window overruns now
  feed the same `HostBudgetExceeded` demotion input as RSS and WSS
  parse/compression errors. Repeated Layer-0 latency budget breaches feed a
  separate latency demotion gate.
- Some performance tasks exist, but a single product budget across mechanisms is
  needed.
- Readcache and WSS tool-use/collapsed-key persistence now use in-memory
  state plus short write-behind flushes for hot-path updates. Reconnect safety
  stays same-process immediate because `Load` sees the in-memory merge before
  disk flush.

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

1. [x] Add host-budget telemetry:
   - [x] product `host_budget` status in `/admin/state`
   - [x] process RSS source alignment for daemon/admin state
   - [x] CPU estimate
   - [x] per-mechanism latency histogram
   - [x] disk write counters
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
   - [x] disable managed Codex reducers under repeated Layer-0 latency pressure
   - [x] reduce TUI product-status polling under host-budget attention
   - [x] force async/batched flush for readcache and WSS tool-use hot state
   - [x] demote managed reducers on windowed CPU/disk-write budget spikes
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
  path; CPU was initially observation-only until the later windowed sampler
  entry below.
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
- 2026-05-31: Added OS-backed lifetime disk I/O operation counters to the
  daemon/admin resource snapshot and `/admin/state.host_budget`. The source is
  `getrusage` on Unix platforms and fail-open unknown elsewhere. Disk counters
  are visibility-only for now; automatic demotion waits for a windowed sampler
  so long-running productive sessions are not punished for cumulative writes.
- 2026-05-31: Added an automatic Layer-0 latency demotion gate. Three consecutive
  reducer frames over the 25 ms budget force managed Codex reducers to full-pass
  with `latency_budget_full_context`; cheap frames recover the gate. This keeps a
  single spike from disabling savings while preventing repeated local overhead
  from becoming Codex UX degradation.
- 2026-05-31: Removed synchronous WSS tool-use/collapsed-key writes from the
  frame hot path. `toolusecache.MergeAsync` updates same-process memory
  immediately for reconnect hydration and flushes JSON state on a short
  write-behind delay; readcache already uses the same pattern. Tests prove no
  synchronous write, immediate cached `Load`, and later disk hydration.
- 2026-05-31: Reduced TUI product-status overhead. The model now refreshes
  product status on ticks/events and renders from the cached snapshot; when
  host-budget attention is active, the next tick slows from 500 ms to 2 s.
- 2026-05-31: Added windowed local resource samples. The daemon now carries
  CPU-window percentage and disk read/write operation deltas between probes;
  `/admin/state.host_budget` reports those values and demotes managed reducers
  on CPU-window or disk-write-window budget spikes.

## Done

The host budget is maxxed when savings mechanisms are not just correct but
cheap, bounded, observable, and automatically demoted before they hurt the
operator's machine or Codex workflow.
