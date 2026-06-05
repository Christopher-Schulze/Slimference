# Changelog

## Unreleased

### 2026-05-16 - Phase G round 7: wsmitm.Session + wscompact.WriteFrame

The final wire piece that closes Phase G's compression hot-path:

- **wscompact.WriteFrame** (new in `internal/wscompact/write.go`): RFC
  6455 frame encoder supporting all opcodes (continuation, text, binary,
  close, ping, pong), masked + unmasked emission, every length
  encoding (7-bit / 16-bit ext / 64-bit ext). Mask key length is
  validated; caller's payload buffer is never mutated. 97.4% coverage.

- **wsmitm.Session** (new in `internal/proxy/wsmitm/session.go`):
  bidirectional WebSocket bridge with per-direction FrameHandler hooks.
  Reads RFC 6455 frames via wscompact.ReadFrame, parses JSON text
  payloads via wsmitm.Parse, invokes the handler (which can mutate the
  Envelope and signal `replace=true` to trigger re-encoding), then
  emits the result via wscompact.WriteFrame. Non-text frames
  (ping/pong/binary/close) and empty payloads pass through byte-equal.
  On JSON parse failure the session latches into **degraded mode**: all
  subsequent frames in both directions are forwarded byte-equal without
  inspection, so a schema change on either side cannot block live
  traffic. Atomic per-session counters surface to operator telemetry
  (c2s/s2c frames + bytes, parse failures, reencoded vs forwarded
  count, degraded flag). 96.3% coverage.

- Session.Serve has a documented "first-direction-wins" semantic: it
  returns when the first pump goroutine ends; the caller is responsible
  for closing both Client and Upstream to release the remaining
  goroutine. Production wrappers `defer` the close. Tests close the
  in-memory pipes explicitly.

This connects every piece we built today: TCP-Accept →
transparent.Engine (SNI peek, TLS terminate) → sniroute.Resolver
(decide MITM vs passthrough) → wsmitm.Session (bridge with Phase F
hooks) → re-encoded frame to upstream.

### 2026-05-16 - Phase G round 6: transparent listener engine

- **internal/proxy/transparent/Engine** anchors the Phase G data path.
  Accepts TCP connections from a caller-provided net.Listener, peeks
  the TLS ClientHello to extract SNI + ALPN (RFC 5246 / 6066 parser
  inline so we never need to complete the handshake for off-domain
  traffic), performs TLS termination with our CA-signed leaf via
  injectable LeafCertProvider, runs the request through the SNI
  router, dispatches to either an MITM handler or a passthrough
  bridge.

- Hardened: bounded concurrency via semaphore (default 256, tunable),
  handshake timeout (default 5s, covers both peek + TLS), fail-open
  on every error path, atomic per-engine telemetry counters
  (accepted/served/mitm/passthrough/rejected/errors), context-driven
  shutdown that waits for in-flight handshakes to finish.

- Resolver elevated to an interface so tests can stub the rare
  Reject path; production satisfies it with the existing
  sniroute.Resolver.

- 90.5% coverage; race-clean under -race; remaining gap is deeply
  defensive TLS-parser overflow branches that valid handshakes never
  hit.

### 2026-05-16 - Phase G rounds 4-5: WSS frame parser + indistinguishability harness

Round 4: tshark already installed (v4.6.5). Two more autonomous packages
landed:

- **T188 WSS frame parser** under `internal/proxy/wsmitm/`. Schema
  derived from openai/codex Rust source (codex-api/src/sse/responses.rs
  + codex-api/src/endpoint/responses_websocket.rs) read 2026-05-16.
  Recognises 18 frame kinds (request, ping, response.created,
  response.output_text.delta, response.completed/incomplete/failed,
  reasoning deltas, function-call-args deltas, codex.rate_limits,
  error, pong, ...). Fail-open: unknown frame kinds map to
  FrameKindUnknown so callers downgrade the session to pure
  passthrough rather than blocking. Envelope.Raw preserves the original
  bytes so byte-equal forwarding is possible when no Phase F mutation
  fires. 100% coverage; race-clean.

- **T190 indistinguishability harness** under `internal/indist/`. The
  diff engine + Capture data model that the operator-side tshark
  wrapper will populate. Captures TLS ClientHello (JA3/JA4, ALPN,
  cipher/extension/curve ordering, GREASE), HTTP/2 SETTINGS frame,
  HTTP/2 pseudo-header order, plus WS Upgrade headers (extensions,
  subprotocol, version). `Diff(baseline, proxy)` returns a Report
  whose `OK()` is true iff every fingerprintable field matches.
  Fingerprint is a sha256 hash of every load-bearing field,
  deterministic across runs (timing fields explicitly excluded). 100%
  coverage; race-clean.

Pending: a real tshark-driven capture of Codex 0.130 traffic on a
fresh Mac to lock the golden file under `research/indist/codex-0.130/`.
The harness is ready to receive captures the moment an operator runs
it.

### 2026-05-16 - Phase G round 3: launchd + file-backup + concrete probes

Two more rounds landed on top of round 2:

**Round 4 — concrete reversibility Steps:**
- `LaunchdInstall` under `internal/control/reversibility/steps/`. Renders
  the user-scope LaunchAgent plist, runs `launchctl load -w` via a
  swappable command path (tests inject a no-op stub). Apply cleans up
  the plist on launchctl failure so re-runs aren't blocked. Reverse
  runs `launchctl unload` (best-effort) + removes the plist. Idempotent
  Inspect against the plist file's presence. ~94% coverage; race-clean.
- `FileBackup` - generic Apply-patch / Reverse-restore step for any
  config file (`~/.codex/config.toml`, `~/.codex/hooks.json`,
  `~/.claude/settings.json` etc.). Pre-patch snapshot lands in
  `<BackupDir>/<basename>.slimference.<unix-ns>.bak`; second Apply
  preserves the OLDEST snapshot (the genuinely-pristine pre-install
  state). Reverse restores byte-equal. Patch function is caller-supplied
  + must be idempotent. ~93% coverage; race-clean.

**Round 5 — concrete probes for state aggregator:**
- `FileCAProbe` + `KeychainCAProbe` under `internal/control/probes.go`.
  FileCAProbe reads `<Dir>/ca/root.crt`, parses, computes SHA-256
  fingerprint, reports NotBefore/NotAfter/DaysUntilExpiry. KeychainCAProbe
  composes with a caller-supplied `Looker` function so the live keychain
  call (`security find-certificate`) is swappable in tests.
- `HTTPDaemonProbe` - hits `/admin/health` with 500ms timeout, reports
  PID + version + running/healthy.
- `PortListenerProbe` - dial-checks 443/8990 via swappable Dial. Default
  dialer uses net.Dialer with 50ms timeout. Inlined itoa() to avoid the
  strconv import for this single use site.
- `HostsFileNetworkProbe` - reads /etc/hosts (or override), reports
  which Slimference targets currently resolve to a loopback address.
- `AppsManagerProbe` - wraps internal/control/apps.Manager + optional
  AppCounters to emit one AppEntry per known app with Enabled / Detected
  / Routed / Bypassed fields.
- `MemoryAppCounters` - atomic in-memory implementation of AppCounters;
  the proxy will plug it into the SNI router so per-app routing decisions
  feed straight into the TUI dashboard.

Full Round 5 coverage: 100% of the internal/control package.

Total Phase G round 3 contribution: ~1500 LOC code + ~2300 LOC tests
across 6 new files. Full test suite green; gofmt clean; vet clean;
race-clean.

### 2026-05-16 - Phase G round 2: concrete Steps + state aggregator + inventory

Building on the foundation packages, three more deliverables landed today:

- **T196 concrete steps** under `internal/control/reversibility/steps/`:
  - `CAGenerate` - materialises (or re-uses) the local Slimference Root CA
    under `<Dir>/ca/`. Apply is idempotent; Reverse moves the files aside
    with a timestamped `.bak.<unix>` suffix (preserves operator's ability
    to recover the previously-trusted fingerprint). Inspect reports
    present/partial/absent.
  - `HostsPatch` - modifies `/etc/hosts` with a fenced Slimference-managed
    block. Atomic writes via tmp-rename. Pre-install backup at
    `<path>.slimference.bak`. Reverse restores from backup (byte-equal)
    or falls back to stripping the fenced block. Marker-fenced so manual
    edits outside the fence survive.
  - 95%+ statement coverage; race-clean; gofmt + vet clean.

- **T191 control state aggregator** under `internal/control/`:
  - `SetupState` JSON-shaped snapshot with seven sub-blocks (CA, daemon,
    listener, network redirect, indist proof, per-app, savings).
  - `Probes` interface struct - one method per state field so callers
    swap probes independently.
  - `Build(ctx, Probes)` runs all probes in parallel goroutines under
    a 100 ms budget; nil probes leave the field at zero (renderer shows
    "unknown").
  - `IsHealthy()` rolls up to a single bool (CA + daemon + listener +
    network redirect must all be present for healthy).
  - 100% coverage; race-clean.

- **T194 inventory codification** under `internal/proxy/sniroute/`:
  - `CodexEndpointInventory` - 27 entries covering every Codex Desktop
    App / OpenAI / Anthropic endpoint family we have observed, each
    tagged with its purpose, expected routing decision, and the Codex
    version we first saw it in.
  - `LookupEndpoint(host, path)` - returns the matching entry (most-
    specific first).
  - `VerifyDecision(host, path, decision)` - runtime safety guard that
    returns false + a reason when the live router disagrees with the
    inventory. Wires into `internal/proxy/wsmitm` (when built) to log
    structured warnings on routing drift.
  - Inventory ↔ router consistency test ensures the static inventory
    cannot drift from the live routing table.
  - 100% coverage; race-clean.

