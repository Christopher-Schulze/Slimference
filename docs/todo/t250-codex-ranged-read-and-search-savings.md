# TASK 250: Codex lossless cross-turn savings coverage (ranged reads + search-output delta)

Status: [x] DONE - ranged read-delta and search-output delta landed
Priority: P1 - lossless, low-risk, raises the savings floor on non-repeat-read sessions
Scope: Codex-only WSS Phase-F. Extend lossless cross-turn savings to the still
uncovered ranged-read and repeated-search-output classes.

## Why

The headline read-delta only covers FULL-file reads: `EvaluateObserved` bails unless
`req.IsFullFileRead()`, which requires `Offset==0 && Limit==0`
(`internal/readcache/types.go:11`, `internal/readcache/evaluate.go:62`). Codex reads
large files via ranges all the time (`sed -n`, `head`, `tail`, `rg -C`, offset reads),
and re-greps the same patterns across turns. None of that is currently deduplicated, so
a session that browses big files in slices or repeats searches gets near-zero savings.

Exact archive-backed cross-turn dedup for repeated NON-file tool outputs already landed
in T248; this task extends the same lossless approach to the ranged-read and
search-output classes that remain uncovered. These classes fire even on sessions
WITHOUT full-file repeat reads, so they raise the savings floor where it is lowest today.

## Acceptance

- Ranged/partial reads are cached keyed by `(sessionID, path, offset, limit)`. An
  identical ranged re-read collapses to an unchanged reference (recoverable via
  archive); a changed one collapses to a position-aware delta. Distinct ranges of the
  same file never collide.
- Repeated identical or near-identical search outputs (`rg`/`grep`/`git grep`) of the
  same query collapse to a reference or delta across turns.
- All transforms exact/lossless or position-aware. The existing `len`-guard and
  `afterTokens < beforeTokens` token-guard are preserved (never net-negative).
- Fixtures use real Codex exec command shapes (string-encoded args, `bash -lc`
  wrappers, MCP-style results); route attribution (WSS Phase-F) is correct.
- Coverage gate stays green; doctrine clean.

## Sub-Tasks

- [x] Extend `readcache.Request` + `IsFullFileRead` handling to support ranged reads;
      key on `(path, offset, limit)`. Extend command-line extraction
      (`filter.ReadPathFromCommandLine` and the offset/limit derivation) to cover
      `sed -n`, `head -n`, `tail -n`, and offset/limit read tools.
- [x] Add cross-turn search-output reference/delta for repeated identical queries
      (reuse the content-archive + session-scoped store from the T248 non-file dedup).
- [x] Fixtures proving ranged-read and search re-read mutation; verify per-route
      attribution and that distinct ranges do not collide.

## Notes

- % impact: ranged reads ~5-15%, search-output ~3-8% additional billable reduction,
  lossless, low risk. Both fire without full-file repeat reads, so they lift the
  weakest sessions.
- 2026-05-30: recognized ranged reads (`head`, `tail`, `sed -n`) are keyed by
  `path+offset+limit`; distinct ranges do not collide. A first recognized read that
  misses read-delta now full-passes instead of falling through to generic log/filter
  compaction, which preserves first-read context and avoids permanent loss.
- 2026-05-30: repeated search outputs (`rg`, `grep`, `git grep`) now get exact
  unchanged references or position-aware deltas through the archive-backed output
  cache. The WSS Phase-F fixture proves a changed repeated `rg` output mutates via
  search-output delta with positive savings.
- Dependencies: none hard. Complements the landed exact non-file dedup; near-dup and
  partial-overlap of these classes is later subsumed by t255 chunk dedup.
- Doctrine: content-free, fail-open, scoped.

## Deviations

(none)
