# TASK 253: Codex aggressive read compression (AST scan-mode, predictive post-edit, reasoning, patch-context dedup)

Status: [ ] QUEUED - gated by capture matrix, shadow replay, and auto-policy proof
Priority: P2 - high savings potential, highest drawdown risk, must be proof-gated
Scope: Codex-only WSS Phase-F. Compress reads more aggressively where it is provably
safe and recoverable.

## Why

Today the first read of a file passes full, and re-reads are only deduplicated. There
is more to take, but each item is lossy on first sight and therefore only safe behind
the t249 safety net (A/B harness + recoverable archive):

- When the model is exploring (scan/browse, not editing), it rarely needs full bodies;
  signatures + structure suffice. `internal/codecompact/api.go` already does AST-based
  compaction for Go (`renderGoSignature`, `goBodyLines`, `shouldIncludeGoBody`,
  `ExtractGoSymbolBody`); the file-read filter already uses it for large Go `cat`.
  Extending it to first-read scan-mode and more languages compresses large reads
  50-70% with recovery.
- The proxy SEES `apply_patch` (it parses the patch in `proxyLayer0EditPaths`), so it
  knows the new file content. A read immediately after a patch can be served as the
  known patched result instead of a full re-read.
- Codex Responses may carry reasoning items and apply_patch surrounding context that
  the server already holds; some of it is redundant.

## Acceptance

ALL sub-tasks start in shadow/proof mode. Promotion into `codex_savings_policy_mode=auto`
requires the t249 A/B harness to show no comprehension regression for the relevant
case, the recoverable-archive note to be live on the route, and the T258 policy matrix
to classify the workload as safe for that mechanism.

- First-read AST/structure scan-mode compression: large code reads in scan/browse
  intent return signatures + structure, recoverable via `local-archive://`,
  intent-gated (scan vs edit; edit/RecentlyEdited always passes full). Go via
  `codecompact`; extend to at least one more language.
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
- [ ] First-read AST/structure scan-mode compression with intent gating + archive
      recovery; extend `codecompact` beyond Go for one more language; fixtures.
- [ ] Predictive post-edit file state from the parsed `apply_patch`; fail-open on
      ambiguity; fixtures.
- [ ] apply_patch context dedup against known content.
- [ ] Stale reasoning compaction (only if verified present in input).

## Target Metrics

- First-read scan-mode: 20-50% input-token reduction on exploration-heavy code-read
  captures while preserving exact edit/debug behavior.
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

- First-read scan-mode must distinguish scan/browse intent from edit/debug intent via
  parsed command shape, recent-edit state, and model-issued tool purpose. Edit/debug,
  recently edited files, small files, and unknown commands full-pass.
- Scan-mode output must be structured, neutral, archive-backed, and stable under
  replay. Go uses `internal/codecompact`; the second language must reuse an existing
  parser or a deterministic lightweight extractor with tests.
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

- % impact: first-read scan compression ~20-50% on exploration-heavy sessions, but
  highest drawdown -> strictly gated. Predictive post-edit ~5-15%. Reasoning ~0-15%
  (verify first). Patch-context dedup ~3-10%.
- Dependencies: HARD dependency on t249 (A/B harness + recoverable archive). Do not
  ship any of this default-on without the comprehension proof.
- Doctrine: content-free, fail-open, scoped; every aggressive transform must be
  recoverable so a loss is never permanent.

## Deviations

(none)
