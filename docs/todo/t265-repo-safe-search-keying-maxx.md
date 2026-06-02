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

## Progress

2026-05-31 core implementation slice:

- `filter.SearchOutputKeyFromCommandLine` now recognizes repo-scoped
  `git -C <repo> grep ...` keys.
- `filter.NormalizeSearchCommandLine` canonicalizes search commands for
  compaction/keying only, never for execution:
  - `cd /repo && rg -n needle internal` -> `rg -n needle /repo/internal`
  - `rg -n needle internal` with workdir `/repo` -> `rg -n needle /repo/internal`
  - `grep -R needle .` with workdir `/repo` -> `grep -R needle /repo`
  - `git grep needle -- internal` with workdir `/repo` -> `git -C /repo grep needle -- internal`
- Captured-output compaction now recognizes `cd <repo> && rg|grep...` instead
  of treating the compound command as opaque noise.
- WSS/HTTP proxy command normalization now applies workdir-aware search
  canonicalization, so repeated search-output keys include repository scope.
- Regression coverage proves identical `rg -n TODO src` commands in `/repo/a`
  and `/repo/b` do not share a repeated-output key.
- Search option parsing now recognizes more real `rg`/`grep`/`ag`/`ack`/`ugrep`
  value-taking flags, including glob/type/context/include/exclude/sort/engine/
  preprocessor/replace/size-limit forms, so the pattern/path split stays stable
  instead of accidentally treating an option value as the search pattern.
- Canonicalized wildcard arguments are quoted before re-parsing, preserving
  literal `*.go`/`?` globs in keys without triggering shellism rejection.
- Search grouping now full-passes non-matchline modes such as `--json`,
  `--files`, `-l`, `-L`, `-c`, `-o`, `--count`, and `--vimgrep`. Exact repeated
  output can still dedup later, but the first output is not semantically
  regrouped into fake `file:line:content` evidence.
- Reusable repeated-output/search-delta keys now require visible repository
  scope. `cd <abs> && rg ...`, workdir-normalized `rg`, absolute search paths,
  and `git -C <abs> grep ...` can seed/collapse; a bare implicit-cwd
  `rg -n TODO src` can still be grouped on the first output but cannot seed a
  cross-turn cache/delta key. This removes the last offline-visible cross-repo
  false-hit path without reducing safe first-pass grouping savings.
- 2026-06-02 automatic scoped CLI search-loop proof covered a repeated `rg -n`
  workload through WSS Phase-F with repo/workdir metadata. Capture
  `/Users/christopher/.slimference/captures/auto-proof-cli-20260602T002703Z.jsonl`
  replayed with `lost=0`, `mutated_requests=4`, `bytes_saved=11381`, and live
  counters reported `captured_output_blocks=1`, `repeated_output_blocks=2`,
  `ledger_search_capsules=3`, and zero command/tool resolution misses. The run
  first exposed a nested archive proof gap for search-output compaction followed
  by exact repeated-output elision; the reducer and A/B harness now prove the
  nested full-payload archive rather than treating it as `reference_mismatch`.
- 2026-06-02 automatic scoped CLI breadth proof covered repo-scoped `rg`,
  changed `rg` result sets, `git grep`, and `grep -R` in a temporary git repo
  through the real WSS Phase-F product path. Capture
  `/Users/christopher/.slimference/captures/auto-proof-search-clean-20260602T004340Z.jsonl`
  replayed with `lost=0`, `gate_passed=true`, `mutated_requests=13`,
  `bytes_saved=146507`, and live WSS counters reported `input_tokens_saved=45273`,
  `captured_output_blocks=19`, `repeated_output_blocks=8`,
  `ledger_search_capsules=28`, and zero tool/command resolution misses,
  parse failures, degraded sessions, or compression errors. The run also proved
  changed search results do not collapse against an obsolete exact-output key.
  A first automatic attempt is intentionally excluded from proof because prompt
  punctuation turned `.` into `..` and resumed in the wrong workdir; the clean
  capture above uses self-contained `cd <tmp> && ...` commands and is the
  citable artifact.
