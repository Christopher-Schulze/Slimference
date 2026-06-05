# T274 - Local generated-artifact hygiene

## Status

Open.

## Source

External model-review follow-up after validating repository state at commit
`f0f96ed`.

## Evidence

The following files or directories exist locally and are not tracked by git:

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

- `git status --short` is clean before and after cleanup.
- `git ls-files proxy.test readcache.test benchmarks dist cmd/slimference/~`
  remains empty unless a file is intentionally tracked.
- Local stale artefacts are removed or reported by a repeatable guard.
- `go run ./scripts/ci` passes after cleanup.
- Disk reclaimed is measured with `du -sh` before and after.

## Verification

- Run `git status --short`.
- Run `git ls-files` against all cleanup candidates.
- Run `du -sh` for before/after.
- Run the guard in check-only mode if implemented.

## Notes

- This is local hygiene, not a model-quality or savings feature.
- Existing T17 covered previously tracked artefacts; this task covers current
  untracked local outputs and repeatability.