Full test suite green; gofmt clean; vet clean. Total new code in this
round: ~1 200 LOC + ~1 800 LOC tests across 6 files.

### 2026-05-16 - Phase G foundations: T193 + T189 + T196

Three load-bearing foundation packages for the Phase G live-conversation
interception path, built ahead of the wire-level work (T188 / T190).

- **T193** `internal/control/apps/` — per-app activation state machine.
  `AppID` enum, `Policy` schema, `Manager` with atomic snapshot + OnChange
  listeners, TOML loader/writer at `~/.config/slimference/apps.toml`,
  UA-prefix and on-disk binary detection cascade. Defaults: Codex CLI on,
  Codex Desktop App on, Claude Code off. 98.4% coverage, race-clean.
- **T189** `internal/proxy/sniroute/` — routing-table evaluator. Decides
  MITM vs passthrough per request based on SNI, path, method, WebSocket
  subprotocol, and per-app policy. Routes Codex conversation traffic
  (HTTP POST or WSS upgrade with `responses_websockets` subprotocol) to
  MITM; routes all Codex Desktop sideband traffic (realtime/voice,
  computer-use, image-gen, plugins, memories, models, analytics, browser
  web UI) to passthrough. 100% coverage, race-clean.
- **T196** `internal/control/reversibility/` — atomic install/uninstall
  framework. `Step` interface, `Plan` with LIFO rollback on partial
  failure, `Inspect` reports per-step + overall state. 100% coverage,
  race-clean.

These three packages do not yet wire into the live `:443` listener -
that is the next iteration (T188 wire + transparent listener bind).
They provide the gates and bookkeeping that every later piece consumes.

### 2026-05-16 - Codex Desktop App routing audit + path tightening

After reading the openai/codex source on GitHub, discovered our prior
`/backend-api/codex/*` prefix match was over-broad. The Codex Desktop
App (2026) emits multiple endpoints under that prefix that are NOT
chat-completion conversation traffic and must not be intercepted:

