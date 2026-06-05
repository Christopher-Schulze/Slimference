# T262 - Layer 2 deterministic context ledger rewrite

## Why

Classic summarization is the wrong default for the product standard. Even a
deterministic extractive summary omits details. Omitted details can become model
memory loss, context drift, wrong reconstruction, or worse workflow decisions.
Layer 2 must stop being "summary as truth" and become a deterministic,
archive-backed context ledger where compact facts are an index, not a lossy
replacement for reality.

## Current reality check

- Layer 2 exists and is default-off.
- The primary local path is deterministic extractive summarization, with
  provider-based summarizers opt-in/fallback.
- Deterministic extraction is reproducible but not complete.
- Old context replacement is the real risk: the model sees the replacement as
  session memory.
- `internal/contextledger` now contains pure deterministic capsule builders for
  command, file, search, failure, decision, and recovery observations. Capsules
  store compact facts, provenance, stable hashes where raw bytes exist, and
  archive ids, never raw omitted content.
- `docs/ocrl.md` now defines OCRL, the Old Context Replacement Layer, as the
  product replacement for classical Layer 2 summary-as-truth. OCRL has explicit
  `off`, `shadow`, `auto`, and `max` modes, route eligibility, archive
  recoverability, positive-token-savings, and zero-product-drawdown gates.
- `internal/contextledger/ocrl.go` now implements the pure OCRL engine. It
  selects capsules through the existing fail-closed selector, verifies archive
  recoverability without copying raw bytes, renders stable machine-readable
  capsule text, keeps Codex WSS shadow-only, and applies only on full-history
  HTTP-style routes when net savings are positive.
- `internal/contextledger/message_apply.go` now implements exact full-history
  message/block application. It accepts only explicit targets, requires exactly
  one archive per target capsule, requires the archive payload to be byte-equal
  to the current target block text, counts only selected targets in final net
  savings, includes covered-marker overhead, and full-passes invalid targets,
  duplicate targets, archive mismatch, shadow mode, Codex WSS, and non-positive
  selected savings. It can also derive explicit targets from full-history
  messages by exact archive-payload equality, but only when one capsule archive
  matches exactly one current message block; ambiguous or missing evidence is
  omitted and reported.
- Codex WSS Phase-F now records content-free OCRL shadow telemetry in debug
  request summaries. It reports mode, route, reason, candidate/verbatim/rejected
  counts, archive expansion count, original archive tokens, replacement tokens,
  and would-save tokens without inserting any model-facing text or counting the
  would-save value as product `net_tokens`.
- OCRL is now a real operator policy surface, not a hard-coded proof path.
  `[compression.ocrl]` defaults to `mode="shadow"`, `max_capsules=512`,
  `min_net_saved_tokens=1`, and `max_replacement_tokens=0`, with matching
  `SLIMFERENCE_OCRL_*` env overrides. `slimference layer2 status` reports the
  effective OCRL policy and the Codex WSS shadow-only route guard.
- `contentarchive.Peek` supports OCRL proof verification by loading exact
  archive payloads without incrementing real expansion/recovery counters.
- `internal/abharness` now understands OCRL archive lists in rendered
  `[ocrl:v1 ...]` blocks. Direct-vs-OCRL replay treats replaced or deleted old
  blocks as recoverable only when the listed archives expand to the exact direct
  block text; missing or mismatched payloads remain lost-comprehension issues.
- The Codex Layer-0 reducer now feeds those builders in the hot path as
  content-free telemetry only for tool-output command/file/search/failure
  observations. `/admin/state.savings` exposes those capsule counts globally and
  per route. It also retains the actual capsule objects internally for future
  OCRL promotion. Decision/recovery capsules are pure primitives only until a
  real provenance source is wired. No capsule is inserted into Codex WSS
  model-facing context yet.
