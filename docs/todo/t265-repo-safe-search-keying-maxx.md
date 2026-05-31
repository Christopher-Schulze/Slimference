# T265 - Repo-safe search keying and search-output savings max-out

## Why

Search output is one of the most common Codex tool surfaces. It is also easy to
compress incorrectly: a search result is only meaningful inside a repository,
with a pattern, cwd, include/exclude filters, and command flags. This task
maximizes search savings while preventing cross-repo false hits and preserving
match context.

## Current reality check

- Search grouping exists and a real Codex rg truncation bug was fixed.
- Search-output delta exists.
- Safe `cd <abs> && ...` normalization exists in some paths.
- Repo-safe keying must be completed and proven across all search shapes.

## Product target

Search reducers must preserve the model's ability to locate code. The compacted
form must include enough information to act: repo root, file paths, line
numbers, match snippets, omitted counts, and recovery/re-run hints when output
is capped.

## Technical work packages

1. Normalize search command keys:
   - `rg`, `grep`, `git grep`, `ag`, `ack`, `ugrep`
   - `bash -lc 'cd <repo> && rg ...'`
   - `git -C <repo> grep ...`
   - workdir metadata from Codex frames
   - quoted paths and spaces
2. Include semantic key parts:
   - repo root
   - pattern or pattern hash
   - file globs
   - ignore flags
   - case flags
   - context flags
   - hidden/binary flags
3. Preserve output evidence:
   - file
   - line
   - column if present
   - snippet
   - number of matches per file
   - omitted files and omitted matches
   - tail truncation detection
4. Add delta and repeated-output handling:
   - identical search output exact collapse
   - changed search output uses added/removed result lines
   - large search output archive-backed where route supports it
5. Add safety policy:
   - no cross-repo key reuse
   - no collapse if command flags are not understood
   - no collapse if output format is non-standard and parser confidence is low

## Zero product-drawdown gates

- A compacted search must never hide the only matching file without a recovery
  path.
- A repeated search collapse must only happen if the same repo/pattern/flags
  were previously full-passed or recoverably archived.
- A search with ambiguous cwd full-passes.
- Truncated output must be labelled as truncated and should prefer preserving
  earliest and highest-signal matches.

## Savings targets

- Real Codex rg/grep captures: grouping should reduce large outputs by a target
  50%+ when there are many matches, without losing first actionable match per
  file.
- Repeated identical searches: second and later outputs should collapse to a
  small exact reference.
- Changed searches: delta should be smaller than full output while preserving
  added/removed matches.

## Verification

- Unit tests for command normalization.
- Cross-repo negative tests.
- Truncated envelope tests.
- Live CLI/Desktop captures with repeated `rg`, changed repo state, and large
  match sets.
- `wss-ab-replay --fail-on-lost`.

## Done

Search is maxxed when repo-safe keys cover the real Codex command shapes, large
searches compact by default, repeated searches dedup exactly, and no compacted
search can mislead the model about where matches are.
