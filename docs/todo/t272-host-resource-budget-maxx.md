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
- Exact token counting for large repeated model-facing texts now uses a bounded
  content-hash cache. It keeps the o200k/cl100k token counts exact while avoiding
  repeated regexp-heavy BPE passes on identical large Codex tool outputs.
- `aggregate-savings`, `workday-savings finish`, and the release-proof plan now
  surface the host-budget snapshot. Workday delta output carries final RSS,
  CPU-window, disk-write delta, state bytes, status, and attention notes, so a
  savings proof cannot omit local resource cost.

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
2. Add resource/profile and benchmark ceremony:
   - [x] deterministic local benchmark harness covers WSS/Layer-0 mutation,
     chunking, readcache, content archive, and planner overhead
   - [x] live CLI resource/profile run
   - [x] live Desktop resource/profile run
   - [x] repeat read
   - [x] search loop
   - [x] chunk-dedup workload
   - [x] long workday
3. Add auto-degradation:
   - skip expensive chunking when output is too small
   - [x] disable managed Codex reducers under repeated Layer-0 latency pressure
   - [x] reduce TUI product-status polling under host-budget attention
   - [x] force async/batched flush for readcache and WSS tool-use hot state
   - [x] demote managed reducers on windowed CPU/disk-write budget spikes
4. Optimize only with evidence:
   - [x] cache exact large-text token counts by encoding + length + SHA-256
     content hash
   - [x] lazy JSON parsing for hot WSS request fields
   - [x] copy-on-write body mutation
   - [x] avoid full-body unmarshal for unneeded frames
   - evaluate faster compression libraries only after profiling
   - keep one stripped Go binary unless evidence proves split binary needed
5. Add resource proof output:
   - [x] included in aggregate-savings and workday-savings finish
   - [x] included in release certification runbook
   - [x] red/yellow/green thresholds in product status

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
- Live resource/profile bundles from CLI and Desktop sessions.
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
  Codex Layer-0 savings policy hot path. The hot path reads an atomic snapshot
  rather than re-measuring RSS/state size per frame.
- 2026-06-02: Refined host-budget demotion after a live proof run showed a
  transient host-budget signal could suppress the safest savings. Host-budget
  attention now demotes recoverable/heavier mechanisms such as chunk references
  but keeps cheap lossless/exact cache-hit reducers (`read_delta` and
  `repeated_output`) available. Repeated Layer-0 latency pressure still has its
  own stronger `latency_budget_full_context` gate. Regression coverage:
  `TestDecideCodexToolOutputHostBudgetKeepsLosslessReducers` and the updated
  `TestReduceCodexLayer0HostBudgetDemotesReducers`.
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
- 2026-06-02: Persisted the Layer-0 latency demotion bucket in a content-free
  runtime-budget state file. The bucket survives proxy restart for 30 minutes,
  caps strike debt at the demotion threshold, and still recovers automatically
  after cheap frames. This closes the offline autopilot gap where a daemon
  restart could immediately forget recent local-overhead pressure.
- 2026-05-31: Removed synchronous WSS tool-use/collapsed-key writes from the
  frame hot path. `toolusecache.MergeAsync` updates same-process memory
  immediately for reconnect hydration and flushes JSON state on a short
  write-behind delay; readcache already uses the same pattern. Tests prove no
  synchronous write, immediate cached `Load`, and later disk hydration.
- 2026-06-01: Readcache write-behind is revision-guarded, so an older delayed
  flush cannot replace newer in-memory state captured after the flush started.
  This closes a full-CI race where ranged-read cache entries could disappear
  before the async flush settled.
- 2026-05-31: Reduced TUI product-status overhead. The model now refreshes
  product status on ticks/events and renders from the cached snapshot; when
  host-budget attention is active, the next tick slows from 500 ms to 2 s.
- 2026-05-31: Added windowed local resource samples. The daemon now carries
  CPU-window percentage and disk read/write operation deltas between probes;
  `/admin/state.host_budget` reports those values and demotes managed reducers
  on CPU-window or disk-write-window budget spikes.
- 2026-06-01: Added deterministic micro-benchmarks for the T272 host-cost
  surface: Codex/WSS Layer-0 mutation, readcache full/ranged repeat decisions,
  FastCDC/chunk-store encoding, content-archive write/read, and planner
  decision overhead. `scripts/benchmarks` now runs these packages by default, so
  host-cost regressions are measured with the same benchmark ceremony as the
  older compression/filter hot paths. Live CLI/Desktop pprof remains operator
  capture work.
