# T264 - Codex read/ranged/repeated-output cache-hit maximizer

## Why

Codex reads files through shell commands, often via `sed -n`, `head`, `tail`,
and repeated search/git/test commands. The safest savings come from exact cache
hits after the model already had the source. This task improves hit rate instead
of using first-read lossy scan modes.

## Current reality check

- Full-file and ranged read-delta exist.
- Repeated non-file output dedup exists.
- Tool-use persistence across WSS reconnect exists.
- First reads must remain full-pass.
- More savings are likely available from better command normalization and
  dependency-aware keys.

## Product target

Maximize exact hits:

- first observation full-passes and seeds state
- later unchanged reads collapse
- changed reads use lossless position-aware deltas or full-pass
- post-edit first read full-passes
- repeated non-file outputs collapse only when exact and dependency-safe

## Technical work packages

1. Broaden command shape extraction:
   - `bash -lc`
   - command arrays
   - shell wrappers
   - `cd <repo> && sed|cat|head|tail`
   - `awk` simple range reads if deterministic and safe
   - `python - <<EOF` file-print helpers only if exact parser exists
2. Build dependency-aware keys:
   - file path + range + content hash for reads
   - repo root + command + relevant env/cwd for non-file outputs
   - git commands include repo root and git dir
   - package/test commands include lockfile/config hashes where needed
3. Strengthen edit/recency rules:
   - same-session edits force next read full-pass
   - unknown edit state full-passes
   - repeated post-collapse re-reads loosen the session
4. Add cache-hit diagnostics:
   - why a read missed
   - why a command was unsafe to cache
   - reconnect rehydration success
   - stale state eviction
5. Bound state:
   - TTL
   - LRU size
   - no raw secrets in metadata
   - archive raw content only through contentarchive policy

## Progress

2026-05-31 exact-read shape slice:

- Added strict `awk` read recognition for deterministic line-range reads:
  - `awk 'NR==42{print}' file`
  - `awk 'NR>=10 && NR<=20 {print}' file`
  - `awk 'NR>=42{print}' file`
  - `awk 'NR<=42{print}' file`
  - `print` and `print $0` only
- Unsupported `awk` forms, multiple files, variables, projections, pipes,
  redirects, and shellisms full-pass.
- Product default first-read filtering now uses the same read-request parser as
  readcache, so newly recognized read commands full-pass on first observation
  and only become cache candidates on later exact observations.
- Workdir and `cd <repo> && awk ...` normalization now inherit the existing
  read command path canonicalization, preserving repo scope.
- Generic repeated-output keys now include Codex workdir metadata when present.
  This prevents `go test ./...` or similar non-file commands in two repositories
  from sharing a session key just because the command string and output hash are
  identical.
- Dependency-sensitive command keys (`go`, `cargo`, Node package managers,
  Python test/package commands, `jest`, `vitest`, `tsc`, `eslint`) now include
  a bounded hash over present dependency/config files such as `go.mod`,
  `go.sum`, `Cargo.lock`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`,
  `pyproject.toml`, `poetry.lock`, and test config files. No raw file content is
  stored in the key, and oversized dependency files are skipped.

Remaining before this task can close:

- Add explicit miss-reason diagnostics for unsafe command shapes, post-edit
  reseed, reconnect hydration, TTL eviction, and ambiguous workdir.
- Prove the broader command-shape matrix on real CLI/Desktop captures.

## Zero product-drawdown gates

- Never collapse first reads.
- Never collapse when the model did not previously receive the full relevant
  content in this session namespace.
- Never collapse after an edit until a full read reseeds the model.
- Never reuse keys across repositories.
- Unknown commands full-pass.

## Savings targets

- Repeat/ranged read workloads: high positive billable-input savings with
  lost=0 in replay.
- Normal workday sessions: cache-hit rate should improve without increasing
  repair/re-read rate.
- Host cost: keying and cache lookup should be sub-millisecond for normal tool
  outputs and bounded for large outputs.

## Verification

- Unit tests for every command shape.
- Cross-repo collision tests.
- Post-edit full-pass tests.
- WSS reconnect tests.
- Real CLI/Desktop captures:
  - repeat read
  - ranged read
  - edited file read
  - repeated git status
  - repeated test/search

## Done

This task is done when cache hits increase through exactness, not elision, and
all miss/full-pass reasons are observable enough to keep improving the hit rate
without risking model context.
