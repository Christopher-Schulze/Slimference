# T17 - Git Hygiene and .gitignore Alignment

Status: done
Priority: low
Scope: repo root, .gitignore, tracked artefacts

---

## Problem

Three files are tracked in git despite being covered by `.gitignore`:

- `sum_coverage.out` (32 KB, leftover from initial commit)
- `tokenproxy` (18 MB build artefact, legacy binary name)
- `tokenproxy.test` (20 MB test binary)

The user wants **everything else tracked**. Only the MiniMax-key file
(`.env.local`) and the RTK reference tree (now `research/rtk-ai/rtk/`)
may be ignored.

---

## Desired End State

- No stale build artefacts or coverage dumps in the working tree.
- `.gitignore` reflects exactly the intent: local secrets ignored, build
  artefacts ignored, rest tracked.
- `git ls-files | grep -v '/'` returns only source-of-truth files
  (docs, go.mod, go.sum, AGENTS.md, CLAUDE.md, handover.md, spec files, .env.local
  is *not* in ls-files).

---

## Work Packages

### WP1 - Untrack stale artefacts

- `git rm --cached sum_coverage.out tokenproxy tokenproxy.test`
- Delete files from working tree (they are regenerated on build/test).
- Commit in isolation with a clear message.

### WP2 - Verify .gitignore correctness

- Confirm `.env.local` is ignored (already present in .gitignore).
- Confirm build artefacts pattern covers `slimference`, `benchmarks`, `ci`,
  `utils`, `tokenproxy*` (already present; just verify).
- Add explicit guard: `/slimference.test` if missing.

### WP3 - Ensure CI stays green

- `go test ./... -count=1` still passes.
- `scripts/ci` is unaffected.

---

## Subtasks

- [x] git rm --cached the three files.
- [x] Verify .gitignore patterns match remaining generated artefacts.
- [x] Commit and push as a self-contained hygiene PR.
- [x] Cross-check: `git ls-files | grep -E '\.(out|test)$|^tokenproxy$'` must be empty.

## Acceptance Criteria

- Repo clone is ~40 MB lighter.
- No build artefact or coverage dump is tracked.
- `.gitignore` contains only intentional ignores: local secrets, build artefacts,
  IDE files, and the RTK reference tree.