- `/backend-api/codex/realtime/calls` (voice / realtime upgrade)
- `/backend-api/codex/realtime/calls/<id>` (call management)
- `/backend-api/codex/models` (GET model listings)
- `/backend-api/codex/memories/trace_summarize` (memory subsystem)
- `/backend-api/codex/responses/compact` (Codex's own conversation
  compaction sideband - already-compressed input, don't double-compress)
- `/backend-api/codex/plugins`, `/backend-api/codex/plugins/install`
- `/backend-api/codex/images/generations` (image generation via
  gpt-image-1.5)

`isCompressiblePath` now matches only the exact
`/backend-api/codex/responses` endpoint (and trailing-slash variant)
for Codex traffic. `isProviderCompressiblePath` mirrors the
tightening. All other Desktop-App sideband endpoints pass through
byte-equal via `handlePassthrough`.

Verified with six integration tests in
`internal/proxy/codex_desktop_safety_test.go`:

- Vision (`input_image` content parts) flows byte-equal through the
  Responses-API conversation endpoint; image URL and `detail` field
  survive the parse / re-marshal round-trip.
- `web_search` tool calls (`function_call` / `function_call_output`
  with name=`web_search`) pass through untouched - our staleread /
  prune mechanisms allowlist only `Read` and the apply_patch family,
  so Codex's tool conventions never trigger them.
- `computer_call` / `computer_call_output` items (screenshot, click,
  type) flow untouched - screenshot base64 payloads in the
  `computer_call_output` shape survive round-trip.
- Realtime voice call setup (`POST /backend-api/codex/realtime/calls`)
  is no longer intercepted; body reaches upstream byte-equal.
- Image generation (`POST /backend-api/codex/images/generations`)
  passes through.
- Models listing (`GET /backend-api/codex/models`) passes through
  unmodified.

### 2026-05-16 - Option C: T186 Quality A/B Harness + T169 Be-Terse Hint

- New `internal/qualityab/` package: lightweight Quality A/B harness
  for gated output-reduce levers. Deterministic FNV-64 cohort routing
  (control vs treatment), per-cohort atomic outcome counters
  (success / upstream error / retry), one-way auto-rollback latch
  when treatment failure rate exceeds control's by 5pp on 50+ samples.
  100% coverage, race-detector-clean.
- New `internal/beterse/` package: deterministic injector for the
  curated be-terse hint ("Reply concisely. No preambles, no closing
  remarks. Show your work directly."). Supports both Anthropic
  (`system` string or array of content blocks) and OpenAI / Codex
  (`messages` with role=system) wire shapes. Idempotent; 100% coverage.
- Wired into `handler.go` step 8.8: when `BeTerseHintEnabled` is on,
  the per-session cohort decides; treatment cohort gets the hint,
  control sees the original body. Outcome (HTTP status >=400 → failure,
  otherwise success) is reported to the harness on every response so
  auto-rollback can fire.
- New toggles `[compression.output_reduce] be_terse_hint_enabled`
  (default **off** - this lever has Quality risk) and
  `be_terse_hint_text` for custom wording, plus env-var equivalents
  (`SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT`,
  `SLIMFERENCE_OUTPUT_REDUCE_TERSE_HINT_TEXT`).
- Telemetry: `/admin/status.output_reduce_counters.beterse_*` for
  injection counters; `/admin/status.quality_ab` for cohort state +
  rollback flag.
- E2E wire tests assert treatment cohort gets the hint, control
  doesn't, and the master toggle short-circuits both.

### 2026-05-16 - Option B: T170 / T174 (Input-side reclamation)

- New `internal/filetracker/` package: per-session FileMutationTracker
  with `RecordRead` / `RecordMutation` / `Get` / `All` / `Forget`.
  Content-hashed via sha256, race-detector-clean, 100% coverage.
  Shared substrate for T170, T174, and future T177 PostToolUse JIT.
- New `internal/staleread/` package with two engines, both 100%
  covered:
  - `AgeMessages` (T170): replaces superseded older `Read` tool_results
    with `[stale read: <path> superseded by turn N]` markers when a
    newer read of the same path exists. Lossless aging.
  - `PruneObsoleteReads` (T174): replaces reads that happened before a
    subsequent `apply_patch` / `Write` / `Edit` of the same path with
    `[obsolete: <path> edited at turn N]` markers. The model retains
    the post-mutation state via later messages.
- Wired into `handler.go` steps 2.5 (aging) and 2.6 (obsolete-prune),
  before secret detection and compression layers so L1/L2 see the
  reduced messages.
- Two new toggles under `[compression.output_reduce]`
  (`stale_read_aging_enabled`, `obsolete_read_prune_enabled`) plus
  `stale_read_aging_min_turn_gap` and env-var equivalents
  (`SLIMFERENCE_INPUT_REDUCE_STALE_AGING`,
  `SLIMFERENCE_INPUT_REDUCE_STALE_AGING_MIN_TURN_GAP`,
  `SLIMFERENCE_INPUT_REDUCE_OBSOLETE_PRUNE`).
- Telemetry: `output_reduce_counters.stale_read_*` and
  `output_reduce_counters.obsolete_read_*` in /admin/status.
- E2E wire tests show real reductions on synthetic sessions
  (~186 bytes saved by aging, ~239 bytes by pruning).

### 2026-05-16 - Option A: T183 / T184 / T185 (Output-Reduction Sprint follow-ups)

- **T183**: OpenAI / Codex non-streaming responses now go through repdet
  rewriting too. Added `passthroughOpenAIWithRepdet` + `rewriteOpenAIResponseBody`
  supporting both Chat Completions (`choices[].message.content`) and
  Responses API (`output[].content[].text`) shapes. Handler dispatch
  splits Anthropic / OpenAI / Codex into per-provider helpers.
- **T184**: Streamcut now uses a 3-line holdback queue
  (`NewCutterWithHoldback`) so the trailing-commentary opener never
  reaches the client. Substantive deltas older than the holdback flow
  through normally; on natural stream end `Flush` drains the queue.
  Refactored `streamingRelayWithCutter` around the new `Forward` /
  `Flush` API.
- **T185**: New `OutputReduceCounters` struct on `Proxy` with atomic
  monotonic counters for stop-seq injections, streamcut fires, repdet
  rewrites and bytes saved. Surfaced under
  `/admin/status.output_reduce_counters` with a stable JSON shape.
  Race-detector clean under concurrent load.
- All three packages stay at 100% unit-test coverage; new wire tests
  cover OpenAI repdet rewrite, streamcut delay-buffer suppression
  (opener no longer in client view, substantive content preserved),
  and admin telemetry round-trip.

### 2026-05-16 - T165 / T166 / T167 Output-Reduction Sprint

- Added `internal/outstop/` with a versioned trailing-commentary phrase
  registry and `MergeIntoBody` that injects `stop_sequences` (Anthropic) /
  `stop` (OpenAI, Codex ChatGPT) capped at four entries, preserving any
  user-supplied list. Idempotent; user-supplied stop entries kept first.
- Added `internal/outstop/streamcut/` cutter that watches SSE deltas and,
  once the response carries >=80 chars of substantive content, closes the
  upstream HTTP body and emits a synthetic provider-shaped terminator the
  first time a phrase-library opener appears in the most recent 96 bytes.
  Stops upstream generation - we no longer pay for the trailing fluff.
- Added `internal/outstop/repdet/` Rabin-Karp repetition detector (100-byte
  windows, 200-byte confirmed-match floor) plus per-request prompt indexer
  and `passthroughAnthropicWithRepdet` that rewrites verbatim echoes in
  non-streaming Anthropic responses into `[unchanged: <name>]` markers.
  Streaming and OpenAI / Codex rewrite paths are follow-ups.
- Three new operator toggles under `[compression.output_reduce]`
  (`stop_sequences_enabled`, `streamcut_enabled`,
  `repetition_detection_enabled`) and env-var equivalents
  (`SLIMFERENCE_OUTPUT_REDUCE_STOP_SEQS`,
  `SLIMFERENCE_OUTPUT_REDUCE_STREAMCUT`,
  `SLIMFERENCE_OUTPUT_REDUCE_REPDET`); all default `true`.
- 100% statement coverage on all three new packages; end-to-end wire test
  asserts `stop_sequences` reaches the upstream Anthropic stub.

### 2026-05-15 - T154 Read/File Delta Maximizer

- Extended read-cache decisions from hook-only reads into proxy-visible
  file-read history with session-scoped content hashes and archive URIs.
- Unchanged rereads now collapse to stable expandable references; changed
  rereads emit concise textual deltas only when shorter than the full content.
- Added recent-edit safety bypass through hook turn-state and tests for
  unchanged rereads, changed rereads, archive expansion, and recent-edit
  fail-open behavior.
- Tightened Layer 2 huge-input handling so summariser input is capped before
  expensive preprocessing/density scoring and target sizing uses submitted text.

### 2026-05-15 - T153 Hierarchical Context Capsules

- Added deterministic `ContextCapsule` schema and builders for micro, phase,
  and session capsules with token accounting, source ranges, validation state,
  archive URIs, and tier selectors.
- Micro capsules now archive large non-anchor tool results through
  `contentarchive`; phase/session capsules skip anchor-bearing ranges so edits,
  failures, decisions, and blockers stay verbatim.
- Reused existing `slimference expand` content-archive retrieval for capsule
  expansion and added tests proving source recovery.

### 2026-05-15 - T152 Async L2 Background Summary Pipeline

- Added Layer 2 candidate scoring so background MiniMax jobs queue only when
  provider, prefix size, recent-anchor safety, projected savings, and cache
  coverage gates say the next request can benefit.
- Added per-session candidate hashes to `CompressJob`; workers now drop stale
  jobs before calling MiniMax and record `stale_job_skips`.
- Made cached summary application prefix-hash aware and exposed Layer 2 cache
  stats in `/admin/status.layer2.cache_stats`.

### 2026-05-15 - T151 L3 Tool-Schema Pruning Maximizer

- Added conservative always-keep classes and `tool_prune_always_keep` so
  shell/edit/read/safety/browser/MCP tools stay attached while cold custom
  tool schemas can be pruned.
- Added one-shot full-schema retry for missing-tool 4xx responses and disabled
  future pruning for that session bucket after a miss.
- Surfaced tool-prune saved-token, pruned-tool, reattach, miss, retry,
  always-keep, and disabled-session telemetry in admin/debug/flight/gain
  surfaces.

### 2026-05-15 - T150 L3 Stable-Prefix Cache Planner

- Added stable-prefix planning for OpenAI prompt-cache hints: keys now rotate
  on stable prefix/tool-schema changes while ignoring latest user-turn churn.
- Gated hint injection on stable-prefix tokens instead of whole-request tokens,
  so one-turn prompts do not pay cache-hint overhead.
- Added content-free prompt-cache hint telemetry to debug/flight records and
  kept provider-reported cache usage as the only billable-savings source.

### 2026-04-19 - TUI Operator Console Polish

- Reworked the BubbleTea dashboard from a hotkey-led status board into a more
  explicit operator console with a selectable `CONTROL SURFACE`, `FLOW`,
  `TRAFFIC`, `PROVIDER MAP`, and action-detail cards.
- Made arrow-key plus `Enter` navigation the primary interaction model across
  Dashboard, Stats, Debug, and Setup. Compatibility shortcuts remain, but the
  visible UI no longer depends on letter mnemonics for normal operation.
- Moved daemon lifecycle and auto-start management into the Dashboard action
  surface, leaving Setup as an installation/status view instead of a mixed
  control panel.
- Refreshed the Lipgloss styling to a darker operator-console palette with
  cyan focus, stronger cards, clearer metrics, and less legacy hotkey visual
  noise.
- Added deterministic TUI coverage for the new action model, render helpers,
  compatibility branches, and operator-state views; `internal/tui` now sits at
  `100.0%` coverage and the full proof stack remains green.

### 2026-04-19 - Final Coverage Closure and Gate Proof

- Closed the remaining `internal/hooks`, `internal/proxy`, and `internal/tui`
  edge-path gaps so `go run ./scripts/ci` now passes again at a real
  `100.0%` Go coverage gate instead of stopping at `99.9%`.
- Added final deterministic regression coverage for hook-install write
  failures, hook coherence checks, checkpoint/read-cache flush failure paths,
  prompt-cache dashboard rendering, and setup-wizard navigation edge cases.
- Fixed `internal/analytics.WritePromptCacheCSV` so CSV flush errors are no
  longer silently lost behind a deferred writer flush.
- Tightened TUI setup-step semantics: when no service-control adapter is wired,
  the setup wizard now exposes no executable steps instead of manufacturing a
  pseudo-selection over unavailable actions.

### 2026-04-19 - Daemon Monitor and Repo-Analysis Closure Sync

- Synced the canonical documentation to the actual daemon-plus-monitor runtime:
  `docs/documentation.md` now describes `slimference start`, the attach-mode
  TUI, Setup view navigation, daemon/service controls, `daemon logs`,
  `stats prompt-cache`, and the hidden `readhook` path.
- Synced `docs/map.md` to the real code layout by adding the attach adapter
  (`cmd/slimference/remote_proxy.go`), daemon admin surface
  (`internal/proxy/admin.go`), prompt-cache reporting
  (`internal/analytics/prompt_cache.go`), and persisted read-cache state.
- Recorded the 2026-04-19 closure pass in `docs/context.md` so the prompt-cache,
  read-cache, hook drift, and daemon-monitor workstream now has one continuous
  evidence trail.

### 2026-04-19 - Continuity Checkpoints and Tool Archive

- Added `internal/checkpoints` as a deterministic continuity layer that saves
  ranked checkpoint artifacts under `~/.slimference/checkpoints/` without
  coupling the capture path into the request hot path.
- Added `slimference checkpoint capture|list|restore|stats` so checkpoint
  creation and restore are explicit operator tools.
- Added `internal/toolarchive` and `slimference expand <id>` for local recovery
  of archived large tool results via `slim://archive/<id>` references.
- Extended `slimference posttool` so large outputs with real hook metadata are
  archived with a bounded preview, while the previous compaction-only behavior
  remains the fallback path.
- Extended daemon admin / TUI status with checkpoint and tool-archive activity
  so continuity and archive health remain visible in attach mode.

## v2.0.2 - 2026-04-17

### Production Readiness Remediation Complete

- Closed the full remediation program opened by `docs/audit-1.md` and
  `docs/gap-analysis.md`.
- Fixed the proxy hot-path zero-downside ordering bug so negative-savings
  requests are reverted before the forwarded body is built.
- Replaced the old Layer 3 text-only cache identity with provider-aware
  canonical full-request hashing plus dependency-path invalidation.
- Reworked Claude Code hooks to emit structured `hookSpecificOutput` and to
  merge/remove `settings.json` entries without destroying unrelated hooks.
- Reworked Codex integration around `hooks.json` PreToolUse/PostToolUse hooks
  plus the dedicated `slimference posttool` output-compaction path. `hook verify`
  now fails on broken Codex installs.
- Tightened Layer 2 by propagating cancellation through production call paths,
  enabling strict summary mode by default, and validating against structured
  message content instead of markdown accidents.
- Hardened the daemon/service path: launchd now sources a `0600` env file,
  never embeds `MINIMAX_API_KEY` in the plist, and install/remove performs real
  `launchctl` lifecycle steps.
- Repaired the release proof stack: `scripts/ci` now enforces the intended
  coverage threshold and the repository now reaches `100.0%` Go coverage across
  `cmd/` and `internal/`.
- Centralized the binary/TUI/health version string in `internal/buildinfo` so
  `slimference version`, the TUI header, and `/health` all report the same
  current release value.
- Hardened Layer 3 further by keying cache entries on the effective forwarded
  request plus normalized cache-relevant headers, skipping explicitly
  stochastic requests, and recording cache hits as normal processed requests in
  analytics/debug output.
- Tightened file-dependent Layer 3 admission again: responses that reference
  dependency paths are now cached only when the file watcher is available and
  the dependency watch is actually armed. Missing watcher capacity or watch
  errors now force a safe cache miss instead of a stale-hit risk.
- Hardened shutdown reliability further: `analyticsWorker` now drains queued
  events before exit, and `internal/caching.FileWatcher.Close()` is idempotent.
- Fixed an analytics collector shutdown bug: draining a closed event channel can
  no longer loop forever or emit zero-value phantom events.
- Fixed analytics math so per-provider latency averages are computed from each
  provider's own request count, not the global mixed-provider total.
- Corrected `AnalyticsSnapshot.CompressionRatio` to report the saved-token
  fraction and made session-log field rendering deterministic for stable
  exports and diffs.
- Tightened hook status detection so the TUI prefers coherent Claude/Codex
  installs over loose file presence and only uses the legacy Codex AGENTS
  marker as a fallback signal.
- Bounded TUI quit shutdown with a timeout context so `q` / `Ctrl+C` cannot
  hang forever if an underlying shutdown path stalls.
- Fixed Codex uninstall config cleanup so `hook remove codex` removes only
  Slimference-managed lines, preserves unrelated user `codex_hooks` and other
  `[features]` entries, and still cleans up the legacy single-flag section.
- Hardened Codex install/verify against silent config conflicts: conflicting
  `openai_base_url` or `codex_hooks = false` now fail fast instead of reporting
  a broken install as healthy.
- Fixed proxy request-body handling so client bodies above 32 MiB now return
  HTTP 413 instead of being silently truncated before forwarding.
- Fixed non-streaming passthrough so upstream bodies above 10 MiB now fail with
  a local 502 instead of partial replay, and local passthrough failures no
  longer leak copied upstream success headers.
- Tightened SSE relay write handling so a failed newline write stops streaming
  immediately instead of continuing after a broken frame boundary.
- Tightened decision-log readers (`ReplaySession`, `readLastDecisionSummaries`)
  so malformed JSON is skipped, pseudo-summaries without `req_id` are ignored,
  and scanner failures do not surface partial results.
- Fixed `slimference test intercept` so listener bind failures now abort
  immediately instead of waiting out the full timeout on a dead server.
- Hardened debug decision-log flushing so marshal/write failures log warnings
  and do not emit corrupt placeholder lines into `decisions.jsonl`.
- Fixed OpenAI request reconstruction so structured `content` arrays remain
  structured on roundtrip and are no longer degraded into stringified JSON.
- Raised the SSE relay line cap from 1 MiB to 8 MiB so large tool-output
  events are less likely to trip local scanner overflow.
- Tightened TUI debug-log exports to user-only permissions (`0700` export
  directory, `0600` exported files).
- Made recursive hook JSON key lookup deterministic so nested `command` /
  `tool_response` extraction no longer depends on Go map iteration order.
- Hardened Codex install preflight again: malformed existing `~/.codex/hooks.json`
  now aborts install before any Slimference scripts or config are written.
- Hardened MiniMax request lifecycle: summarization HTTP calls now use the
  caller context directly, retry backoff is cancelable, and canceled requests
  stop before any further retry/fallback work is attempted.
- Hardened Layer 2 cancellation semantics: canceled jobs no longer write fresh
  summaries into `SummaryCache`, and fallback-provider traversal stops as soon
  as the parent context is canceled.
- Hardened proxy shutdown further: background compression now runs under a
  proxy-owned worker context, in-flight summarization is canceled on shutdown,
  and queued compression jobs are skipped once shutdown begins.
- Tightened the offline savings toolchain in `scripts/utils`: real
  `session-report`, `decision-report`, `filter-report`, and `combined-report`
  outputs now exist with text, JSON, and CSV formats.
- Raised JSONL scan limits in analytics/debug/reporting readers from the old
  1-2 MiB defaults to 8 MiB so large decision/session lines fail less often in
  production logs.
- Synced `docs/documentation.md`, `docs/map.md`, `docs/context.md`, and the
  legacy T01-T10 todo artifacts to the current implementation state so current
  docs no longer describe stale hook/version/reporting behavior.
- Added the fresh-eyes follow-up review in `docs/audit-2.md` and synced
  `docs/documentation.md`, `docs/map.md`, `docs/context.md`, and the T11-T16
  workstream docs to the completed state.

## v2.0.1 - 2026-04-17

### Production Readiness Audit Baseline + Remediation Program

- Added `docs/audit-1.md` as the fixed production-readiness baseline for later
  audit comparison.
- Added `docs/gap-analysis.md` to map the remaining implementation gap against
  the existing documentation/spec target without lowering that target.
- Added the following tracked remediation plans under `docs/todo/`:
  - `t11-audit-remediation-program.md`
  - `t12-hook-contract-hardening.md`
  - `t13-zero-downside-and-cache-correctness.md`
  - `t14-layer2-strictness-and-cancellation.md`
  - `t15-daemon-service-productionization.md`
  - `t16-proof-gates-and-release-readiness.md`
- Updated `docs/todo.md`, `docs/context.md`, `docs/map.md`, and
  `docs/documentation.md` to link the audit baseline and the new execution
  plans.

## v2.0.0 - 2026-04-13

### Full Spec Parity: spec+.md v2.0.0-draft - Claude Code + Codex CLI

Complete implementation of all normative requirements in spec+.md v2.0.0-draft.
Scope: Claude Code and Codex CLI only (Cursor, Copilot, Gemini CLI = non-goals for this release).

#### Layer 0: Pre-Entry Filtering (`internal/filter/`, `internal/hooks/`)

- 24 built-in filters (F01-F24) fully implemented: git, build, test, lint, search, JSON, log,
  AWS, GitHub/GitLab CLI, PostgreSQL, .NET, Ruby, Python typecheckers, formatters.
- 200+ `TryCompact*` functions across 18 built-in files covering 150+ command variants.
- TOML Filter DSL (`filters_toml.go`): 8-stage pipeline (`strip_ansi`, `replace`, `match_output`,
  `unless`, `strip_lines_matching`, `keep_lines_matching`, `truncate_lines_at`, `head_lines`,
  `tail_lines`, `max_lines`, `on_empty`). Project-local + user-global merge with deduplication.
- Hook system (`internal/hooks/`): `claude.go` + `codex.go` + `verify.go`. Commands:
  `slimference hook install claude|codex`, `hook verify`, `hook remove`. SHA-256 integrity checks.
- Tee recovery (`tee.go`): raw output saved to `~/.slimference/tee/` on failure, 20-file rotation.
- SQLite tracking (`tracking.go`, `modernc.org/sqlite`): `filter_runs` schema, 90-day retention.
- Permission model (`permissions.go`): deny/ask/exclude_commands exit codes 0/1/2/3.
- `slimference gain` (`internal/analytics/gain.go`): today/week/month/all, JSON/CSV output.

#### Layer 1: All 14 Deterministic Sub-Layers (`internal/compression/`)

- L1.1 JSON Minification, L1.2 Comment Stripping (10 languages), L1.3 Exact + MinHash/LSH
  near-deduplication (128 dimensions, shingle-3, Jaccard 0.85), L1.4 Regex Structure Extraction
  (10 languages, replacing tree-sitter), L1.5 Delta Encoding (LCS unified diff), L1.6 Prompt
  Cache Breakpoint Injection, L1.7 ANSI Strip, L1.8 Tool Classifier, L1.9 Tool Compressor,
  L1.10 Success Short-Circuit, L1.11 Image Base64 Replacement, L1.12 Repeated Tool Collapse,
  L1.13 Graph Pruning (file op deduplication), L1.14 Pre-Filtered Content Tagging.
- Config renamed: `tree_sitter_*` -> `structure_*` throughout.

#### Layer 2: MiniMax M2.7 Summarization (`internal/summarization/`)

- Adaptive sliding window (`adaptive_window.go`): complexity-based dynamic window 3-7.
- Tool result priority classification (`priority.go`): HIGH/MEDIUM/LOW tiering.
- Full MiniMax client with retry (max 2), timeouts (5s connect, 30s response, 45s total).
- Anchor detection, summary cache (30-min TTL), progressive compression tiers, validation
  (5% min / 40% max ratio), graceful Layer 1 fallback on failure.

#### Layer 3: Response Cache - True LRU Fix (`internal/caching/response_cache.go`)

- **Bug fix:** `ResponseCache` was FIFO, not LRU. `Get()` now calls `promoteKey()` on hit,
  `Set()` on existing keys also promotes. New helper `promoteKey()` moves key to MRU position.
- New tests: `TestResponseCache_LRU_promotion`, `TestResponseCache_LRU_setPromotes`.

#### SSE Streaming Robustness (`internal/proxy/streaming.go`)

- `streamingRelay` now accepts `ctx context.Context` as first parameter. Client disconnect
  detection: `select { case <-ctx.Done(): return }` at top of scan loop exits relay early
  without blocking on upstream data.
- Scanner overflow (`bufio.ErrTooLong`): logged at WARN level instead of DEBUG, operator-visible.
- New tests: `TestStreamingRelay_contextCancelled`, `TestStreamingRelay_scannerOverflow`.

#### Resilience (`internal/proxy/handler.go`, `internal/proxy/proxy.go`)

- `recoverMiddleware`: panic recovery with stack trace logging + best-effort passthrough.
- `doUpstreamRequest`: rate-limit retry (429/529, max 2 retries, `parseRetryAfter` up to 30s).
- Context overflow retry: aggressive re-compress (window=2, L2 target 10%, raw fallback).
- `EventRateLimitRetry` + `EventOverflowRetry` analytics events tracked in dashboard.

#### Health Monitoring (`internal/proxy/health_monitor.go`)

- 20-slot ring buffer per provider, derived from real request outcomes only (no pinging).
- Thresholds: idle (>5 min), down (last 3 consecutive failures), degraded (>20% error rate).
- Health dots in TUI, `/health` JSON endpoint.

#### Debug & Observability (`internal/debug/`, `internal/tui/`)

- Decision chain JSONL, session replay, `slimference debug last|summary|tail|paths|replay`.
- BubbleTea TUI: 3 views (main dashboard, stats, debug log tail). Keyboard: c/x providers,
  1-3 layers, s/d/f/q views. Provider health dots, TTFT saving display, retry breakdown.
- Persistent analytics: JSONL to `~/.slimference/analytics/YYYY-MM-DD.jsonl`, flushed on shutdown.

#### Configuration & CLI (`internal/config/`, `cmd/slimference/`)

- Full TOML + environment (`SLIMFERENCE_*`) + CLI flag override hierarchy.
- Subcommands: `filter`, `hook`, `rewrite`, `gain`, `debug`, `doctor`, `stats`, `test`, `version`.

#### Test Coverage

- 100% statement/branch coverage across all 18 packages.
- New test files: `health_monitor_test.go`, `views_test.go`, LRU cache tests, SSE robustness tests.
- Integration tests: CompressesLargeConversation (ratio=0.80), PassthroughNonCompressiblePath,
  HealthEndpoint. TypeScript test suite (6 tests, bun:test).

---

## v1.4.0 - 2026-04-13

### Spec Parity Complete: §17.2 Panic Recovery + §17.7 Latency Display + Retry Breakdown

#### §17.2 Panic Recovery Middleware (`internal/proxy/proxy.go`)

- `recoverMiddleware(next http.Handler) http.Handler` added - wraps the full HTTP mux.
- On panic: logs error + full stack trace via `slog.Error`, then best-effort passthrough
  of the original request unmodified (using the body stashed in context via `origBodyKey`).
- Fallback: if body not yet stashed (panic before readBody), returns 502 Bad Gateway.
- Wired in `New()`: `Handler: p.recoverMiddleware(mux)`.
- Import `"runtime/debug"` added to proxy.go.

#### §17.7 Latency Display in Stats View (`internal/tui/views.go`)

- New "Avg Request Latency" section added to `renderStatsView`, shown after MiniMax stats.
- Displays per-provider table: Provider | Avg ms | TTFT saved/req
- Anthropic and OpenAI rows shown when data is available; MiniMax row shown separately.
- `providerTTFTSaving(snap, prov, prefillSpeed) float64` helper added to compute
  per-provider estimated TTFT improvement from `PerProvider.InputTokensSaved / Messages / prefillSpeed`.
- Uses existing `snap.LatencyAnthropicMs` / `snap.LatencyOpenAIMs` fields (already tracked).

#### Retry Breakdown (`internal/types/types.go`, `internal/analytics/collector.go`,
  `internal/proxy/handler.go`, `internal/tui/views.go`)

- Two new event types: `EventRateLimitRetry` (429/529) and `EventOverflowRetry` (context-length).
- `Analytics` struct: `RateLimitRetries int` and `OverflowRetries int` added alongside `AutoRetries`.
- `Record()` handles both new events: increments specific counter AND `AutoRetries`.
- `AnalyticsSnapshot` includes `RateLimitRetries` and `OverflowRetries`; `Snapshot()` populates them.
- `doUpstreamRequest` emits `EventRateLimitRetry` before each sleep-and-retry.
- Context overflow path emits `EventOverflowRetry` immediately on detection.
- Stats view resilience line: "Auto-retries: N (Nx rate-limit, Nx overflow)" when N > 0.

## v1.3.9 - 2026-04-13

### Spec Parity: §17.3 Rate-Limit Retry + §17.5 Provider Health TUI

#### §17.3 Rate-limit retry (`internal/proxy/handler.go`)

- `doUpstreamRequest` now implements a direct status-code-only retry loop (max 2 retries)
  for 429 and 529 responses.
- Critical fix: `resilience.Do` was replaced because it calls `io.ReadAll` on every response
  body, which would buffer complete SSE streams in memory and break all streaming responses.
- New direct loop: checks `resp.StatusCode` only; body is never read for 200/SSE responses.
- `parseRetryAfter(header string) time.Duration` added: parses integer-seconds and HTTP-date
  `Retry-After` headers, caps at 30s per spec §17.3. Falls back to exponential backoff via
  `resilience.ExponentialBackoff`.
- `"strconv"` import added; `resilience` import kept for `ExponentialBackoff` utility.

#### §17.5 Provider health dots (`internal/types/types.go`, `internal/proxy/health_monitor.go`,
  `internal/proxy/proxy.go`, `internal/tui/model.go`, `internal/tui/components.go`,
  `internal/tui/views.go`, `cmd/slimference/main.go`, `internal/tui/model_test.go`)