- Model-facing summary replacement is now separately gated. `layer2_enabled=true`
  can no longer make cached summaries or mid-exchange summaries replace
  model-facing history unless the explicit legacy override
  `[compression.summary].allow_model_facing_replacement=true` is also set.
  The reverse is also enforced: the legacy override alone cannot apply cached
  summaries when `layer2_enabled=false`, including overflow recovery.
- The planner now uses the same split: Layer 2 can be enabled for background
  work, but it cannot report or drive a model-facing `run` decision for
  classical summaries unless that legacy override is set.
- Capsule selection now requires an explicit policy session id. If a future
  insertion caller cannot prove the current session namespace, capsules stay
  verbatim instead of risking cross-session memory contamination.

## Product target

Layer 2 becomes a Context Ledger:

- active working set remains verbatim
- old inactive context can be represented by exact structured facts
- raw source remains archive-backed and recoverable
- no semantic paraphrase is a default source of truth
- no LLM-generated summary is default-on
- all facts include provenance: message id, command, path, hash, archive id

## Ledger schema

The ledger stores deterministic capsules:

- command capsule: command line, cwd, exit code, stdout/stderr hashes, archive
  ids, reducer mechanisms applied
- file capsule: path, normalized repo/workdir scope, read range, content hash, archive id,
  edit state, latest known full-pass turn
- failure capsule: tool, file, line, column, message, stack/test name, exit code
- search capsule: pattern hash, repo root, files matched, line ranges, omitted
  count, archive id for full output
- decision capsule: user-requested goal, explicit constraints, accepted plan,
  blocked reasons, current active files
- recovery capsule: archive ids, expansion hints, last recovery attempt, success
  or miss

## Technical work packages

1. [x] Create `internal/contextledger` with pure deterministic builders.
   - [x] command/file/search/failure capsules
   - [x] decision/recovery capsules
2. [x] Feed builders from existing reducer telemetry:
   - [x] Codex Layer-0 tool-result observations build command/file/search/failure
     capsules as telemetry
   - [x] `/admin/state.savings` exposes content-free capsule counts globally and
     per route
   - [x] readcache archive ids and full-pass turn provenance
   - [x] WSS Phase-F request summaries beyond Layer-0 stats
   - [x] quality/re-read canaries
3. [x] Build capsule selection:
   - [x] active turn: verbatim
   - [x] recent working set: verbatim or exact delta
   - [x] old inactive context: ledger capsules
   - [x] high-risk content: full-pass
4. [x] Build archive-backed expansion:
   - [x] every capsule referring to omitted content must carry archive ids
   - [x] expansion must restore exact source bytes
   - [x] missing archive means no replacement
   - [x] wire archive expansion replay into the A/B harness engine
   - [x] add real archived reducer-output fixtures to the A/B harness
   - [x] add OCRL archive-list replay to the A/B harness, including deleted
     block coverage and mismatch-fails-lost tests
   - [x] add allocation-light archive recoverability verification for OCRL
     apply gates without copying archive bytes
   - [x] add read-only archive peek for shadow/proof verification
5. [~] Replace summary replacement with ledger insertion only behind proof:
   - [x] classical summary replacement is blocked by default, even when Layer 2
     background work is enabled
   - [x] default-off while shadowing
   - [x] shadow produces ledger sidecar and compares against direct context
   - [x] quality-pressure, active-path, wrong-session, missing-fact, and
     missing-archive candidates fail closed before any future promotion
   - [x] implement the pure OCRL route/recovery/token gate engine
   - [x] keep Codex WSS shadow-only unless a future surface resends old full
     context and live proof supports promotion
   - [x] attach OCRL shadow results to WSS debug summaries without changing
     model-facing frames
   - [x] expose OCRL as `[compression.ocrl]`, env overrides, and
     `slimference layer2 status`
   - [x] implement exact full-history message/block apply with archive
     byte-match, explicit target mapping, selected-target-only token
     accounting, marker-overhead accounting, and full-pass gates
   - [x] add exact archive-to-message target derivation for full-history
     messages without guessing ambiguous or missing matches
   - [x] require real non-synthetic OCRL full-history evidence in the global
     `benchmark-corpus --maxx-check` promotion gate
   - [ ] promotion only after live corpus proof
