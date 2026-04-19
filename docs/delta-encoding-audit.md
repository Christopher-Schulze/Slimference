# Delta Encoding Audit

Date: 2026-04-19

## Current Scope

Slimference currently applies delta-style savings in two places:

- file-version delta tracking in `internal/compression/delta.go`
- cross-tool-call repeat/delta stability through resolved tool-call keys in
  `internal/compression/tool_call_key.go`, `layer1.go`, and
  `repeated_collapse.go`

## Current Result

- tool outputs are no longer keyed by weak first-line heuristics alone
- repeated `tool_result` blocks can be matched against the originating
  `tool_use` metadata
- normalized preprocessed output is used as the comparison base, which makes
  deltas survive ANSI/comment stripping and similar preprocessing

## Remaining Limit

The current implementation is a pragmatic textual delta/repeat system, not a
full semantic diff engine with parser-backed JSON or command-specific renderers.

That is acceptable for the current product state because:

- the savings are already material on repeated tool-heavy sessions
- the output stays deterministic and readable
- the zero-downside guard still prevents net-negative compaction

If a later benchmark corpus proves that command-specific semantic diffs would
move the needle further, that should be a fresh follow-up task rather than a
retroactive rewrite of the current scope.
