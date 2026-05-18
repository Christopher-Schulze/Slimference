# TASK 237: Codex provider display name

Status: DONE
Priority: P2 UX polish
Scope: User-facing Codex provider label only

## Why

The scoped Codex route was technically named `slimference-codex`, but the user
experience should show a clean product label when Codex surfaces a provider
badge. The label should read `Slimference` without implying a different routing
mode or extra integration surface.

## Acceptance

- Provider display text is `Slimference`.
- No transport behavior changes.
- No config format changes.
- No Desktop routing claim is introduced by this cosmetic rename.

## Sub-Tasks

- [x] Rename user-facing provider display label.
- [x] Keep route identifiers and cert/version logic behavior-compatible.
- [x] Commit as a standalone cosmetic task.

## Notes

Shipped before this detail file was added. This file records the already-landed
task so later task numbers remain contiguous and understandable.

## Deviations

None.
