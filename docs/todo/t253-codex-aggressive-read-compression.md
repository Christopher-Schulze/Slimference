# TASK 253: Codex aggressive read compression (default-safe only)

Status: [x] CLOSED - unsafe/reconstructive candidates retired from the product roadmap.
Default-safe lossless/recoverable reducers remain in T248/T250/T255/T256.
Priority: P2 - high savings potential, highest drawdown risk, must be proof-gated
Scope: Codex-only WSS Phase-F. Compress reads more aggressively where it is provably
safe and recoverable.

## Why

Today the first read of a file must pass full. Re-reads, ranged reads, repeated outputs,
chunk overlap, and search/build/test output are the correct savings surface. T253
was the audit bucket for aggressive read-context ideas. The final decision is:
do not keep features that only make sense as manual experiments or lab toggles.

- First-read scan-mode proved why this boundary exists: it saved tokens in probes, but
  it made the model see less file information on first sight and relied on recovery after
  the model noticed missing detail. That is not a product-default feature.
- Predictive post-edit synthesis is not a product-default feature: the first read after
  an edit is exactly where the model needs fresh, salient, byte-real context. Existing
  recent-edit guards full-pass that read; later repeat reads can use the proven cache.
- `apply_patch` context dedup is not product-default: patch context is part of the
  model's working memory of what changed. A few percent of possible savings is not worth
  patch-reasoning drift.
- Reasoning compaction is closed unless a future Codex wire contract exposes a
  deterministic, input-side, non-cognitive repeated payload. Current code only proves
  response-side reasoning frame kinds, not a safe c2s savings surface.

## Acceptance

- First-read AST/structure scan-mode compression is retired from Codex runtime.
  First-read file outputs full-pass in all policy modes.
- Predictive post-edit file-state synthesis is explicitly not a product-default target.
  Reads after edits full-pass first, then participate in normal repeat/ranged caching.
- `apply_patch` context dedup is explicitly not a product-default target unless it later
  reappears as exact repeated-output dedup through existing mechanisms.
- Reasoning-trace compaction is explicitly not a product-default target without a new
  wire contract and proof that it does not touch model cognition.
- The remaining savings direction is cache-hit improvement only: stronger command/range
  normalization, deterministic repeated-output keys, search/repeated command keys, and
  server-state mirror shadow signals that feed safe reducers without model-facing
  reconstruction.

## Sub-Tasks

- [x] Close reasoning compaction as not product-default: response-side reasoning frame
      kinds exist, but there is no proven safe c2s input savings surface and no
      acceptable cognition-risk budget.
- [x] Remove first-read scan-mode from Codex runtime: no policy decision, no apply env,
      no live counters, no persisted scan-origin keys; first-read file outputs full-pass
      even in `max`.
- [x] Replace any desired scan-like idea only with a default-safe cache-hit design:
      no first-read information weakening and no lossless-cache cannibalization.
- [x] Close predictive post-edit synthesis: first post-edit read remains full context;
      subsequent repeats are handled by normal read/ranged cache.
- [x] Close `apply_patch` context dedup as a standalone roadmap item; exact repeated
      outputs remain covered by existing repeated-output/chunk mechanisms.
- [x] Close stale reasoning compaction.

## Target Metrics

- First-read scan-mode: retired from runtime despite 20-50% probe savings because it
  weakens first-read information.
- Predictive post-edit synthesis: rejected for default-auto. Target savings 5-15% on
  patch/read cycles is too narrow for the recency/context risk. Safe replacement:
  first post-edit read full-passes, later repeats dedup normally.
- apply_patch context dedup: rejected as standalone default-auto work. Target savings
  3-10% is not worth patch-memory risk. Safe replacement: exact repeated tool outputs
  dedup through existing mechanisms.
- Reasoning compaction: rejected for default-auto. Any useful future path needs a new,
  deterministic input-side wire proof; do not keep dead code or speculative tasks.

## Promotion Gates