- 2026-06-01: Fixed the benchmark CLI's documented `-- -benchtime=...` path so
  short local runs do not silently fall back to the 3s default. Also removed an
  unnecessary archive expansion from unchanged archive-backed readcache hits:
  once the observed content hash matches the stored hash, the full gzip archive
  is not needed unless the content changed and a delta must be built.
- 2026-06-02: Wired host-budget proof into the operator evidence tools.
  `aggregate-savings` now prints and emits JSON for host-budget status, RSS,
  CPU window, disk write ops/delta, state bytes, compression/degradation health,
  and reasons. `workday-savings finish` carries the current host snapshot plus
  deltaed disk ops and adds explicit final host-budget notes. The release proof
  plan now names host-resource measurement and makes the host-resource budget a
  promotion gate.
- 2026-06-02: Automatic scoped CLI proof windows exercised the host-budget
  reporting path. The long search breadth window saved 45273 live WSS input
  tokens and ended with `workday-savings finish` host budget `ok` (RSS
  93618176 bytes, CPU window 0.92%, disk write delta 0, state 3471253 bytes),
  although an immediate post-finish aggregate sample briefly showed
  `cpu_window_budget_exceeded`; this confirms the demotion signal is sensitive
  to short CPU spikes and should be interpreted from the finish-window snapshot
  for release proof. The isolated git-status window saved 1518 live WSS input
  tokens and ended with host budget `ok` (RSS 86163456 bytes, CPU window 0.00%,
  disk write delta 0, state 3471943 bytes). Both windows had zero
  parse/degraded/compression errors.
- 2026-06-02: Profiled `BenchmarkReduceCodexLayer0_WSSRepeatedRead64KB` and
  found the hot cost was repeated exact o200k tokenization of identical large
  tool-output text, not readcache I/O. Added a bounded exact token-count cache
  keyed by encoding, byte length, and SHA-256 content hash. The benchmark moved
  from about 44.5 ms/op, 9.1 MB/op, and 87k allocs/op to about 2.2 ms/op,
  0.59 MB/op, and 882 allocs/op on the Apple M1 run, with no model-facing
  semantic change.
- 2026-06-02: Removed the unconditional double body copy in the WSS Phase-F
  input pipeline. The reducer now uses copy-on-write: it reads/parses the
  original frame body directly and only allocates a replacement body when a
  reducer, recovery note, or output-reduce injection actually changes the
  request. The changed flag still compares against the original bytes, so the
  model-facing semantics and fail-open behavior stay unchanged while no-op WSS
  frames avoid two full-body allocations.
- 2026-06-02: Removed a second unnecessary full request parse from the WSS
  shadow-mirror path for no-op frames. The mirror now reuses the messages that
  `applyInputPipeline` already extracted when the frame is unchanged, and only
  re-extracts the original request on real mutations where pre-pipeline and
  forwarded model-facing context can differ. This keeps T254 telemetry exact
  while avoiding duplicate JSON work on the common no-mutation path.
- 2026-06-02: Cached WSS request metadata inside the Phase-F hot path. Session
  id, previous-response id, and model are now resolved once per request stage
  and reused for tool-use hydration, re-read canary, recent-edit observation,
  reducer invocation, request summaries, and planner dry-runs. Existing wrapper
  functions stay for tests and non-hot callers, but the common WSS path avoids
  repeated full-map JSON unmarshalling of the same frame body.
- 2026-06-02: Removed the remaining eager WSS request-body copy in
  `wsRequestBody`. The Phase-F input pipeline now receives a read-only alias to
  the envelope body/request/raw bytes and only allocates when the replacer writes
  a mutated frame back into the envelope. Replacement still copies into
  envelope fields, so caller-owned mutation buffers cannot alias live frame
  state. This completes the copy-on-write direction for normal no-op WSS frames
  without changing model-facing semantics.
- 2026-06-02: Host-budget state is now part of proof-matrix live deltas from
  `codex-capture-run`: status, exceeded flag, reasons, RSS bytes, CPU window
  percent, disk-write delta, state bytes, compression_ok, and degradation_ok.
  `wss-proof-matrix` fails any new row that reports a non-ok host budget, and
  focused gates can explicitly require `host_budget_ok`. Existing legacy rows
  without host-budget fields remain readable, but new release/resource rows can
  no longer pass while local overhead is in attention/degraded state.
- 2026-06-02: `codex-capture-run` now also prints and persists Layer-0 policy
  and cache deltas. This closes the proof blind spot where a live capture could
  show replay savings but zero live savings without explaining whether the
  mechanism was blocked, full-passed, or simply missed. The first rerun exposed
  a real product guard decision: chunk-dedup full-passed under
  `host_budget_full_context` due to `cpu_window_budget_exceeded`, while
  lossless read/repeated reducers remained allowed under host pressure. This is
  the intended zero-drawdown behavior, but it means chunk-dedup default
  promotion still needs either a `host_budget_ok` live proof or a cheaper
  hotpath.
