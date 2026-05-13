# T55 - Structure-Preview (T38) Default-On nach Beta-Phase

Status: superseded by T74, then restored by T76
Priority: P2
Scope: `internal/compression/`, `internal/config/`, `docs/tuning-inventory.md`, `docs/benchmarks.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

T38 shipped "Structure-aware tool-result preview": for large tool outputs,
inject a short structural summary (top-level keys / table of contents / file
list) instead of truncating the body. It was temporarily gated behind
`structure_preview = false` by T74 because the original T38 acceptance criteria
required cautious rollout until recovery was proven.

4 weeks of opt-in use is enough signal to re-evaluate. Evidence from
analytics snapshots of opted-in users shows:

- 0 reported info-loss incidents.
- 12-18 % additional token savings on large tool outputs.
- Zero user complaints on the `Read`/`Grep` flows (the most common
  large-result tools).

Conclusion at the time: switch default to on with a prominent opt-out. T74
paused that rollout because archive-backed reversibility was not complete; T76
later restored default-on after the recovery foundation landed.

## Current State

- `structure_preview = true` by default again after T76.
- Behaviour is fully implemented, tested, documented, and archive-backed.
- No measurement on what users lose when it triggers.

## Target State

- Default `structure_preview = true` after T76's archive-backed recovery.
- Loud opt-out via `SLIMFERENCE_STRUCTURE_PREVIEW=false` + config
  field.
- Preview records a **reversible hint**: each preview references the
  original bytes via a tool-archive ID (T40 integration) so a user can
  run `slimference expand <id>` to retrieve.
- Telemetry:
  - `structure_preview_applied_total`
  - `structure_preview_expand_rate` (how often users recover the
    original - a proxy for info-loss).
- Per-type opt-out: allow skipping preview for specific tool names
  (e.g. `Edit`, `Write` where the original output is already small).

## Design

### Config

`[compression.structure_preview]`:

```toml
enabled = true
min_tokens_for_preview = 600      # already exists as structure_min_tokens
reversible = true                  # integrate with tool-archive for expand
opt_out_tools = []                 # e.g. ["Edit", "Write"]
```

### Reversible integration

On preview generation:

1. Submit full tool output to `internal/toolarchive` with a
   `source=structure_preview` tag.
2. Embed archive ID in preview text:
   `[slim://tool/<id> - run 'slimference expand <id>' for full output]`.

### Telemetry

- Counter: applied / skipped / expand-hits.
- Expand-hit rate = `expand_invocations / preview_applied`. If > 5 %,
  flag for review (users are frequently recovering - preview may be
  too aggressive).

### Rollout plan

1. Merge with `enabled = true` default.
2. Add entry to `docs/changelog.md` Unreleased: "BREAKING-ish: structure
   preview is now on by default. To preserve prior behaviour set
   `[compression.structure_preview] enabled = false`."
3. Monitor expand-hit rate for first week. If > 10 %, revert default to
   false and revisit heuristic.

## Implementation Plan

### WP1 - Config flip + migration note in changelog.

### WP2 - Tool-archive integration (if not already wired).
- `structure_preview` pipeline submits original output to
  `internal/toolarchive`, embeds archive ID in preview.

### WP3 - Telemetry
- Counters for applied / skipped / expand-rate.

### WP4 - Per-tool opt-out.

### WP5 - Tests
- Preview on, expand works, SHA of expanded matches original.
- Per-tool opt-out filters as expected.

### WP6 - Rollout docs.

---

## Subtasks

- [x] Flip default in config. Superseded by T74 safety rollback.
- [ ] Wire tool-archive integration for reversibility.
- [ ] Add `opt_out_tools` config.
- [ ] Telemetry counters + expand-rate.
- [ ] Tests: end-to-end preview + expand.
- [ ] Changelog migration note.
- [ ] `docs/tuning-inventory.md` entry.

## Risks

- Preview may drop critical output for rare tool types. Mitigation:
  reversibility means user can always retrieve. Monitor expand-rate.
- Tool-archive disk growth if previews are frequent. Mitigation: T40
  already has retention/gc; ensure `source=structure_preview` items
  participate in gc.

## Acceptance Criteria

- [ ] Default is on in v2.1.0.
- [ ] Preview references archive ID, `expand` round-trips full output.
- [ ] Expand-rate tracked and exposed.
- [ ] Per-tool opt-out works.
- [ ] Changelog migration note present.
- [ ] `go test -race ./...` green.

## Out of Scope

- Learning which tools produce previewable output at runtime.
- Language-specific preview beyond existing structure extraction.

---

## Validation

```
go test -race ./internal/compression/... ./internal/toolarchive/...
curl -s 127.0.0.1:8990/admin/status | jq .structure_preview
./slimference expand <id>   # round-trip check
```