- `ProviderHealthStatus` enum and `ProviderHealthInfo` struct added to `types` package.
- `healthMonitor` (20-slot ring buffer per provider) in new `internal/proxy/health_monitor.go`.
  No upstream pinging - derived solely from actual request outcomes (spec §16.4).
- Health status thresholds: idle (>5 min idle), down (last 3 consecutive failed),
  degraded (>20% error rate), healthy (otherwise).
- `Proxy.GetProviderHealth(prov)` added, wired to `ProxyInterface` and `proxyAdapter`.
- TUI `renderMainView` shows colored health dots (`●`/`○`) next to each provider badge.
- `renderHealthDot` helper in `internal/tui/components.go`.
- `mockProxy.GetProviderHealth` added in test file.

## v1.3.8 - 2026-04-13

### Spec Parity: Enhanced Health Endpoint + CLI Flag Overrides

#### §17.8 Enhanced `/health` endpoint (`internal/proxy/handler.go`)

- `healthHandler` converted from standalone function to `(p *Proxy) healthHandler` method,
  giving it live access to all proxy state.
- Response now includes: `status`, `service`, `version`, `layers` (1/2/3 enabled state),
  `providers` (anthropic/openai enabled state), `queue_depth` (compress + analytics queues),
  `cache_entries` (live LRU count), `minimax_configured` (API key present).
