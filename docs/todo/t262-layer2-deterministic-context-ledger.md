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
- The Codex Layer-0 reducer now feeds those builders in the hot path as
  content-free telemetry only for tool-output command/file/search/failure
  observations. `/admin/state.savings` exposes those capsule counts globally and
  per route. Decision/recovery capsules are pure primitives only until a real
  provenance source is wired. No capsule is inserted into model-facing context
  yet.
- Classical summary replacement is now separately gated. `layer2_enabled=true`
  can no longer make cached summaries replace model-facing history unless the
  explicit legacy override
  `[compression.summary].allow_model_facing_replacement=true` is also set.
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
5. [~] Replace summary replacement with ledger insertion only behind proof:
   - [x] classical summary replacement is blocked by default, even when Layer 2
     background work is enabled
   - [x] default-off while shadowing
   - [x] shadow produces ledger sidecar and compares against direct context
   - [x] quality-pressure, active-path, wrong-session, missing-fact, and
     missing-archive candidates fail closed before any future promotion
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
- No LLM-produced summary can be default-on.

## Savings targets

- Long HTTP/full-history sessions: measurable billable-input reduction after
  active working set stabilizes.
- Codex WSS: no model-facing replacement claim until the ledger has a meaningful
  WSS insertion point that does not fight `previous_response_id` semantics. The
  current hot-path wiring is telemetry-only and cannot change model behavior.
- Net win must include ledger overhead and archive-recovery note overhead.

## Verification

- Pure unit tests for every capsule builder.
- Golden fixtures for commands, reads, search, tests, failures, edits.
- A/B context replay: direct vs ledger, with raw archive expansion proving no
  unrecoverable fact loss.
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
  reducer only counts search ledger telemetry when the tool call carries a
  scoped workdir, so implicit-cwd searches cannot later become cross-repo
  context.
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
