# T18 - RTK-Master Logic Audit and Folder Removal

Status: done
Priority: medium
Scope: rtk-master/ folder, cross-reference with internal/filter, internal/compression

---

## Problem

`rtk-master/` was embedded early as the reference implementation whose Rust
logic was to be ported to Go. 287 files and 3.2 MB are tracked. Intent from
`AGENTS.md` was always "inspiration only, folder disappears when port is done".

Open questions that block deletion:

1. Is every useful heuristic from RTK ported to `internal/filter/` and
   `internal/compression/`?
2. Are there edge cases, regex patterns, or tool-output fixtures in RTK that
   our Go port does not cover?
3. Which of the 24 built-in filters has full RTK parity, which is a strict
   subset, which has additions we made?

Without answers, we risk losing compression quality if we delete blindly.

---

## Desired End State

- Documented parity matrix: for each RTK module/filter, state "ported fully",
  "ported with modifications", or "intentionally dropped".
- Every useful fixture ingested into `tests/fixtures/` or package-level
  `testdata/`.
- After audit: `rtk-master/` removed from the tree and from git history
  (or at least from HEAD), `.gitignore` updated, documentation cleaned.

---

## Work Packages

### WP1 - Inventory RTK logic

- Walk `rtk-master/src/` and list all compressors/filters/classifiers.
- Match each to the closest Go counterpart under `internal/filter/` or
  `internal/compression/`.
- Produce a matrix in `docs/rtk-parity.md` with three columns: RTK source,
  Go counterpart, parity status.

### WP2 - Identify true gaps

For each "subset" or "missing" entry:

- Extract the heuristic or regex as a standalone note.
- Decide: port to Go now (new subtask under T25 if L0 filter) or drop with
  written justification.
- Copy relevant fixtures into `internal/filter/testdata/` with provenance.

### WP3 - Remove the folder

- Once WP1/WP2 are signed off: `git rm -r rtk-master/`.
- Add `rtk-master/` to `.gitignore` as a guard against accidental re-add.
- Remove cross-references from `AGENTS.md` / `docs/*` where applicable
  (keep a historical note in `docs/changelog.md`).

---

## Subtasks

- [x] WP1 parity matrix checked in as `docs/rtk-parity.md`.
- [x] WP2 gap list resolved: either ported or written-off with reason.
- [x] Fixtures from RTK copied where valuable.
- [x] `git rm -r rtk-master/` + `.gitignore` update + changelog entry.

## Acceptance Criteria

- No compression heuristic regresses after RTK removal (verified by
  benchmark diff against pre-removal baseline).
- `docs/rtk-parity.md` exists and is complete.
- Repo is lighter, clone time drops.