- `ResponseCache.Len() int` added to `internal/caching/response_cache.go` (read-lock guarded).
- `var Version = "dev"` added to `internal/proxy/proxy.go`; set by `cmd/main.go` at startup
  as `proxy.Version = version` before any other call.
- `TestHealthHandler` updated to use method call on a real Proxy instance; asserts all new fields.

#### §13.3 CLI flag overrides (`cmd/slimference/main.go`)

- `main()` sets `proxy.Version = version` and routes flag args (`--`) to `runTUIFn()` instead
  of `handleSubcommand()`.
- `applyTUIFlags(cfg, os.Args[1:])` called in `runTUI()` after config load, before logging setup.
- Supported flags at the time: `--port`/`-port`, `--sliding-window`,
  `--no-layer1`, `--no-layer2`, and `--log-level`.
- `TestApplyTUIFlags` added with 11 parallel subtests covering all flags, combinations,
  invalid values (zero port, non-numeric port), and unknown flags.

## v1.3.7 - 2026-04-13

### Reliability Audit + Rotating Debug Logger + Docs Flush

#### Rotating JSONL logger (`internal/slogutil`)

- New `RotatingWriter`: goroutine-safe `io.Writer` with size-based rotation (10 MB per file, 5 copies).
- `setupLogging()` in `cmd/slimference/main.go` wires it as the `slog.Default` handler.
- Defaults updated: `logging.level="debug"`, `logging.format="json"`, `logging.file="~/.slimference/logs/slimference.jsonl"`.
- All existing `slog.*` calls across all packages now go to the rotating file automatically.

#### Strategic debug logging

- Hot path (`handleCompressibleRequest`): request-scoped logger with `req_id`, `provider`, `model`.
  Events: `request started`, `layer1 applied` (with per-sub-layer savings), `layer2 applied`, `request_processed`.
- Layer 0 (`filter/pipeline.go`): `layer0 exec`, `layer0 filter applied` (includes filter name), `layer0 passthrough`, `layer0 result`.

#### Reliability fixes (7 bugs)

| Bug | Fix |
|-----|-----|
| Panic: send to closed subscriber channel | `trySend()` with `recover()` in sessions/logger.go |
| Hot path blocked by analytics queue | All 5 `analyticsQueue` sends made non-blocking |
| Double `close(shutdownCh)` on concurrent shutdown | `sync.Once` wraps entire Shutdown() body |
| No graceful proxy shutdown on TUI quit | `p.Shutdown(ctx)` added after `runTeaProgramFn` returns |
| `reconstructBody` error silently discarded | Error checked; 500 returned to client on failure |
| `json.Marshal` silent null payload in analytics | Errors propagated from WriteEvent/WriteSnapshot/writeLine |
| fsnotify kqueue data races under `-race` | `t.Parallel()` removed from 3 caching tests that touch OS-level kqueue |

#### Docs flush

- `docs/documentation.md` updated to v1.3.5: new slogutil package, updated logging defaults,
  non-blocking analytics description, idempotent Shutdown, trySend rationale, request-scoped
  logging tables, Layer 0 debug events, race detector status.
- `docs/context.md` rewritten to current state (was stale at v1.2.0).
- Changelog entry added.

## v1.3.6 - 2026-04-13

### Integration Tests Fixed + TypeScript Tests + Initial Git Commit

#### Integration Tests (`tests/integration/`)

- **Root cause 1 - compression test**: Layer 1 only compresses `tool_result` blocks; test was using
  plain string message content (parses as `{type:"text"}`) which is skipped entirely by the compressor.
  Fixed by rewriting messages to use array-form content with `tool_result` blocks containing identical
  large filler. Dedup fires for repeated occurrences in the compressible prefix. Result: ratio=0.80, layers=[1].
- **Root cause 2 - passthrough test**: `detectProvider("/v1/models", body)` returns `OpenAI` (path has
  no `/messages`). `newTestProxy` only set Anthropic upstream to mock; OpenAI upstream still pointed to
  `https://api.openai.com` → real network call returned 400. Fixed by also setting
  `cfg.Upstream.OpenAI.BaseURL = upstreamURL` in `newTestProxy`.
- All 3 integration tests now passing: `CompressesLargeConversation`, `PassthroughNonCompressiblePath`,
  `HealthEndpoint`.

#### TypeScript Tests (`tests/ts/`)

- Fixed wrong relative paths in `cli.test.ts`: `../../cmd/slimference` → `./cmd/slimference` (paths
  are relative to `cwd=moduleRoot`, not relative to the test file).
- All 6 bun:test tests passing: 3 session fixture schema tests + 3 CLI integration tests.

#### Initial Git Commit

- Repository initialized and full codebase committed locally (508 files, 145782 insertions).
- Updated `.gitignore` to exclude build artifacts (`/benchmarks`, `/ci`, `/slimference`, `/slimference.test`,
  `*.out`, `*.test`).

## v1.3.5 - 2026-04-13

### Risk Mitigations Verified + Synergy Documentation + Bug Fixes

#### Risk Mitigations Audit

All four open risk mitigation items verified as implemented:

- **Filter false-positive**: `([]byte, bool)` + length-check pattern verified across all 24 built-in filters.
  No filter can produce output without it being strictly shorter than input. Passthrough guaranteed on
  all parse/unmarshal errors (JSON validity pre-checked before unmarshal; `ok=false` on any mismatch).
- **Graph Pruning**: `messageReferencesIndex` already implemented in `PruneRedundant`. Checks "message N",
  "msg N", "[N]" case-insensitively in all subsequent messages before pruning any candidate.
- **Provider invisibility**: Headers forwarded 1:1 (only hop-by-hop headers Host/Content-Length/Connection/
  Transfer-Encoding dropped). No custom proxy headers. URL path + query pass through unchanged. SSE relay
  streams immediately without buffering. Verified against spec+.md §16.4.
- **Image Base64**: Dimensions extracted for all PNG/JPEG images. Terminal screenshot heuristic (>30%
  printable ASCII) works for text-based data URIs and SVG. Known limitation: PNG-encoded terminal
  screenshots are treated as regular images (show dimensions only, no text extraction). Not a bug.

#### Bug Fix: Transfer-Encoding Header in Passthrough

**`internal/proxy/handler.go`** - `handlePassthrough()`:
- Added `Transfer-Encoding` to the hop-by-hop header skip list (alongside Host, Content-Length, Connection)
- Without this fix, passthrough requests with chunked encoding from client would forward the
  Transfer-Encoding header to upstream, potentially conflicting with the explicit ContentLength set below
- `doUpstreamRequest()` already skipped Transfer-Encoding correctly; now both paths are consistent
- Verified: all proxy tests pass, 100% coverage maintained

#### Synergy Optimizations Documentation

New **Section 17** added to `docs/documentation.md`:

- **17.1 L0→L1 Cascade**: Compact L0 output dramatically improves L1 dedup hit rate, delta quality,
  and prompt cache prefix stability. Table: dedup/MinHash/delta/cache impact with vs without L0.
  Concrete example: 8000-byte go test output vs 26-byte compact version.
- **17.2 Response Cache Key Stability**: L0 deterministic compact output eliminates timestamp/process-ID
  variance, increasing the cache-layer hit rate from ~5% to 30-40%.
- **17.3 MiniMax Input Reduction**: L0-filtered messages reduce MiniMax summarization input 5-10x,
  lowering cost, latency, and improving summary quality.
- **17.4 Prompt Cache Prefix Extension**: Stable compact tool_results extend Anthropic prompt cache
  prefix to 8-15 messages (vs 1-3), reducing effective token cost 60-80% on cached messages.
- **17.5 Compression Multiplier Stack**: Numeric example: 100K tokens without compression -> 1K tokens
  with all four layers active (99% reduction in optimal case).

#### Benchmark Infrastructure

Added benchmark functions and runner for performance regression tracking:

- **`internal/compression/bench_test.go`** (new) — 8 benchmarks:
  `BenchmarkCompress_small_8msg`, `BenchmarkCompress_medium_12msg`, `BenchmarkCompress_large_22msg`,
  `BenchmarkCompress_code_12msg`, `BenchmarkStripANSICodes_short/long`, `BenchmarkStripComments_go`,
  `BenchmarkExtractStructure_go`
- **`internal/filter/bench_test.go`** (new) — 7 benchmarks:
  `BenchmarkTryCompactGitStatus`, `BenchmarkTryCompactBuildOutput`, `BenchmarkTryCompactJSONMinify_large`,
  `BenchmarkRunPipeline_gitStatus`, `BenchmarkApplyLayer0AfterANSI_noMatch`,
  `BenchmarkTruncateStdoutWithHint_noTrunc/truncates`
- **`scripts/benchmarks/main.go`** (new) — standardized runner:
  `go run ./scripts/benchmarks -benchtime=3s -count=1 -pkg=<name>`;
  runs `go test -bench=. -benchmem -run=^$` on compression + filter packages
- **`scripts/README.md`** updated with concrete command examples

---

## v1.3.4 - 2026-04-13

### Session Replay: Full Pipeline Implementation

#### `slimference debug replay <session.jsonl>` - Now Fully Functional