- No manual/lab-only promotion gate remains for the rejected mechanisms. A feature that
  cannot become part of the single automatic default product mode is closed, not kept
  as an operator toggle.
- Cache-hit improvements still use the existing T249/T257/T258 gates: captured
  workloads, replay with no lost context, no parse/degrade/compression errors, no
  post-collapse re-read spike, and positive billable-input savings.

## Technical Design Requirements

- First-read file output must full-pass. Any future high-savings read design must be
  expressed as cache/delta after full context has already been sent.
- Reads after edits full-pass at least once. The safe savings point is the next
  repeated read, not the first post-edit recency refresh.
- Do not mutate `apply_patch` tool-call context for savings. Preserve patch semantics
  and the model's memory of what changed.
- Do not mutate reasoning content for savings. Reasoning is model cognition surface,
  not a compression target.

## Notes

- % impact: first-read scan compression was measured at ~20-50% on exploration-heavy
  sessions but retired due first-read information weakening. Predictive post-edit
  (~5-15%), reasoning (~0-15%), and patch-context dedup (~3-10%) are closed because
  the drawdown risk is on the model's fresh context/cognition/patch memory surface.
- Doctrine: one automatic default product mode. No manual experiment toggles for
  features that cannot satisfy the no-drawdown bar.

## Progress (2026-05-31) - first-read scan retired

Earlier commits built and proved a first-read scan prototype, including shadow
measurement, apply wiring, recovery notes, persisted scan-origin keys, and re-read
frequency counters. The prototype saved tokens in narrow probes, but it violated the
product rule: a first file read must not give the model less file information and hope
recovery catches up later.

Current runtime state:

- No `ScanRead` policy decision exists.
- No `SLIMFERENCE_SCAN_APPLY` or `SLIMFERENCE_SCAN_SHADOW` product path exists.
- No `scan_read` mechanism exists in `reduceCodexLayer0`.
- No scan-origin key persistence or scan-read counters exist in `/admin/state`.
- The `strip_comments_file_read` runtime filter/helper was removed; read parsing remains
  for readcache/ranged-read detection.
- `TestReduceCodexLayer0NeverElidesFirstRead` proves first-read file output full-passes
  in `auto`, `conservative`, and `max`.
- `TestLayer0PipelineDoesNotElideFirstFileRead` proves captured file-read output also
  full-passes through the default filter pipeline.

Default savings now focus on deterministic cache hits: read-delta, ranged read-delta,
repeated output, chunk overlap, search grouping/search delta, and build/test/git filters.

Search-output compaction DONE (`1a2d478`) - the better, conflict-free lever. Root cause
(verified from the capture): `groupSearchResults` abandoned the whole output on the first
colon-less line, and Codex's truncated exec output always has one (cut-off tail + a leading
`Total output lines: N` header), so search grouping NEVER fired on real Codex searches. Fix:
skip colon-less noise lines, bail only when nothing parses or noise dominates
(`skipped*2 > nonEmpty`). On the real captured `rg` (402 matches, 79 files) this compacts
40 KB -> ~9 KB (78%). Default-auto in the filter pipeline; low-risk (match count +
which files kept, re-run the search to recover dropped matches); a search-output reducer, so
NO first-read-seeding conflict. Also shrinks requests, mitigating the upstream oversized-
request 400.

OPEN:
- Improve cache hit-rate without first-read elision: stronger range normalization,
  repeated deterministic command output keys, and server-state mirror shadowing.
- Fixed: the `Total output lines: N` envelope-header line is stripped as Codex metadata, not
  grouped as a fake search file.

## Closure decision (2026-05-31)

T253 is closed as a product-scope cleanup, not because the ideas were impossible to
prototype, but because they do not meet the user's operating rule: Slimference must have
one automatic default mode that is useful every day, not lab switches. The surviving
engineering work belongs to default-safe cache-hit improvements in T248/T250/T254/T256.
Nothing in this closure removes or weakens the proven reducers.

## Deviations

(none)