6. [x] Keep provider summarizers outside default:
   - opt-in only
   - labelled in docs and admin state
   - never needed for product default savings claims

## Zero product-drawdown gates

- The ledger cannot replace active files, active failures, active user
  instructions, active patches, or recent tool outputs.
- If the quality/re-read canary or another product-quality signal reports
  pressure, all ledger candidates must full-pass for that session.
- A capsule cannot stand in for raw details unless the raw details are
  recoverable through archive.
- If capsule provenance is missing, full-pass.
- If the selection policy lacks a current session id, full-pass.
- If a file or search capsule lacks an explicit execution scope, full-pass.
- If archive expansion fails, full-pass and disable the mechanism for the
  session.
- If the request route is not model-facing old-context eligible, OCRL stays
  shadow-only.
- If token accounting is missing, invalid, or net-negative after capsule and
  recovery overhead, full-pass.
- No LLM-produced summary can be default-on.

## Savings targets

- Long HTTP/full-history sessions: measurable billable-input reduction after
  active working set stabilizes.
- Codex WSS: no model-facing replacement claim until the ledger has a meaningful
  WSS insertion point that does not fight `previous_response_id` semantics. The
  current hot-path wiring is telemetry-only and cannot change model behavior.
- Net win must include ledger overhead and archive-recovery note overhead.
- OCRL benchmark target: large capsule batches must stay cheap enough for
  offline/proof and non-hotpath operation. Current local Apple M1 measurement:
  512 file capsules render in about 0.709 ms, 238109 B/op, 11 allocs/op;
  exact archive-to-message target derivation runs in about 0.406 ms, 190034 B/op,
  946 allocs/op; full archive-match OCRL apply runs in about 2.289 ms,
  1183727 B/op, 3860 allocs/op.

## Verification

- Pure unit tests for every capsule builder.
- Golden fixtures for commands, reads, search, tests, failures, edits.
- A/B context replay: direct vs ledger, with raw archive expansion proving no
  unrecoverable fact loss.
- Unit gate: `go test ./internal/contextledger -count=1`
- A/B harness gate: `go test ./internal/abharness ./internal/contextledger -count=1`
- Proxy telemetry gate:
  `go test ./internal/proxy -run 'TestApplyProxyLayer0Ledger|TestProxyLayer0Ledger|TestApplyProxyLayer0Branches' -count=1`
- Benchmark gate:
  `go test ./internal/contextledger -bench='Benchmark(BuildOCRLReplacement|DeriveOCRLMessageTargets|ApplyOCRLToMessagesByArchiveMatch)' -benchmem -run '^$'`
- Corpus gate:
  `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --check`
  includes the synthetic `ocrl_full_history` validator fixture. This proves
  gate wiring, not live promotion.
- Maxx promotion gate:
  `go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --maxx-check`
  now requires a real non-synthetic `ocrl_full_history` workload and fails if
  OCRL is not applied on a full-history route with archive expansions, positive
  OCRL saved tokens, and no shadow-only OCRL rows. This is expected to remain
  open until a real full-history OCRL capture exists.
- Live corpus gate:
  - CLI and Desktop
  - no repair/re-read spike
  - no model-facing unresolved archive ids
  - no degraded sessions

## Done

Layer 2 is product-ready only when it is a deterministic context ledger with
archive-backed recovery and proof that it preserves task decisions. Classical
summary remains opt-in, not default.

## Progress

- 2026-05-31: Added telemetry-only `context_ledger` summaries to WSS
  `RequestSummary` records. The summary carries deterministic command/file/
  search/failure capsule counts plus the re-read canary count, so live
  decisions logs can prove ledger coverage and quality pressure without
  inserting capsules into model-facing context.
- 2026-05-31: Added fail-closed capsule selection and archive expansion
  planning in `internal/contextledger`. The selector keeps active/recent turns,
  high-risk failures, missing provenance, missing archives, wrong sessions, and
  over-budget capsules out of any future replacement path. Archive expansion is
  loader-based and returns copied exact bytes or an error, so missing archive
  state cannot silently become model context.