**`internal/debug/session.go`** - Added `ReplaySession(path string) ([]RequestSummary, error)`:
- Reads all JSONL lines from a decisions log file (oldest first)
- Parses each line as `RequestSummary`; malformed lines are silently skipped
- Returns slice of summaries; scanner errors surfaced as return value
- Uses 2 MB per-line buffer (consistent with Recorder's JSONL format)

**`cmd/slimference/main.go`** - `handleDebugReplay` fully implemented:
- Keeps file stats header (file, size, non-empty lines) for quick orientation
- Calls `replaySessionFn(path)` (injectable var, default = `dbg.ReplaySession`)
- For each `RequestSummary`: shows timestamp, provider/model, token savings + ratio
- Optionally shows layers applied, Layer 1 sub-layer breakdown (blocks + saved per sub-layer)
- Optionally shows Layer 2 stats (compression ratio, anchor count) when `Applied=true`
- Footer: total request count + total tokens saved across session
- "No decodable request summaries found." printed when file has no valid RequestSummary JSON

#### Test Coverage

**`internal/debug/session_test.go`** - 5 new tests:
- `TestReplaySession_happy`: valid 2-record JSONL, order preserved, field values correct
- `TestReplaySession_mixedLines`: non-JSON lines skipped, valid lines retained
- `TestReplaySession_empty`: whitespace-only file returns empty slice
- `TestReplaySession_nonExistentFile`: os.Open error returned
- `TestReplaySession_scanError`: scanner error on line > 2 MB returned

**`cmd/slimference/main_test.go`** - 3 new tests + import:
- `TestHandleDebugReplay_replayParseErrorExits1`: inject error via `replaySessionFn`, verify exit 1
- `TestHandleDebugReplay_noSummaries`: non-JSON JSONL produces "No decodable..." message
- `TestHandleDebugReplay_fullOutput`: full replay with layer1 + layer2 output verified end-to-end

All 17 packages: 100% statement coverage maintained.

---

## v1.3.3 - 2026-04-13

### F01/F05 Git Filter Completions + Documentation Overhaul

#### F01 Enhancement: Rename and Conflict Detection

**`internal/filter/builtin_git.go`** - `TryCompactGitStatus`:
- Added `renamed` counter: incremented when `line[0] == 'R'` (staged rename) or `line[0] == 'C'` (staged copy)
- Added `conflicts` counter: incremented for conflict codes (`UU`, `AA`, `AU`, `UD`, `UA`, `DU`, `DD`)
- Conflict lines skip staged/worktree counting via `continue`
- `renamed:N` and `conflicts:N` only appear in output when N>0 (no noise for clean repos)
- Output format: `[git status] N paths (staged:S worktree:W untracked:U[ renamed:R][ conflicts:C])`

**`internal/filter/builtin_git_test.go`** - `TestTryCompactGitStatus_renameAndConflict`:
- 7 test cases covering: staged rename (R), copy (C), UU/AA/AU/DD conflicts, no-conflict passthrough

#### Documentation and Todo Cleanup

#### F05 Enhancement: Full Push/Pull/Fetch/Merge/Rebase Confirmations

**`internal/filter/builtin_git.go`** - `TryCompactGitF05` extended:
- `git push` success: extracts ref update lines → `[git push] N ref(s) updated\n  <refs>`
- `git push` new branch: detects `* [new branch]` → included in ref count
- `git fetch`/`pull` success: counts `abc..def branch -> origin/branch` updates + `* [new branch/tag]` → `[git fetch] N updated, M new`
- `git merge` fast-forward: detects "Fast-forward" → `[git merge] fast-forward (N file(s), +X/-Y)`
- `git rebase` success: detects "Successfully rebased" → `[git rebase] ok`
- Helper functions: `compactGitPushOutput`, `compactGitFetchOutput`, `extractMergeStatLine`
- All return "" / passthrough when compact result is not shorter than input

**`internal/filter/builtin_git_test.go`** - new tests:
- `TestTryCompactGitF05_pushSuccess`: ref update, new branch push, no-refs passthrough
- `TestTryCompactGitF05_fetchSuccess`: updates + new branches, no-update passthrough
- `TestTryCompactGitF05_mergeSuccess`: fast-forward with/without stat, non-ff passthrough
- `TestTryCompactGitF05_rebaseSuccess`: successful rebase detection
- `TestCompactGitPushOutput_notShorter`, `TestCompactGitFetchOutput_noUpdates`, `TestExtractMergeStatLine_noMatch`

- `docs/todo.md`: All F01-F24 filter items audited and marked done; ProxyInterface item marked done; docs/map items marked done
- `docs/documentation.md`: Structure corrected (config keys, testing section, package structure, new CLI commands)
- `docs/map.md`: Added hooks/claude.go, codex.go, verify.go; filter/builtin_read.go, builtin_compact_helpers.go, project_filters.go; tui HookStatus/renderHookStatus

---

## v1.3.2 - 2026-04-13

### TUI Hook Status Indicator + Documentation Overhaul

#### TUI — Hook Status Display

**`internal/tui/model.go`**
- Added `HookStatus` struct (`Claude bool`, `Codex bool`)
- Added `hookStatus HookStatus` field on `Model`
- Added `SetHookStatus(HookStatus)` method — called from `cmd/slimference/main.go` at startup

**`internal/tui/views.go`**
- Added `renderHookStatus(s Styles, h HookStatus) string`
  - Returns `""` when both hooks absent (no UI noise)
  - Shows `"Hooks: claude ✓  codex ✓"` with green/muted styling per state
- Inserted hook status block into `renderMainView()` between provider badges and usage section

**`internal/hooks/verify.go`**
- Added `InstalledStatus(home string) (claude, codex bool)`
  - Claude Code: checks `~/.claude/hooks/slimference-rewrite.sh` existence
  - Codex: checks `~/.codex/AGENTS.md` for `SLIMFERENCE_BEGIN` marker

**`cmd/slimference/main.go`**
- Hook status read at startup via `hooks.InstalledStatus(osUserHomeDir())`
- Passed to TUI via `model.SetHookStatus(...)` before BubbleTea program starts

#### Tests — 100% Coverage Maintained

- `internal/hooks/hooks_test.go`: 4 new tests for `InstalledStatus` (none/claude/codex/both)
- `internal/tui/model_test.go`: 6 new tests for `HookStatus`, `SetHookStatus`, `renderHookStatus`, main view rendering with hooks

#### Documentation

- `docs/documentation.md`: updated Section 11 (TUI Dashboard) with hook status indicator details;
  Section 12 config keys `tree_sitter_*` -> `structure_*` corrected; Section 13 CLI commands
  expanded with filter/hook/rewrite/gain/debug subcommands; Section 15 integration test status
  updated; Section 16 package structure updated with all new files
- `docs/map.md`: added `internal/hooks/claude.go`, `codex.go`, `verify.go`; added
  `internal/filter/builtin_read.go`, `builtin_compact_helpers.go`, `project_filters.go`;
  updated TUI model/views entries with `HookStatus`/`renderHookStatus`
- `docs/todo.md`: marked documentation, map, coverage-gate items as done

---

## v1.3.1 - 2026-04-13

### 100% Test Coverage + L1.6 Prompt Cache Integration Test

#### cmd/slimference — Full Test Coverage

**`cmd/slimference/main.go`** — refactored for in-process testability (no subprocess spawning):
- Added injectable package-level vars: `configLoadFn`, `runTUIAfterStartFn`, `proxyStartTimeout`, `runTeaProgramFn`, `tuiSendProxyEventFn`, `makeSignalChanFn`
- Extracted `runTUIAfterStart(p, progCh)` from `runTUI` — now independently injectable/testable
- Added `progSender` struct with `send(rm)` method (replaces closure, avoids `tui.SendProxyEvent` blocking in tests)
- Signal goroutine cleanup: `defer func() { signal.Stop(sigCh); close(done) }()` prevents goroutine leak on panic unwind

**`cmd/slimference/main_test.go`** — 6 new test functions:
- `TestProgSender_send_withProg` — covers `select` branch with prog in channel
- `TestProgSender_send_noProg` — covers `default` branch (no prog yet)
- `TestRunTUI_proxyStartOK` — covers `case <-time.After(proxyStartTimeout)` (normal start)
- `TestRunTUIAfterStart_signalPath` — covers signal goroutine body via channel-based exit capture
- `TestRunTUIAfterStart_tuiError` — covers TUI error path via `captureExit` panic pattern
- `TestMakeSignalChanFn_default` — covers the default `makeSignalChanFn` implementation

#### L1.6 Prompt Cache Breakpoint Verification

**`internal/proxy/handler_compressible_test.go`** — `TestServeHTTP_promptCacheBreakpointsInjected`:
- End-to-end integration test: builds 7-exchange Anthropic conversation with 1500-char messages
- Verifies that `cache_control: {type: "ephemeral"}` breakpoints appear in upstream request
- Confirms `CompressiblePrefixEnd` + `OptimizeCacheBreakpoints` pipeline works correctly with real request flow
- Uses `json.RawMessage` to handle mixed string/array Anthropic content format

#### Config Fix

**`internal/config/defaults.go`** — `DefaultTOML()` `structure_languages` extended from 5 to 10 languages:
- Added `c`, `cpp`, `java`, `ruby`, `shell` (matching `structure.go` which already supported all 10)

#### Coverage

All 17 production packages: **100% statement coverage** on `cmd/slimference` and all `internal/` packages.

---

## v1.3.0 - 2026-04-12

### L1.14 + Debug Decision Chain + Phase E Documentation

#### New Components

**`internal/compression/prefilter_tag.go`** (L1.14)
- `isPreFiltered(content string) bool` - detects Layer 0 compact markers on first line
- Pattern set: `[git *]`, `[×N]`, `[ok]`, `[N matches]`, `[full output:]`, `[build]`, `[test]`, `[search]`, `[grep]`
- Integrated into `compressMessage`: skips JSON compact (L1.1), comment strip (L1.2), structure extract (L1.4) when pre-filtered
- Test coverage: `prefilter_tag_test.go` (11 cases including integration test)

**`internal/debug/decisions.go`**
- `DecisionEntry` type: per-block compression decision record (timestamp, req_id, msg_idx, block_idx, layer, sub_layer, action, reason, tokens before/after, settings)
- `RequestSummary` type: per-request aggregate (provider, model, tokens, layer1_breakdown map, layer2 details)
- `Recorder` struct: thread-safe ring buffer (configurable capacity, defaults to 100)
  - `Record(RequestSummary)` - adds to ring, optionally flushes to JSONL
  - `Last(n int, withEntries bool) []RequestSummary` - returns newest-first
  - `Aggregate() map[string]SubLayerBreakdown` - cross-request totals
  - `flushJSONL(path, summary)` - appends to `decisions_log` JSONL file
- `NopRecorder` - no-op implementation for disabled debug mode
- Test coverage: `decisions_test.go` (7 test functions)

**`internal/proxy/proxy.go`** - added `debugRecorder *dbg.Recorder` field, initialized on `New()` from `cfg.Debug.DecisionsLog`
**`internal/proxy/handler.go`**
- `newRequestID()` - crypto/rand hex ID for debug correlation
- `buildLayer1Breakdown(Layer1Result) map[string]SubLayerBreakdown` - converts result to per-sub-layer map
- Records `RequestSummary` to `debugRecorder` after every request

**`cmd/slimference/main.go`** - `handleDebugLast` updated:
- Reads `cfg.Debug.DecisionsLog` JSONL first (proxy Layer 1-3 summaries)
- `readLastDecisionSummaries(path, n)` reads last N entries from JSONL
- Falls back to SQLite `filter_runs` if no decisions log configured
- Supports `slimference debug last N` for multiple entries

#### Documentation (Phase E)
- `docs/documentation.md`: updated to v1.2.0; added Layer 0 section (§3), L1.14 sub-layer table, L2.8-L2.9 sub-sections, Debug & Observability section (§10), renumbered all sections; Package Structure expanded with all new files
- `docs/map.md`: full rewrite including Layer 0 filter package, all compression sub-layers (L1.1-L1.14), L2.8-L2.9, debug package, hooks package; updated dependency graph

#### Tests
All 18 packages pass. Full test suite clean.

## v1.0.0 - 2026-04-09

### Initial Implementation

Complete implementation from scratch based on spec.md v1.0.0-final.

#### Packages
- `internal/types`: Core shared types (Message, ContentBlock, RingBuffer, events)
- `internal/config`: TOML config loading with env var overrides and validation
- `internal/tokens`: Token counting (tiktoken cl100k_base) and usage tracking
- `internal/security`: Secret detection (12 patterns) with redact/warn/block/off modes
- `internal/compression`: Layer 1 deterministic compression (JSON compact, comment strip, dedup, regex-based structure extraction, delta encoding, Anthropic prompt cache optimization)
- `internal/summarization`: Layer 2 MiniMax M2.7 integration (anchor detection, summary cache, quality validation, progressive tiers)
- `internal/caching`: response/provider cache, now Layer 2 (LRU, TTL) + fsnotify file watcher
- `internal/analytics`: Session metrics collection, per-provider stats, JSONL persistence
- `internal/resilience`: HTTP retry with exponential backoff, health checks, latency tracking
- `internal/sessions`: In-session log ring buffer with subscriber fan-out
- `internal/proxy`: HTTP reverse proxy (provider detection, message extraction, compression pipeline, SSE relay)
- `internal/tui`: BubbleTea TUI (main/stats/debug views, lipgloss styling, keyboard controls)
- `cmd/slimference`: Entry point, CLI subcommands, adapter wiring

#### Test Coverage (13 files)
- `internal/types`: RingBuffer push/last/overflow/concurrent/len
- `internal/config`: Load, env overrides, defaults
- `internal/compression`: JSON compact, comment strip, dedup, Layer 1 integration
- `internal/caching`: LRU eviction, TTL expiry, cache hit/miss
- `internal/security`: Pattern matching, entropy filtering, scan integration
- `internal/analytics`: Record/snapshot/ratio/concurrent, EstExtraMessages, AvgTTFTImprovement
- `internal/summarization`: Anchor detection (edit/error/decision/config), validator quality checks
- `internal/resilience`: Do retry loop, max retries, context cancel, backoff, IsContextOverflow
- `internal/proxy`: detectProvider, extractMessages (all block types), reconstructBody

#### Architecture Decisions
- No CGO: tree-sitter replaced with regex-based code structure extraction
- Interface-based TUI/proxy decoupling via tui.ProxyInterface (prevents import cycles)
- Atomic toggle switches for zero-lock provider and layer enable/disable
- AnalyticsSnapshot computed fields for TUI display (no separate calculation in view layer)

---

## v1.2.0 - 2026-04-12

### Layer 1.8-1.13 + Layer 2.8-2.9 Implementation

New compression sub-layers implementing the remaining spec+.md §5/§6 features.
All tests pass. Total coverage: 97.4%.

#### New Components

**`internal/types`**
- `ToolResultType` enum (11 values: Unknown, GitOutput, TestOutput, BuildOutput, LintOutput, FileRead, SearchResult, JSONData, LogOutput, DirListing, CommandOutput)
- `ToolResultPriority` enum (Low/Medium/High)

**`internal/compression`**
- `tool_classifier.go` (L1.8): `classifyToolResult(toolName, content)` - tool_name first, then content pattern matching for 9 types
- `tool_compressor.go` (L1.9): `compressToolOutput(type, content, messageAge, window)` - per-type RTK-style filters with aggressive/moderate modes based on message age; filters for git, test, build, lint, log, dir, search output
- `image_replace.go` (L1.11): `replaceImageBase64(block, msgIdx, prefixEnd)` - replaces "image" type blocks and inline data URIs; extracts PNG/JPEG dimensions; tries terminal text extraction from high-printable data
- `repeated_collapse.go` (L1.12): `ToolCallIndex.CollapseRepeated` - hashes (tool_name, normalized input), compares result hashes; collapses only when call+result identical AND replacement is shorter
- `graph_pruning.go` (L1.13): `FileOpGraph.PruneRedundant` - builds Read/Edit/Write op graph; prunes Read@i when Edit@j and Read@k exist (k>j>i); safety-checks for message index references

**`internal/compression/layer1.go` updates**
- `Layer1Result`: added ToolCompressorSaved, ImageSaved, RepeatedCollapseSaved, GraphPruningSaved
- `DeterministicCompressor`: added ToolCallIndex and FileOpGraph fields
- `compressMessage`: inserted L1.8+L1.9 between delta and success-short-circuit; added L1.11 image path; skips tool compressor when delta/structure already transformed text
- Cross-message L1.12 and L1.13 run after per-message loop in `Compress()`
- `Reset()`: resets new stateful components

**`internal/summarization`**
- `adaptive_window.go` (L2.8): `AdaptiveWindowSize(messages, base)` - complexity score from UniqueFilePaths, AnchorDensity, ToolCallDiversity; adjusts window by +-2 from base; clamped to [max(3,base-2), base+2]
- `priority.go` (L2.9): `ClassifyPriority(type, content, isAnchor)`, `SummarizationHint(messages)` - builds HIGH/MEDIUM/LOW priority hint for MiniMax prompt injection

#### Tests
New test files with full coverage of all new code paths.

---

## v1.1.0 - 2026-04-12

### Coverage Push to 97.8% Total

Comprehensive test coverage expansion across all packages. All tests pass.

#### Coverage Results
| Package | Coverage |
|---------|----------|
| `cmd/slimference` | 89.3% |
| `internal/analytics` | 97.5% |
| `internal/caching` | 99.3% |
| `internal/compression` | 99.8% |
| `internal/config` | 98.6% |
| `internal/debug` | 100.0% |
| `internal/filter` | 99.6% |
| `internal/hooks` | 97.2% |
| `internal/proxy` | 96.6% |
| `internal/resilience` | 100.0% |
| `internal/security` | 100.0% |
| `internal/sessions` | 100.0% |
| `internal/summarization` | 98.8% |
| `internal/tokens` | 97.4% |
| `internal/tui` | 99.7% |
| `internal/types` | 100.0% |
| `internal/util` | 100.0% |
| **Total** | **97.8%** |

#### Key Additions
- `internal/caching/file_watcher_test.go`: Added 8 new tests covering watcher.Add errors, Unwatch remove errors, pruneStale remove errors, debounce timer creation/reset, Unwatch on non-tracked paths, maxWatchedDirs cap, Chmod event filtering, and onChange fire path. Coverage 97.2% -> 99.3%.
- `internal/filter/builtin_lint_test.go`: Additional branch coverage for BiomeCheck and BiomeFormat.
- `internal/hooks/hooks_test.go`: Added MkdirAll error path for `mergeClaudeSettings` via 0555 permission trick. Coverage 96.2% -> 97.2%.
- `internal/summarization/layer2_run_job_test.go`: Added `TestLayer2_RunCompressionJob_emptyToSummarize` covering all-anchor-message early return.
- `internal/proxy/proxy_unit_test.go`: Added file watcher callback test, invalid regex pattern test, persister init error test, port-in-use Start test, and ClearLayer2/CompressQueue/SessionLogger tests.
- `cmd/slimference/main_test.go`: 3079-line comprehensive test suite (139 test functions) covering all CLI subcommands, error paths, subprocess tests for os.Exit paths, and doctor command checks.

#### Remaining Gaps (practical ceiling)
- `cmd/slimference main()` + `runTUI()` (0%): Require full TUI terminal + proxy startup; not unit-testable.
- `testIntercept` 60-second timeout path (7 stmts): Impractical.
- Subprocess-only os.Exit paths (~15 stmts): Coverage only counted for in-process execution.
- Defensive guards on impossible errors (~40 stmts across all packages): json.Marshal on concrete structs, os.UserHomeDir failure, fsnotify.NewWatcher failure, sql.Open failure, tiktoken init failure, timer goroutine tick paths (60s/5min intervals).

### 2026-05-15 - T149f planner hot-path behavior gates

Promoted the cross-layer planner from telemetry-only advice into the HTTP
compression hot path for L0/L1/L2. Planner actions now skip L0 proxy compaction
on bypass, switch L1 into cheap-only mode for small/recent-edit safety gates,
coordinate L1 with L2 only when the planner says L2 will run, and prevent L2
cache apply/background enqueue only for hard L2 bypasses such as disabled policy
or edit-window safety.

### 2026-05-15 - T149g planner fact closure

Closed the remaining planner placeholders. Recent-edit protection now reads
file-backed hook turn state, live-corpus confidence can come from explicit
config or corpus metadata, and direct WebSocket routes report inspect-only
shape-registry knowledge without enabling frame mutation.

### 2026-05-15 - T148a output-reduce repair feedback

Added repair-followup detection for output-reduce. Follow-up turns asking for
missing detail or reporting malformed patch/application failures now bypass
brevity injection and feed a one-shot repair signal into the previous
provider/model/profile/task-shape auto-tune bucket.

### 2026-05-15 - T143d semantic stacktrace compaction

Layer 1 test-output compression now has a semantic stacktrace reducer. Large
test failures keep anchors, assertion/diff detail, app source frames, and
package summaries while collapsing framework/vendor frames and excess diff or
context lines behind explicit omitted-count markers.

### 2026-05-15 - T144a Layer 2 task contracts

Layer 2 summaries now select task-shaped prompt contracts for coding,
debugging, review, planning, documentation, live E2E, and generic sessions.
The validator also rejects invented file paths absent from the summarized
source slice, while tolerating normalized relative/absolute path variants.

### 2026-05-15 - T146d live-corpus scenario validators

The live-corpus gate can now fail categories on declared scenario validators:
tool-heavy, cache-reuse, output-reduce, planner-alignment, WebSocket, low-error,
layer-combo-diversity, and L2-summary. Unknown validator names fail closed so a
metadata typo cannot silently remove proof pressure from aggressive defaults.

### 2026-05-15 - T145b prompt-cache heat map

`gain --proxy` now reports prompt-cache heat by stable-prefix hash. Text shows
the hottest rows, JSON exposes `prompt_cache_heat`, and CSV carries heat-key
count plus the top hash/cached-token count, while keeping provider cache
credits separate from local token-deletion claims.

### 2026-05-15 - T142b WebSocket shadow estimator

WebSocket inspection now emits non-mutating shadow accounting for text frames.
JSON payloads get `json_compact` would-save bytes/tokens and applied-layer
labels, while non-JSON and RSV/compressed frames report explicit blockers. Raw
frame bytes remain byte-for-byte unchanged.

### 2026-05-15 - T145c Anthropic prompt-cache truth reconcile

T145 tracking now reflects the already-landed Anthropic cache-control path:
stable-prefix token gate, max-four breakpoint cap, high-value tool-result
scoring, non-mutating message copies, and proxy upstream-request coverage.
Remaining T145 work is explicitly limited to WebSocket response-ID proof and
30+ turn live-corpus hit-rate proof.

### 2026-05-15 - T148b output-reduce profile rows

`gain --output` now reports provider/model/profile/task-shape rows in text,
JSON, and CSV. Rows include requests, applied/skipped counts, directive input
overhead, observed output tokens, applied-turn output tokens, and averages,
while still refusing to claim output savings without a comparable baseline.

### 2026-05-15 - T143e multi-language structure truth reconcile

T143 tracking now reflects the live structural extractor coverage: requested
Go/TypeScript/JavaScript, Python, Rust, Svelte, Zig, C/C++, SQL, and Markdown
stacks plus Swift, Kotlin, PHP, Dart, Scala, Elixir, Solidity, GraphQL, HCL,
Dockerfile, and Makefile. This is structural slicing only, not a false AST
body-on-demand claim.

### 2026-05-15 - T144 local Layer 2 status reconcile

T144 tracking now reflects landed local Layer 2 pieces: adaptive ROI candidate
gating, session-keyed background summaries, hierarchical capsules, deterministic
outbound pre-processing, original context fallback on failed summaries, and
doctor/TUI provider policy status. Remaining work is narrowed to live-corpus
quality/default-on proof.

### 2026-05-15 - T140 Codex CLI Layer 0 resolver polish

Codex CLI proxy Layer 0 now recognizes additional safe command/read shapes
(`cmdline`, `shell_command`, `args`, local-shell aliases, and read path aliases)
before compacting tool output. Unknown shapes still fail open and rewrites remain
accepted only when the token count decreases.

Planner telemetry is also tightened for the current CLI-only route: Codex HTTP
provider requests explicitly bypass WebSocket mutation, Codex cache accounting is
not mislabeled as prompt-cache-key mutation without a previous response id, and
exact-reply prompts stay out of Layer-3 directive plans.

`slimference proxy run codex --proxied -- <args>` now executes the same safe
one-process Codex environment that `proxy env codex --proxied` prints, removing
the copy/paste or `eval` step for normal repo-local tests.