- 2026-06-02: Added a CPU-window minimum-sample guard. The daemon now reports
  `cpu_window_seconds`; `/admin/state.host_budget`, aggregate/workday reports,
  and `codex-capture-run` carry it through. `cpu_window_budget_exceeded` only
  demotes managed reducers after at least a one-second measured window. This
  preserves the product guard against sustained CPU pressure while eliminating
  the false startup/admin-poll demotion that blocked the first focused
  chunk-dedup live proof before the session workload had run.
- 2026-06-02: The rerun after the minimum-sample guard stayed under product
  host budget (`host_budget_status=ok`, RSS 25 MB, state 3.7 MB) and allowed
  chunk-dedup live. This proves the guard now distinguishes startup-poll
  measurement artifacts from real product resource pressure.
- 2026-06-02: The focused Desktop Codex.app chunk proof stayed under product
  host budget while chunk-dedup fired live. The proof matrix row
  `/Users/christopher/.slimference/captures/desktop-chunk-dedup-matrix.jsonl`
  reports `host_budget_status=ok`, RSS about 15 MB, `cpu_window_percent=0.32`
  over a 58.8 second window, disk write delta 0, state 3.7 MB,
  `compression_ok=true`, and `degradation_ok=true`. This closes the focused
  Desktop host-budget regression that previously made chunk-dedup look live-cold
  under a startup CPU artifact; broader workday/tool-heavy pprof/resource proof
  remains open.
- 2026-06-02: Hardened state-size accounting against partial undercounts. The
  bounded directory-size probe now reports whether it completed; the daemon
  treats an incomplete state-tree scan as host-budget pressure instead of
  accepting a partial byte total as healthy. This prevents too many tiny cache or
  state files from being treated as free just because the scan hit its bound.
- 2026-06-04: Re-ran the local host-cost benchmark surface after the live-corpus
  and tool-prune proof updates. Short 100 ms benchmark run on Apple M1:
  FastCDC chunking 256 KB about 135 us/op, chunk-store partial-overlap 64 KB
  about 509 us/op, content-archive write 64 KB about 2.49 ms/op, archive read
  about 131 us/op, planner Codex WSS large-tool decision about 259 ns/op, WSS
  repeated git-status about 808 us/op, WSS repeated read 64 KB about 849 us/op,
  readcache full-repeat 64 KB about 454 us/op, ranged-repeat 16 KB about
  165 us/op. These numbers keep the local hot path under the 25 ms product
  latency budget for the measured reducers; archive writes remain the expensive
  bounded operation and should stay batched/fail-open. At that point the live
  resource/profile proof was not yet closed; the final 2026-06-04 CLI/Desktop
  bundles below close that proof.
- 2026-06-04: Re-ran the same short local host-cost surface after the Layer-1
  corpus round-trip guard. Current Apple M1 numbers: FastCDC chunking 256 KB
  about 140 us/op, chunk-store partial-overlap 64 KB about 540 us/op,
  content-archive write 64 KB about 2.54 ms/op, archive read about 129 us/op,
  planner Codex WSS large-tool decision about 283 ns/op, WSS repeated
  git-status about 856 us/op, WSS repeated read 64 KB about 837 us/op, readcache
  full-repeat 64 KB about 463 us/op, ranged-repeat 16 KB about 170 us/op. This
  keeps the measured local reducer path under budget after the latest safety
  tests; live CLI/Desktop pprof/resource captures remain the only unclosed
  T272 proof class.
- 2026-06-04: Re-ran the full default benchmark surface after the Layer-2
  active-path/quality-pressure selector hardening and proof-tooling updates.
  Apple M1 numbers: FastCDC chunking 256 KB about 211 us/op, chunk-store
  partial-overlap 64 KB about 2.03 ms/op, content-archive write 64 KB about
  29.4 ms/op, archive read about 129 us/op, planner Codex WSS large-tool
  decision about 267 ns/op, WSS repeated git-status about 788 us/op, WSS
  repeated read 64 KB about 814 us/op, readcache full-repeat 64 KB about
  446 us/op, ranged-repeat 16 KB about 167 us/op. The long 3 s benchmark run
  confirms the reducer hot paths remain well under the 25 ms local budget;
  archive writes remain the bounded expensive recovery path and must continue
  to be batched, fail-open, and avoided for unrecoverable or low-ROI cases.
