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
