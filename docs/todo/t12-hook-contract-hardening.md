# T12 - Hook Contract Hardening

Status: closed
Priority: critical
Scope: `internal/hooks/*`, hook-related CLI surfaces, supported agent compatibility

---

## Problem

The repository currently claims strong hook adoption for Claude Code and Codex,
but the implementation does not yet satisfy that claim with enough rigor:

- Claude hook generation is not yet aligned with the modern structured rewrite
  contract and overwrites unrelated user hook configuration.
- Codex `PostToolUse` currently uses the wrong execution surface for tool output
  filtering.
- `slimference hook verify` does not fail hard enough on Codex breakage.

---

## Desired End State

1. Claude Code install/remove/verify is contract-correct and non-destructive.
2. Codex install/remove/verify is contract-correct and non-destructive.
3. Hook verification is authoritative for both supported CLIs.
4. Hook tests prove real behavior, not just string presence.

---

## Work Packages

### WP1 - Claude Code rewrite contract

- Replace raw stdout command-rewrite behavior with the documented structured
  hook response format.
- Emit explicit `updatedInput` only when rewrite is requested.
- Preserve passthrough, deny, and ask semantics with deterministic exit rules.

### WP2 - Claude settings merge and removal safety

- Merge into existing hook structures instead of replacing `PreToolUse`.
- Tag Slimference-owned entries so removal can delete only owned records.
- Leave unrelated user hooks untouched on remove.

### WP3 - Codex post-tool filtering correctness

- Replace the current `slimference filter -- "$RESPONSE"` misuse with a
  dedicated path that filters an existing tool output blob.
- Use one explicit implementation path for post-tool filtering instead of a
  command-execution path.
- Ensure the returned hook payload matches the current Codex contract.

### WP4 - Verification semantics

- Make `hook verify` fail whenever a supported integration is missing, broken,
  or internally inconsistent.
- Verify both config surface and generated script surface.
- Include SHA-256 only as a supporting detail, not as the only validation.

### WP5 - Test matrix

- install on empty home directory
- install on pre-populated home directory
- merge with unrelated existing hook config
- remove only Slimference-owned entries
- verify fails on missing script
- verify fails on malformed config
- verify fails on contract mismatch

---

## Implementation Notes

- Preserve backward compatibility only when it does not reduce correctness.
- Prefer explicit ownership markers in generated config over brittle string
  heuristics.
- Keep the supported surface narrow: Claude Code and Codex only.

---

## Subtasks

- [x] Rework Claude hook script output to the structured contract.
- [x] Make Claude install merge-only and Claude remove ownership-aware.
- [x] Introduce a dedicated post-tool filtering path for Codex output.
- [x] Rework Codex hook generation to the real supported contract.
- [x] Make `hook verify` fail hard for Codex integration errors.
- [x] Add fixture-heavy tests for install, verify, and remove flows.
- [x] Add compatibility notes to `docs/documentation.md` after the code is proven.

Closure note:

- Claude Code now emits `hookSpecificOutput.updatedInput` / permission decisions
- Codex now installs `hooks.json` PreToolUse + PostToolUse hooks and uses
  `slimference posttool` for finished tool output compaction
- verify checks scripts, config, and runtime install coherence for both targets

---

## Acceptance Criteria

- A user with pre-existing Claude hooks does not lose unrelated config.
- A user with pre-existing Codex hook config does not lose unrelated config.
- `slimference hook verify` is trustworthy for both supported CLIs.
- The hook paths no longer rely on executing a finished tool output as if it
  were a shell command.