- 2026-06-04: Ran a focused race pass over the critical savings/safety packages:
  `go test -race ./internal/contextledger ./internal/filter ./internal/readcache
  ./internal/chunkdedup ./internal/toolprune ./internal/outputreduce
  ./internal/proxy/wsmitm ./internal/quality ./internal/hostmetrics -count=1`.
  It passed, covering the currently touched selector, reducer, cache, chunk,
  tool-prune, output-reduce, WSS, quality, and host-budget packages under the
  race detector. This is local concurrency evidence; it still does not replace
  the final live CLI/Desktop pprof/resource capture.
- 2026-06-04: Promoted the race evidence from focused packages to the full repo
  gate. The first `go test -race ./...` failed only because
  `TestRunRecertCommandHelperProcess` used a 1 s helper-process timeout that
  was too tight under race instrumentation; no product data race was reported.
  The test now uses a 10 s helper timeout, matching the heavier race-runtime
  cost without weakening production behavior. Verification passed:
  `go test -race ./cmd/slimference -run TestRunRecertCommandHelperProcess -count=1`,
  `go test -race ./cmd/slimference -count=1`, and finally
  `go test -race ./...`.
- 2026-06-03: Hardened missing-resource handling. If the daemon RSS probe is
  unavailable while WSS is otherwise active, `/admin/state.host_budget` now stays
  `unknown` instead of reporting `ok`; WSS parse/compression/degrade errors still
  escalate to `attention`. The TUI product panel renders `host budget unknown`
  rather than `safety ok`, so release proof cannot accidentally treat an
  unmeasured host as green.
- 2026-06-03: Consolidated WSS Phase-F request metadata parsing. The hot path now
  derives session id, previous-response id, and model from the raw request map
  already produced by `extractMessages`, and only the internal detailed pipeline
  returns that metadata to the adapter. Existing body-based helpers remain for
  tests and non-hot callers. This removes repeated full-map JSON unmarshalling
  from normal request handling without changing any model-facing bytes, reducer
  decisions, or planner semantics.
- 2026-06-03: Hardened release inventory semantics for host-resource proof. A
  `host_resource_long_workday` row must now show a positive live economic token
  signal plus `host_budget_ok`; host telemetry alone cannot close the maxx gate.
  Local billable-input deletion, provider-cache read tokens, and output-side
  evidence remain separately reported so the resource proof cannot blur what
  kind of savings made the row positive.
- 2026-06-03: Wired host-budget evidence into the live-corpus benchmark gate.
  Corpus session records can now carry either nested `host_budget` snapshots or
  flat `host_budget_*` fields, and `benchmark-corpus --maxx-check` can require
  `host_budget_ok` as a scenario validator. This gives the long-workday resource
  proof a second strict gate beside WSS proof-matrix inventory.
- 2026-06-04: Current proof inventory over `~/.slimference/captures` reports the
  full maxx workload matrix present and complete: chunk-similar, chunk-log,
  chunk-test, output-reduce-aggressive, tool-heavy, provider-cache-long-session,
  and host-resource-long-workday all have zero safety issues and host-budget-ok
  evidence where required. The inventory now contains 85 matrix rows, 23
  host-budget-ok rows, 57 positive economic-token rows, and complete maxx
  workload status. This closes the
  repeat/search/chunk/long-workday workload proof checkboxes for T272; the only
  remaining closeout is real CLI and Desktop pprof/resource capture, because
  host-budget snapshots prove product health but do not replace CPU/profile
  flamegraph evidence.
- 2026-06-04: Added `go run ./scripts/verify -mode host-resource-plan` as the
  content-free T272 live proof ceremony. It prints exact CLI/Desktop commands to
  capture aggregate-savings host snapshots, `ps` RSS/CPU rows, `workday-savings
  finish --json`, a macOS `sample` CPU profile, and a WSS proof row requiring
  `host_budget_ok`. This keeps the remaining pprof/resource work reproducible
  without opening a daemon pprof HTTP listener or adding a new runtime surface.
- 2026-06-04: Hardened focused `wss-proof-matrix` semantics for T272 and other
  single-mechanism closeouts. Focused runs now evaluate only requested workload
  classes and, when `--expected-reducer` is passed, use those explicit reducer
  expectations instead of stale row-local expectations. Unfocused release mode
  remains strict across every recorded row. The existing host-resource row now
  passes the hard focused gate with positive live billable input-token savings
  in that row, `host_budget_ok`, and replay `lost=0`.