- 2026-05-31: Readcache decisions now carry structured `ArchiveURI` and
  `FullPassTurnID` provenance. The Layer-0 reducer feeds that into file ledger
  observations, so file capsules only count when the readcache has an actual
  archive-backed source and a turn provenance path instead of relying on marker
  text parsing.
- 2026-05-31: The offline A/B harness now supports archive expansion replay.
  Referenced elisions remain safe only if the resolver expands the archive id to
  exact original bytes or the bytes were already sent verbatim earlier. Missing
  or mismatched archive expansions are counted as lost comprehension issues.
- 2026-05-31: Added an offline WSS replay fixture where the real Codex reducer
  emits a changed-read delta with an archive handle. The A/B harness must expand
  that reducer-created archive entry to the exact changed read bytes, otherwise
  the replay is counted as lost.
- 2026-06-01: Added an explicit legacy gate for model-facing Layer 2 summary
  replacement. Cached summaries now stay shadow/background-only unless
  `[compression.summary].allow_model_facing_replacement=true` or
  `SLIMFERENCE_L2_ALLOW_MODEL_FACING_REPLACEMENT=1` is set. This prevents
  classical summary-as-truth from being accidentally promoted while the ledger
  insertion path remains live-proof gated.
- 2026-06-05: Extended the same model-facing legacy gate to mid-exchange
  summaries. `ApplyMidExchange` now returns full-pass unless
  `allow_model_facing_replacement=true`, so enabling the old T99 tuning knob
  cannot insert any summary-as-context path without the same explicit override
  that protects cached Layer 2 replacement.
- 2026-06-01: Aligned the cross-layer planner with the same safety gate. Long
  contexts now produce `context_ledger_shadow_summary_replacement_blocked`
  instead of planner `run` unless model-facing legacy summary replacement is
  explicitly allowed.
- 2026-06-02: Hardened context-ledger promotion safety. Capsule selection now
  keeps command/file/search/failure capsules verbatim when their required
  deterministic facts are missing, even if an archive id exists. File capsules
  with archive provenance also require `full_pass_turn`, so telemetry cannot
  count archive-backed file context unless the exact prior full-read turn is
  known.
- 2026-06-02: Hardened the selector against missing caller session scope.
  `SelectCapsules` now keeps all capsules verbatim when `SelectionPolicy`
  lacks a session id, so a future model-facing insertion path cannot silently
  select archive-backed context across session namespaces.
- 2026-06-04: Hardened the remaining legacy summary replacement function. Even
  with the explicit `allow_model_facing_replacement` override, Layer 2 now
  refuses model-facing replacement when the caller lacks a trusted session id,
  falls back to `empty` / `fh:*` content-hash IDs, has empty cached summary
  text, or lacks positive cached token savings. This keeps the legacy path
  fail-closed and prevents cross-conversation or useless summary-as-truth
  rewrites from ever counting as product work. The old sessionless
  `ApplyToMessages` wrapper is therefore full-pass only; callers must use the
  session-keyed API before any replacement can be considered.
- 2026-06-04: Added two fail-closed selector inputs for future ledger promotion.
  `SelectionPolicy.ActivePaths` keeps file, search, and decision capsules
  verbatim when they touch an actively worked file, even if the capsule is old
  and archive-backed. Search capsule matching is repo-root aware, so a
  repo-relative match such as `a.go` is protected when the active path is
  `/repo/a.go`. `SelectionPolicy.QualityPressure` keeps every capsule verbatim
  when quality/re-read/recovery canaries report pressure. This turns the "no
  active-file or comprehension-pressure replacement" rule into tested code, not
  only operator policy.
