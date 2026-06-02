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
- Read-delta and exact repeated-output now emit content-free cache decisions:
  mechanism, hit/miss action, reason, route, and count. Reasons include first
  seed, unchanged hit, recent-edit/full-pass, archive unavailable, missing
  session/key, non-shorter delta, no delta, previous-content unavailable, and
  dependency-safe repeated-output full-pass states.
- `/admin/state`, `aggregate-savings`, and `wss-audit --admin-state-file` expose
  the same cache decisions globally and per route, so future hit-rate work can
  separate "not seen before" from "unsafe/not worth caching" without payload
  capture.
- 2026-06-02 automatic scoped CLI proof covered ranged read-delta through the
  product WSS path. Capture
  `/Users/christopher/.slimference/captures/auto-proof-cli-20260602T002703Z.jsonl`
  includes a repeated `sed -n '1,160p' AGENTS.md` workload. Replay passed
  `--fail-on-lost`, the ranged hit contributed a `recoverable_prior_full`
  elision, and live counters reported `read_delta_blocks=1`,
  `read_delta_misses=1`, `tool_resolution_misses=0`, and positive billable WSS
  input savings. The same run proved repeated non-read output hits through
  `repeated_output_blocks=2`.
- 2026-06-02 hardened Codex exec-envelope normalization for repeated non-read
  outputs, matching the read-delta fix. Repeated-output now hashes/caches the
  stable payload after `Output:\n` while preserving the current envelope header
  around the marker; captured-output archives also store stable payload bytes.
  This prevents volatile `Chunk ID` / `Wall time` metadata from lowering hit
  rate or producing unreconstructable nested archive chains.
- 2026-06-02 automatic scoped CLI proof broadened repeated non-file coverage
  beyond `rg` into large `git status --short` output in a temporary git repo.
  Capture
  `/Users/christopher/.slimference/captures/auto-proof-git-status-20260602T004545Z.jsonl`
  replayed with `lost=0`, `gate_passed=true`, `mutated_requests=3`, and
  `bytes_saved=4128`; live WSS counters reported `input_tokens_saved=1518`,
  `codex_exec_envelope_blocks=3`, zero tool/command/read misses, zero
  parse/degraded/compression errors, and final host budget `ok`. This proof is
  a git/status Layer-0 reducer win rather than an exact repeated-output hit:
  the repeated-output cache correctly reported `missing_key_session_or_short_output`
  while the safer git-status/envelope reducer carried the savings.

Remaining before this task can close:

- Prove the remaining command-shape matrix on real Desktop captures and on CLI
  shapes not covered by the 2026-06-02 automatic ranged/search run.
- If live captures show unexplained cold misses, add narrower diagnostics for
  reconnect hydration, TTL eviction, and ambiguous workdir.

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
