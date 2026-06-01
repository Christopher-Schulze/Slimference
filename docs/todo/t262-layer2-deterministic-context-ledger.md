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
  command, file, search, and failure observations. Capsules store compact facts,
  provenance, stable hashes, and archive ids, never raw omitted content.
- The Codex Layer-0 reducer now feeds those builders in the hot path as
  content-free telemetry only. `/admin/state.savings` exposes command, file,
  search, and failure capsule counts globally and per route. No capsule is
  inserted into model-facing context yet.
- Classical summary replacement is now separately gated. `layer2_enabled=true`
  can no longer make cached summaries replace model-facing history unless the
  explicit legacy override
  `[compression.summary].allow_model_facing_replacement=true` is also set.

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
- file capsule: path, normalized repo root, read range, content hash, archive id,
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
   - default-off while shadowing
   - shadow produces ledger sidecar and compares against direct context
   - promotion only after live corpus proof
6. [x] Keep provider summarizers outside default:
   - opt-in only
   - labelled in docs and admin state
   - never needed for product default savings claims

## Zero product-drawdown gates

- The ledger cannot replace active files, active failures, active user
  instructions, active patches, or recent tool outputs.
- A capsule cannot stand in for raw details unless the raw details are
  recoverable through archive.
- If capsule provenance is missing, full-pass.
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
