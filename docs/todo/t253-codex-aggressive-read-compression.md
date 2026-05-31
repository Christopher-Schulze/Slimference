# TASK 253: Codex aggressive read compression (default-safe only)

Status: [~] IN PROGRESS - first-read scan-mode RETIRED from Codex runtime; default-safe
lossless/recoverable reducers remain. Other sub-tasks (predictive post-edit, apply_patch
dedup, reasoning) stay queued only if they can become one automatic no-drawdown mode.
Priority: P2 - high savings potential, highest drawdown risk, must be proof-gated
Scope: Codex-only WSS Phase-F. Compress reads more aggressively where it is provably
safe and recoverable.

## Why

Today the first read of a file must pass full. Re-reads, ranged reads, repeated outputs,
chunk overlap, and search/build/test output are the correct savings surface. Aggressive
ideas remain valid only when they preserve that invariant:

- First-read scan-mode proved why this boundary exists: it saved tokens in probes, but
  it made the model see less file information on first sight and relied on recovery after
  the model noticed missing detail. That is not a product-default feature.
- The proxy SEES `apply_patch` (it parses the patch in `proxyLayer0EditPaths`), so it
  knows the new file content. A read immediately after a patch can be served as the
  known patched result instead of a full re-read.
- Codex Responses may carry reasoning items and apply_patch surrounding context that
  the server already holds; some of it is redundant.

## Acceptance

Promotion into the single default product mode requires the t249 A/B harness to show no
comprehension regression, a deterministic recovery path where content is elided, and the
T258 policy matrix to classify the workload as safe for that mechanism.

- First-read AST/structure scan-mode compression is retired from Codex runtime.
  First-read file outputs full-pass in all policy modes.
- Predictive post-edit file state: a read right after an `apply_patch` to the same
  path is served as the known patched result (proxy-synthesized), with archive
  recovery; fail-open if the patch could not be applied deterministically.
- Reasoning-trace compaction: FIRST verify whether reasoning is actually present in the
  client->server `input` (it may be server-side only via `previous_response_id` and
  already discounted). Only then compact stale reasoning.
- apply_patch context dedup against known content.
- Coverage gate green; doctrine clean; every transform recoverable.

## Sub-Tasks

- [ ] Verify (capture-driven, content-free) whether reasoning items appear in the
      c2s `input`; document the finding before building any reasoning compaction.
- [x] Remove first-read scan-mode from Codex runtime: no policy decision, no apply env,
      no live counters, no persisted scan-origin keys; first-read file outputs full-pass
      even in `max`.
- [ ] Replace any desired scan-like idea only with a default-safe cache-hit design:
      no first-read information weakening and no lossless-cache cannibalization.
- [ ] Predictive post-edit file state from the parsed `apply_patch`; fail-open on
      ambiguity; fixtures.
- [ ] apply_patch context dedup against known content.
- [ ] Stale reasoning compaction (only if verified present in input).

## Target Metrics

- First-read scan-mode: retired from runtime despite 20-50% probe savings because it
  weakens first-read information.
- Predictive post-edit: 5-15% reduction on patch/read cycles; zero wrong-file or
  stale-version substitutions.
- apply_patch context dedup: 3-10% reduction where patch context repeats known file
  content.
- Reasoning compaction: only measured if c2s reasoning items are actually present;
  otherwise close that sub-task as "not applicable on current Codex WSS wire".

## Promotion Gates

- Capture gate: at least 2 CLI and 2 Desktop captures for each mechanism's workload
  class, plus one long mixed workday capture.
- Replay gate: `wss-ab-replay --fail-on-lost --json` reports `gate_passed=true`,
  `parse_failures=0`, `degraded_sessions=0`, and no unexpected elisions.
- Comprehension gate: A/B harness shows no missing model-facing facts compared with
  direct/full context. Any ambiguity keeps the mechanism shadow-only.
- Recency gate: no post-collapse re-read canary spike after enabling the mechanism.
- Recovery gate: every model-facing elision has a `local-archive://` recovery handle
  and the recovery note is injected only when needed.
- Policy gate: T258 classifies the mechanism as auto-eligible only for workload
  classes where all above gates passed.

## Technical Design Requirements

- First-read file output must full-pass. Any future high-savings read design must be
  expressed as cache/delta after full context has already been sent.
- Predictive post-edit may synthesize a known post-patch file state only when the
  patch applies deterministically to the last known file bytes. Any mismatch, fuzzy
  hunk, missing base, binary file, or multi-file ambiguity full-passes.
- apply_patch context dedup may remove only context that is byte-identical to known
  file state already forwarded to the server. It must preserve new/removed lines and
  hunk metadata.
- Reasoning compaction cannot be implemented from speculation. First capture the
  actual Codex c2s input shape; if reasoning is server-side only through
  `previous_response_id`, document that and do not build dead code.

## Notes

- % impact: first-read scan compression was measured at ~20-50% on exploration-heavy
  sessions but retired due first-read information weakening. Predictive post-edit ~5-15%. Reasoning ~0-15%
  (verify first). Patch-context dedup ~3-10%.
- Dependencies: HARD dependency on t249 (A/B harness + recoverable archive). Do not
  ship any of this default-on without the comprehension proof.
- Doctrine: content-free, fail-open, scoped; every aggressive transform must be
  recoverable so a loss is never permanent.

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

## Deviations

(none)
