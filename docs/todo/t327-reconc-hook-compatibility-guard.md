# T327 Reconc Hook Compatibility Guard

## Why

Reconc is a repo-local workflow and policy tool that can install its own Codex
hooks into `.codex/hooks.json`. Slimference also installs and removes Codex
hooks. Both tools must coexist without either install path deleting the other's
entries.

## Acceptance

- Slimference Codex hook install preserves existing Reconc-owned hook entries.
- Slimference Codex hook removal removes only Slimference-owned hook entries.
- Reconc-specific events that Slimference does not own, including
  `PostToolUseFailure` and `SessionEnd`, remain untouched.
- The compatibility contract is covered by a focused Go regression test.

## Sub-Tasks

- [x] Audit Reconc Codex hook shapes read-only under
  `/Users/christopher/CODE/Golem/tools/reconc`.
- [x] Add a Slimference regression test for Reconc hook preservation.
- [x] Document the compatibility contract.

## Notes

- Reconc's Codex hook generator uses `.codex/hooks.json` with a nested `hooks`
  object and repo-local commands that invoke `tools/reconc/bin/hook`.
- Slimference identifies only its own hook scripts/status messages when merging
  or removing entries. The new test pins that Reconc entries survive both
  operations.

## Deviations

None.
