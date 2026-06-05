# TASK 283: RTK Codex audit closure

## Why

The RTK audit still used `port-later` wording for items that are not Codex
product gaps after the current Codex hook reality check. The product docs must
not imply hidden open work when an item is either Claude-only, prompt-level
advisory, or rejected as a default because it can remove model-relevant code
detail.

## Acceptance

- `docs/rtk-audit.md` distinguishes Codex product gaps from Claude-only or
  advisory RTK surfaces.
- RTK aggressive code-signature summaries remain rejected as default product
  behavior because they remove implementation bodies.
- `docs/rtk-parity.md` states that non-ported RTK surfaces are closed decisions,
  not live dependencies.
- `go test ./docs` passes.
- `git diff --check` passes.

## Sub-Tasks

- [x] Close stale `port-later` wording in the RTK audit.
- [x] Update parity summary with closed non-port decisions.
- [x] Run docs gates and commit.

## Notes

- Codex CLI `0.137.0` reports `hooks` as stable and enabled in this local
  installation.
- The refreshed Codex manual says hooks execute command handlers for lifecycle
  events, but only `type: "command"` handlers run today; `prompt` and `agent`
  handlers are parsed but skipped, and async command hooks are skipped.
- The same manual lists matchers for `PreToolUse`, `PermissionRequest`, and
  `PostToolUse`, but it does not establish a Claude-style `updatedInput`
  command-mutation contract for Codex.
- RTK's Codex integration is prompt-level awareness, while its Claude
  integration uses `PreToolUse.updatedInput`.
- `go test ./docs` passed.
- `git diff --check` passed.

## Deviations

- None.
