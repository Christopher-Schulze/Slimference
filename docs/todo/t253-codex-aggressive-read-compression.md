# TASK 253: Codex aggressive read compression (AST scan-mode, predictive post-edit, reasoning, patch-context dedup)

Status: [ ] QUEUED - GATED, default-off until t249 proves no comprehension regression
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

ALL sub-tasks are default-OFF and gated: enabled only after the t249 A/B harness shows
no comprehension regression for the relevant case AND the recoverable-archive note is
live.

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
