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
2. [ ] Feed builders from existing reducer telemetry:
   - Layer 0 filter decisions
   - readcache observations
   - WSS Phase-F request summaries
   - quality/re-read canaries
   - archive ids
3. [ ] Build capsule selection:
   - active turn: verbatim
   - recent working set: verbatim or exact delta
   - old inactive context: ledger capsules
   - high-risk content: full-pass
4. [~] Build archive-backed expansion:
   - every capsule referring to omitted content must carry archive ids
   - expansion must restore exact source bytes
   - missing archive means no replacement
5. [ ] Replace summary replacement with ledger insertion only behind proof:
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
- Codex WSS: no default claim until the ledger has a meaningful WSS insertion
  point that does not fight `previous_response_id` semantics.
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