- 2026-06-04: Verified the selector hardening with the focused race gate:
  `go test -race ./internal/contextledger ./internal/filter ./internal/readcache
  ./internal/chunkdedup ./internal/toolprune ./internal/outputreduce
  ./internal/proxy/wsmitm ./internal/quality ./internal/hostmetrics -count=1`
  passed. This does not promote Layer 2 into model-facing context; it proves the
  fail-closed safety primitives stay concurrency-clean across the critical
  savings packages.
- 2026-06-05: Hardened the remaining background summariser input path for huge
  Unicode/CJK histories. Layer 2 now caps oversized message text before outbound
  redaction/rendering, caps the formatted summariser body again, keeps both caps
  UTF-8 valid, respects the same CJK-heavy token heuristic used by
  `estimateTokens`, and does not mutate the original message slice used for
  hashes, anchors, and covered range validation. The focused package race gate
  `go test -race ./internal/summarization -count=1` and full repository race
  gate `go test -race ./...` both passed after the fix. This is still
  background-only hardening; it does not promote classical summaries into the
  model-facing product path.
- 2026-06-03: Hardened search capsule scope. `BuildSearchCapsule` now requires
  an explicit repo/workdir scope and `SelectCapsules` requires the `repo_root`
  fact before any search capsule can become promotable old context. The Codex
  reducer only counts search ledger telemetry when the tool call carries an
  explicit scope, either via tool `workdir`/`cwd` metadata or a repo-scoped
  command such as `cd /repo && rg ...`, an absolute search path, or
  `git -C /repo grep ...`. Implicit-cwd searches still do not become
  cross-repo context.
- 2026-06-03: Hardened file capsule scope the same way. Archived file capsules
  now require `repo_root` plus full-pass-turn provenance, and the Codex reducer
  only counts file ledger telemetry when the read came from a tool call with an
  explicit workdir. Absolute paths without session workdir still keep normal
  read-delta behavior, but they cannot become promotable ledger context.
- 2026-06-03: Added deterministic decision and recovery capsule primitives.
  Decision capsules preserve explicit goals, constraints, accepted plans,
  blocked reasons, active files, and optional archive provenance without
  paraphrasing. Recovery capsules require archive ids plus an attempt status.
  `SelectCapsules` now recognizes both kinds but still fails closed on missing
  facts or missing archives, so they are safe building blocks for future ledger
  insertion without changing model-facing context today.
- 2026-06-05: Closed the remaining mid-exchange model-facing gap. The
  Layer2-owned `ApplyMidExchange` method now full-passes unless
  `allow_model_facing_replacement=true`, matching cached summary replacement.
  The pure deterministic mid-exchange helper remains available for tests and
  legacy labs, but the product proxy cannot insert any summary-as-context path
  by only enabling `mid_exchange_enabled`.
- 2026-06-05: Closed the overflow-recovery model-facing gap. The aggressive
  context-overflow retry can consume an existing cached summary only when
  `layer2_enabled=true` and the Layer2-owned model-facing legacy gate accepts
  the session/cache. A focused regression test proves
  `allow_model_facing_replacement=true` alone cannot inject cached summary text
  while Layer 2 is disabled. The async background enqueue path already required
  Layer 2 to be enabled, so recovery now matches the normal request hot path.
- 2026-06-05: Extended scoped search ledger telemetry without widening product
  risk. The Layer-0 reducer now derives search capsule scope from already
  normalized repo-scoped commands (`cd /repo && rg ...`, absolute search paths,
  or `git -C /repo grep ...`) as well as tool `workdir` metadata. The existing
  implicit-cwd guard remains: unscoped `rg ...` still produces no search
  capsule. Focused proxy/filter tests cover both sides.
- 2026-06-05: Added `docs/ocrl.md` and the pure OCRL engine in
  `internal/contextledger/ocrl.go`. OCRL now has concrete modes, route gates,
  archive-recoverability verification, deterministic machine-readable rendering,
  and positive net-savings accounting. Codex WSS is explicitly shadow-only.
  The Layer-0 proxy stats path now carries real capsule objects in addition to
  counters, with regression tests proving command/file/search/failure object
  coverage and provenance. Focused verification passed:
  `go test ./internal/contextledger -count=1`,
  `go test ./internal/proxy -run 'TestApplyProxyLayer0Ledger|TestProxyLayer0Ledger|TestApplyProxyLayer0Branches' -count=1`,
  and `go test ./internal/contextledger -bench=BenchmarkBuildOCRLReplacement -benchmem -run '^$'`
  measured about 0.875 ms/op for 512 capsules on Apple M1 after the latest
  shadow-telemetry pass.