- 2026-06-04: Found and fixed a real WSS host-resource regression during an
  autonomous CLI sanity run. A no-savings WSS/Codex request loaded heavy exact
  o200k token accounting in two non-claim paths: Layer-0 counted every
  tool-result block before any mutation existed, and planner/output-reduce
  telemetry counted prompt bodies exactly before any product saving was known.
  Layer-0 now lazy-counts exact Codex tokens only after a candidate mutation
  exists and still uses exact before/after counts for every real token-savings
  claim. WSS planner no-op rows and output-reduce low-ROI gating use cheap
  byte/4 estimates because those paths do not claim local billable savings.
  Focused tests pin the estimate-only planner path and the existing
  output-reduce behavior. A follow-up scoped CLI sanity capture
  `/Users/christopher/.slimference/captures/host-resource-sanity-20260604T201638Z`
  stayed `host_budget_status=ok` with RSS 77,414,400 bytes, replay `lost=0`,
  and zero parse/degrade/compression errors. It is intentionally not counted as
  a savings proof because the prompt produced no positive token savings.
- 2026-06-04: Hardened proof evidence retention. `codex-capture-run` now writes
  the matrix row before failing an expected-reducer gate, so missing reducer
  hits and host-budget attention remain reviewable evidence. The exit code
  still fails closed. Added a regression test proving the negative host-budget
  row is persisted before returning code 3.
- 2026-06-04: Corrected the T272 runbook endpoint from `/admin/state` to the
  actual scoped admin endpoint `/_slimference/admin/state`. The old path could
  return a proxied 404 and invalidate otherwise correct resource proof steps.
- 2026-06-04: Hardened the final release resource gate. `release-proof-report`
  now rejects a loose profile text file and requires both CLI and Desktop proof
  bundle directories
  with `admin-before.json`, `admin-after.json`, `ps-before.txt`,
  `ps-after.txt`, `workday-finish.json`, `slimference.sample.txt`, and
  `matrix.jsonl`.
  Both aggregate-savings snapshots and the workday current/delta host-budget JSON must be
  `ok`, compression/degradation-safe, RSS-measured, CPU-window-measured, and
  the workday WSS parse/degrade/compression deltas must stay zero. Each local
  `matrix.jsonl` must contain a positive `host_resource_long_workday` row with
  `host_budget_ok` for the matching client. The `host-resource-plan` runbook
  now prints the final two-bundle `release-proof-report` shape, so the live
  proof cannot accidentally pass on a placeholder file or on only one product
  surface.
- 2026-06-04: Removed the remaining manual CLI-resource ceremony. `codex-capture-run`
  now accepts `--resource-profile-proof <bundle-dir>` and, while it owns the
  managed daemon, writes the release bundle files itself: `frames.jsonl`,
  `matrix.jsonl`, aggregate admin snapshots before/after, `ps` before/after,
  macOS `sample`, and `workday-finish.json`. `host-resource-plan -client
  codex_cli` now prints this single automated command. Desktop remains manual
  only because the App prompts are operator-driven, not because the verifier is
  loose.
- 2026-06-04: Closed the final live resource/profile proof. The validated CLI
  bundle is
  `/Users/christopher/.slimference/captures/host-resource-codex_cli-auto-20260604T212018Z`;
  the validated Desktop bundle is
  `/Users/christopher/.slimference/captures/host-resource-codex_desktop-20260604T212111Z`.
  The resource proof validator passes both bundles (`resource_profile_proof_ok`
  with clients `cli` and `desktop`). After the 2026-06-05 honesty hardening,
  `release-proof-report ~/.slimference/captures --resource-profile-proof <cli>
  --resource-profile-proof <desktop> --json` intentionally fails over the
  historical capture archive because it still includes old diagnostic rows.
  Release claims must therefore run `wss-proof-clean-matrix
  ~/.slimference/captures <clean-release-matrix.jsonl> --json` first, then run
  `release-proof-report <clean-release-matrix.jsonl> --resource-profile-proof
  <cli> --resource-profile-proof <desktop> --json`.
- 2026-06-04: Hardened replay semantics for output-reduce instruction injection.
  `wss-ab-replay --fail-on-lost` now treats a known output-reduce marker suffix
  in top-level Codex `instructions` as an expected instruction extra while still
  reporting it. The Desktop resource capture therefore passes with `lost=1`,
  `expected_extras=1`, and `gate_passed=true`; unknown instruction rewrites
  still fail as lost context.

## Done

The host budget is maxxed when savings mechanisms are not just correct but
cheap, bounded, observable, and automatically demoted before they hurt the
operator's machine or Codex workflow. Lossless exact cache-hit reducers remain
available under host-budget attention because they are the safest high-value
savings path; heavier recoverable mechanisms full-pass until the host is healthy.
The final CLI and Desktop resource/profile bundles are now validated by the
release proof report, so T272 is closed for the current maxx/release scope.
