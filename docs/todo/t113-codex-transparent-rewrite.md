# TASK 113: Codex hook transparent rewrite path

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P1
Scope: `internal/hooks/codex.go`, `internal/hooks/verify.go`, `internal/hooks/drift.go`, `cmd/slimference/integrate_cmd.go`
Driver: The current Codex pre-tool hook uses `{decision: "block", reason: "Rerun this command through the local output filter: <rewritten>"}` because Codex 0.121 had no `updatedInput` semantics. This is fragile: it relies on the model interpreting the reason string and re-issuing the call. A rebellious / quota-conscious model can ignore it. A lossy reason text can cause the model to re-issue the wrong command. T05 acknowledged this and shipped the workaround; T72 tightened ownership; neither gave us transparent rewrite. Codex has since shipped multiple releases - we need to detect the supported semantic, prefer transparent rewrite when the version supports it, and ship a hard error for unsupported versions instead of the silent block-rerun fallback.

---

## Problem

`internal/hooks/codex.go::CodexPreToolHookScript`:
- Always emits `decision=block + reason=<rewritten cmd>` regardless of Codex version.
- Relies on model compliance with the reason text.
- Cannot distinguish "I rewrote your command, run this instead" from "I'm refusing this command for safety" - the same JSON shape covers both.
- Failure mode if model ignores: original (uncompacted) command runs unchanged - silent regression.
- `hook verify` does not check Codex version.

## Target State

Three layers:

1. **Version-aware emission**: detect the locally-installed Codex CLI version. For versions that support the structured rewrite hook output (whatever Codex calls it - `updatedInput` style or equivalent), emit that. For older versions, emit the legacy block-rerun **with a deprecation warning** logged to `~/.slimference/logs/hooks.log`.
2. **Hard verify**: `hook verify` fails (exit 2, not 1) when the installed Codex version is below the minimum that supports transparent rewrite, unless the operator has explicitly set `[hooks.codex] allow_legacy_rewrite = true` in config.
3. **Default min-version cutover**: once a Codex release with transparent rewrite has been the recommended version for a release cycle, drop the legacy emission entirely and require minimum version. Tracked under T113b.

This task lands the version-aware emission, the verify gate, and the deprecation warning. It does NOT immediately drop legacy support (operator deserves a transition window).

## Implementation Plan

### WP1 - Codex version probe
- `internal/hooks/codex.go::DetectCodexVersion()` runs `codex --version` (or whatever the canonical version flag is) and parses semver.
- Failure to detect = treat as "unknown legacy" -> warning + legacy emission.

### WP2 - Capability table
- New `internal/hooks/codex_caps.go` mapping (codex_version_range -> supported_features). Features enum: `transparent_rewrite`, `permission_decision`, `structured_output`.
- Single source of truth so `claude.go` can adopt the same pattern when Anthropic CLI evolves.

### WP3 - Version-aware script generator
- `CodexPreToolHookScript(version, cmd)` branches:
  - Modern: emits `{hookSpecificOutput: {hookEventName: "PreToolUse", updatedInput: {command: <rewritten>}}}` (or whatever Codex's structured shape is - **update to actual schema once verified against current Codex**).
  - Legacy: emits today's `{decision: "block", reason: "Rerun..."}` + writes a one-time-per-day deprecation line.

### WP4 - hook verify gate
- `verify.go` reads detected Codex version. If below `[hooks.codex] minimum_version` (default = the lowest version with transparent rewrite), exits 2 with a clear message: "Codex <ver> does not support transparent rewrite; upgrade or set [hooks.codex] allow_legacy_rewrite=true."
- `slimference doctor` mirrors this check.

### WP5 - Drift watchdog hook
- T33's drift watchdog already polls hook scripts for content drift. Extend: if the locally-installed Codex version changes (probe periodically), re-run `InstallCodex` to regenerate the script for the new version's capabilities.

### WP6 - Telemetry
- `RequestSummary.HookPath string` ("transparent_rewrite" | "block_rerun" | "passthrough").
- `/admin/status.hooks.codex.{transparent_total, block_rerun_total, model_ignored_block_total}`.
- "model ignored block" detection: when Codex post-tool sees a tool_result for a command that pre-tool tried to block-rewrite, that's an ignored block - count it.

### WP7 - Tests
- Per-version script generation snapshot tests.
- Version detection happy path + failure path.
- verify gate exit-code matrix.
- Drift watchdog regenerates on version change.

### WP8 - Documentation
- `docs/integration.md` updated with the supported Codex version range + legacy-mode escape hatch.
- Hook install command output includes the detected version + chosen path.

## Acceptance Criteria

- [ ] `CodexPreToolHookScript(version, cmd)` emits the modern shape for supported versions and the legacy shape with a deprecation warning for older versions.
- [ ] `slimference hook verify` exits non-zero when Codex is below `minimum_version` and `allow_legacy_rewrite` is not set.
- [ ] Telemetry exposes the three counters above; `model_ignored_block_total` is observable on real traffic.
- [ ] Drift watchdog regenerates the script on Codex upgrade.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Implementing transparent rewrite for Anthropic CLI (claude.go already supports it; this task is Codex-specific).
- Auto-upgrading the operator's Codex install (T65 owns that surface).
- Dropping legacy support entirely (tracked as T113b once the soak window confirms modern is universal).

## Validation

```
go test -race ./internal/hooks/...
go run ./scripts/verify
slimference hook verify  # manual on dev machine with both Codex versions tested
```