- 2026-06-05: Wired OCRL into WSS Phase-F debug summaries as shadow proof.
  `internal/proxy/ocrl_shadow.go` builds a content-free OCRL result from actual
  Layer-0 ledger capsules, uses archive-payload token counting only for
  would-save telemetry, keeps Codex WSS at `route_not_eligible`, and records no
  product `net_tokens`. `internal/contentarchive.Peek` verifies exact archive
  payloads without incrementing expansion telemetry. Focused verification
  passed:
  `go test ./internal/contentarchive ./internal/contextledger ./internal/debug ./internal/proxy -run 'TestPeekDoesNotRecordExpansion|TestBuildOCRL|TestRenderOCRL|TestBuildMechanismAccounting|TestBuildOCRLShadow|TestWSRecordRequestPlanIncludesOCRLShadowTelemetry' -count=1`.
  `go test ./internal/contextledger -bench=BenchmarkBuildOCRLReplacement -benchmem -run '^$'`
  measured `874991 ns/op`, `413990 B/op`, and `8202 allocs/op`.
- 2026-06-05: Exposed OCRL policy as a real product configuration surface.
  `internal/config` now validates `[compression.ocrl]` with env overrides,
  `internal/proxy/ocrl_shadow.go` consumes that policy instead of hard-coded
  max-mode values, and `slimference layer2 status` shows OCRL mode/budget plus
  the Codex WSS shadow-only route guard. Tests cover defaults, env overrides,
  invalid values, default shadow telemetry, explicit max route blocking, and
  off-mode no-savings behavior. Verification passed with focused OCRL tests,
  affected package tests, `go test ./... -count=1`, benchmark
  `BenchmarkBuildOCRLReplacement` at `1023157 ns/op`, `414016 B/op`,
  `8202 allocs/op`, and `go run ./scripts/ci` all 8 steps green with 97.0%
  total coverage.
- 2026-06-05: Hardened the pure OCRL engine against caller formatting drift.
  `BuildOCRLReplacement` now normalizes mode and route strings for whitespace
  and case before making apply/shadow/off decisions, with a unit test proving
  mixed-case `AUTO` and `FULL_HISTORY_HTTP` still hit the intended guarded
  full-history apply path. `go test ./internal/contextledger -count=1` and
  `go test ./... -count=1` passed.
- 2026-06-05: Added exact full-history OCRL message application in
  `internal/contextledger/message_apply.go`. `ApplyOCRLToMessages` takes
  explicit message/block targets, proves every target against a single
  byte-equal archive payload, reruns the OCRL selector and route gates, clears
  raw/archive metadata on rewritten blocks, inserts compact `covered_by`
  markers only when a whole single-block message would otherwise disappear, and
  counts final net savings only over selected targets plus marker overhead.
  Regression tests cover positive application, multi-block deletion,
  archive-mismatch full-pass, shadow/Codex-WSS route gates, marker-overhead
  rejection, selected-only token accounting, and duplicate-target rejection.
  `go test ./internal/contextledger -count=1` passed.
  `go test ./internal/contextledger -bench=BenchmarkBuildOCRLReplacement -benchmem -run '^$'`
  measured `2494166 ns/op`, `414023 B/op`, and `8202 allocs/op`.