- 2026-06-02 added explicit repo-safe keying regression coverage for quoted
  repository and search paths containing spaces. This pins macOS-realistic
  command forms such as `cd "/Users/.../My Repo" && rg ... "src files"` and
  `git -C "/Users/.../My Repo" grep ...`, preventing future normalization work
  from silently dropping repository scope or corrupting quoted path identity.
- 2026-06-02 added canonical search match-set identity for repo-scoped
  repeated-output keys. The identity parser accepts grep-style `file:line:body`
  and `file:body` lines, skips Codex envelope noise, rejects grouped/capped
  summaries and noisy low-confidence output, and sorts only for cache identity.
  Model-facing replacements remain archive-backed and point at the current raw
  output, preserving exact recovery for order-sensitive inspection.
- 2026-06-02 wired search repeated-output lookup before search grouping on the
  Codex Layer-0 hotpath. This avoids caching already-grouped summaries under
  the same key and lets a second equivalent `rg` result collapse before the
  normal first-pass grouping reducer. Regression coverage proves the first
  search still groups and seeds, the second same-match-set search collapses,
  repo-scoped commands remain distinct, and ambiguous implicit-cwd searches do
  not get reusable keys.
- 2026-06-02 replayed the prior Desktop search-loop capture through the real
  WSS A/B reducer with `--fail-on-lost`: `frames=106`, `request_turns=4`,
  `mutated_requests=2`, `bytes_saved=48522`, `lost=0`, `gate_passed=true`.
  This closes the offline gap that caused the strict matrix to report zero
  `repeated_output` live block hits for Desktop search, but the live-token claim
  remains unchanged until a fresh Desktop capture is recorded.
- 2026-06-02 added search-set delta for changed canonical search evidence. Real
  Desktop `rg` repeats can return a different truncated subset even for the same
  command; treating that as unchanged would be wrong. The reducer now compares
  canonical match lines as sets, emits only removed and added match evidence,
  appends `[context-archive kind=full-output ...]`, and fail-opens when the delta
  is not shorter than the raw current output.
- 2026-06-02 fresh scoped Codex Desktop live proof with the current source
  recorded
  `/Users/christopher/.slimference/captures/live-desktop-search-delta-20260602T144108.jsonl`.
  Product counters: `billable_input_tokens_saved=14973`,
  `proxy_layer0_repeated_output_blocks=1`, `proxy_layer0_captured_output_blocks=1`,
  `tool_use_unresolved_blocks=0`, `command_unresolved_blocks=0`. Cache
  counters show `repeated_output hit reason=delta count=1` after the first seed.
  Replay passed `--fail-on-lost` with `frames=186`, `request_turns=4`,
  `mutated_requests=2`, `bytes_saved=57084`, `lost=0`, and `gate_passed=true`.
- 2026-06-02 tightened repo-safe search identity to plain match-line output
  only. Heading/context/passthrough/multiline/custom-separator searches no
  longer produce `search:` repeated-output keys and no longer enter the grouped
  match parser. They can still be saved by exact repeated-output identity under
  the generic command key, but they cannot be collapsed as same-match-set or
  search-delta because those reductions would not preserve the extra context
  semantics.
- 2026-06-02 fresh automatic scoped Codex CLI proof covered repeated
  repo-scoped `git grep` through the managed WSS capture runner. Capture
  `/Users/christopher/.slimference/captures/live-cli-git-grep-token3-20260602-extra.jsonl`
  produced live product counters with `billable_input_tokens_saved=4530`,
  `input_tokens_saved=4530`, `compressed_messages_mutated=2`,
  `frames_reencoded=2`, `phasef_mutations=2`,
  `proxy_layer0_captured_output_blocks=1`,
  `proxy_layer0_repeated_output_blocks=1`, and zero parse, degraded-session, or
  compression errors. Replay passed `--fail-on-lost` with `frames=151`,
  `request_turns=4`, `mutated_requests=2`, `bytes_saved=15095`, `lost=0`, and
  `gate_passed=true`. The matching matrix row is appended to
  `/tmp/slimference-live-extra-matrix.jsonl`.

Remaining before this task can close:

- Add remaining live captures for Desktop `git grep` and grep variants.
- Add explicit proof report showing large search grouping, repeated exact
  collapse, and changed search delta all stay repo-scoped with `lost=0`.

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
