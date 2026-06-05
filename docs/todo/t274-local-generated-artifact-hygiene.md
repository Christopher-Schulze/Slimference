# T274 - Local generated-artifact hygiene

## Status

Done.

## Source

External model-review follow-up after validating repository state at commit
`f0f96ed`.

## Evidence

The following files or directories existed locally and were not tracked by git:

- `proxy.test` around 32 MB
- `readcache.test` around 12 MB
- `benchmarks` around 3.4 MB
- `dist/` around 20 MB
- `cmd/slimference/~` empty directory tree

`git ls-files` does not list them, so this is working-tree and disk hygiene, not
a committed-artifact bug.

## Why

Generated local artefacts waste disk and confuse audits. This project operates
under tight local disk pressure, and generated files in the repo root make model
and human reviews noisier. Cleanup must be explicit and repeatable, but it must
never delete source, fixtures, captures, or release artefacts that are intended
to be preserved.

## Scope

- Verify each candidate is untracked before removal.
- Verify ignore patterns cover Go test binaries, local benchmark binaries,
  coverage output, and release scratch output.
- Add or update a small Go utility or verification mode only if existing
  scripts do not already cover this.
- Prefer a guard that reports stale artefacts first; deletion remains an
  explicit operator action unless the target is an obvious build/test output.

## Non-goals

- Do not delete checked-in fixtures, scrubbed live corpus rows, docs, or
  release-source files.
- Do not rewrite `.gitignore` broadly.
- Do not move real release pipeline output unless the release pipeline already
  designates it as scratch.

## Acceptance

- [x] `git status --short` is clean before and after cleanup.
- [x] `git ls-files proxy.test readcache.test benchmarks dist
  cmd/slimference/~` remains empty unless a file is intentionally tracked.
- [x] Local stale artefacts are removed and reported by a repeatable guard.
- [x] `go run ./scripts/ci` passes after cleanup.
- [x] Disk reclaimed is measured with `du -sh` before and after.

## Implementation

Added `go run ./scripts/utils local-artifact-hygiene` as a narrow fail-closed
guard for only the known local generated artefacts:

- `proxy.test`
- `readcache.test`
- `benchmarks`
- `dist`
- `cmd/slimference/~`

The default check exits non-zero when these paths exist and emits text or JSON.
`--clean` removes only these candidates, and only after the git status check
confirms that the candidate is not tracked. If a candidate ever becomes tracked,
the guard reports it as unsafe and does not remove it.

## Verification

- `git status --short` before cleanup: clean.
- `git ls-files proxy.test readcache.test benchmarks dist cmd/slimference/~`:
  empty.
- `git check-ignore -v proxy.test readcache.test benchmarks dist
  cmd/slimference/~`: all candidates ignored by the existing `.gitignore`
  rules.
- Before cleanup:
  - `proxy.test`: 32M
  - `readcache.test`: 12M
  - `benchmarks`: 3.4M
  - `dist`: 20M
  - `cmd/slimference/~`: 0B
- `go run ./scripts/utils local-artifact-hygiene --json` before cleanup:
  found 70,606,266 bytes across five ignored, untracked candidates and exited
  non-zero as intended.
- `go run ./scripts/utils local-artifact-hygiene --clean`: removed 67.3MiB
  across five candidates.
- `go run ./scripts/utils local-artifact-hygiene --json` after cleanup:
  `clean=true`, `total_bytes=0`.
- `go test ./scripts/utils -run 'LocalArtifact|TestRunLocalArtifact' -count=1`:
  passed.
- `go run ./scripts/ci`: passed.

## Notes

- This is local hygiene, not a model-quality or savings feature.
- Existing T17 covered previously tracked artefacts; this task covers current
  untracked local outputs and repeatability.