- 2026-06-05: Extended the offline A/B harness for OCRL proof replay.
  `internal/abharness` now extracts archive ids from OCRL `archives=[...]`
  render lines and, when a compressed turn deletes a following old block under
  an OCRL replacement, checks the whole compressed turn's OCRL archive set
  before classifying the direct block as lost. Tests prove replaced and deleted
  old blocks are recoverable when archives expand byte-equal, and mismatched
  OCRL archive payloads remain `reference_mismatch` lost-comprehension issues.
  `go test ./internal/abharness ./internal/contextledger -count=1` passed.
- 2026-06-05: Reduced OCRL renderer and archive-verify hot allocations without
  weakening gates. `RenderOCRLCapsules` now writes quoted fields, archive lists,
  and maps directly into one builder with reusable scratch buffers, while
  singleton archive verification avoids per-capsule sort/map allocation.
  `go test ./internal/contextledger -bench='Benchmark(BuildOCRLReplacement|DeriveOCRLMessageTargets|ApplyOCRLToMessagesByArchiveMatch)' -benchmem -run '^$'`
  measured `709099 ns/op`, `238109 B/op`, and `11 allocs/op` for render-only.
- 2026-06-05: Added safe full-history OCRL target derivation.
  `ApplyOCRLToMessagesByArchiveMatch` and `DeriveOCRLMessageTargets` load each
  capsule's single archive payload and derive a message/block target only when
  that payload is byte-equal to exactly one current message block. Ambiguous
  matches, missing archives, archive read errors, unmatched payloads, and
  duplicate target positions are omitted and counted in the derivation report,
  so a future route can use model-facing OCRL only with proven target mapping.
  The apply path now maps selected targets from the selector decision order
  instead of rendering per-capsule keys. Current Apple M1 benchmarks measured
  `405735 ns/op`, `190034 B/op`, and `946 allocs/op` for target derivation, and
  `2289053 ns/op`, `1183727 B/op`, and `3860 allocs/op` for full archive-match
  OCRL apply over 512 capsules.
  `go test ./internal/contextledger -count=1` passed.
- 2026-06-05: Added corpus-level OCRL proof gating. `benchmark-corpus` now
  aggregates OCRL context-ledger fields and the `ocrl_full_history` validator
  requires applied full-history evidence, archive expansions, positive OCRL
  savings, and no shadow-only rows. `synthetic_ocrl_full_history` keeps this
  gate covered in CI without pretending to be real CLI/Desktop promotion
  evidence.
- 2026-06-05: Promoted OCRL into the strict max-out evidence gate without
  promoting the product behavior. `benchmark-corpus --maxx-check` now requires
  a real non-synthetic `ocrl_full_history` workload and independently verifies
  applied OCRL, full-history route rows, candidate capsules, archive expansions,
  positive OCRL saved tokens, and zero shadow-only rows. The committed
  synthetic OCRL fixture still proves only gate wiring; the max-out gate now
  remains failed until real model-facing OCRL live proof exists.
- 2026-06-05: Wired OCRL full-history into the release-proof operator runbook.
  `verify -mode release-proof-plan` now emits CLI/Desktop live-corpus plan
  commands for `ocrl_full_history`, and `verify -mode live-corpus-plan
  -category ocrl_full_history` renders metadata with the `ocrl_full_history`
  validator plus positive saved-token evidence instead of generic low-error
  metadata. This closes the offline runbook gap while keeping the real live
  proof itself as the remaining promotion blocker.
- 2026-06-05: Reconciled live-corpus documentation with the actual max-out
  gate. The metadata example now lists OCRL and all current maxx workload
  classes, and the supported validator list includes `output_reduce_ab` so the
  documented corpus contract matches the benchmark validator implementation.
- 2026-06-05: Added a regression test that reads the live-corpus docs and
  fails if any benchmark-supported scenario validator or promotion/maxx workload
  is omitted. This closes the validator-list drift class that hid
  `host_budget_ok` and `output_reduce_ab` from the operator-facing contract.
- 2026-06-05: Hardened explicit full-history OCRL message targets to normalize
  their single archive id through the same sorted/trimmed archive-id rule used
  by target derivation, rendering, and archive verification. Added tests proving
  trimmed single ids apply and multi-id targets still fail closed.
