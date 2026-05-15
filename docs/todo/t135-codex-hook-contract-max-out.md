# TASK 135: Codex hook contract max-out and PostToolUse replacement

Status: DONE (local implementation 2026-05-13; live hook-behavior proof remains T140)
Priority: P1
Scope: `internal/hooks/`, `cmd/slimference/main.go` hook subcommands, `internal/filter/`, `internal/toolarchive/`, `internal/readcache/`, `docs/integration.md`, `docs/todo/t113-codex-transparent-rewrite.md`.

## Why

The primary Codex path should be transparent proxy mode. Hooks are still worth maxing out because they can compress tool-output and maintain session state before the next model turn. Official Codex hooks currently support useful events, but they do not support transparent input rewrite: `updatedInput` is parsed but not honoured and fails open. Therefore the hook strategy must be brutally realistic:

- Use what works.
- Probe what might work.
- Gate unsupported output shapes.
- Never claim hook-based transparent rewrite until live proof exists.

Official current surfaces and hard limits:

- `SessionStart`: extra developer context.
- `PreToolUse`: deny/block only, no transparent `updatedInput`.
- `PermissionRequest`: allow/deny approval prompts.
- `PostToolUse`: add context, block with feedback, or replace normal processing of the original tool result via `continue: false`; it cannot undo tool side effects.
- `UserPromptSubmit`: add context or block prompt.
- `Stop`: final validation/checkpoint hook; `decision: "block"` means continue the turn with the hook reason as the next prompt.
- `PreCompact` / `PostCompact`: schemas exist upstream, but narrative docs do not currently expose them; must be probed before relying.
- `PreToolUse`, `PermissionRequest`, `PostToolUse`, `UserPromptSubmit`, and `Stop` run at turn scope; matching hooks from multiple files all start concurrently, so one hook cannot prevent another matching hook from starting.
- `PreToolUse` / `PostToolUse` currently do not intercept every shell path; `unified_exec`, WebSearch, and non-shell/non-MCP tools need live probes before any claim.
- `UserPromptSubmit` and `Stop` currently ignore matchers.
- Unsupported fields must stay disabled: `PreToolUse.updatedInput` / `additionalContext` / `continue:false` fail open; `PermissionRequest.updatedInput` / `updatedPermissions` / `interrupt` fail closed; `PostToolUse.updatedMCPToolOutput` / `suppressOutput` fail open.

## Target State

Optional hook install becomes an advanced precision layer:

1. User can run Slimference with no Codex hook mutation.
2. If hooks are installed, every supported hook event is used for measurable value.
3. `PostToolUse` supports an opt-in visible saving path: compact/replace large supported tool output before the model relies on it when `SLIMFERENCE_CODEX_HOOK_MODE=compact` or `aggressive` is set.
4. `SessionStart` and `UserPromptSubmit` create correct session/turn boundaries for T138.
5. `PermissionRequest` enforces policy without blocking safe work.
6. `Stop` snapshots final state/checkpoints and validates no dangerous failure loop.
7. Unsupported or fail-open fields stay gated by capability matrix and live probes.

## Work Packages

### WP1 - Official contract matrix

- Extend `internal/hooks/codex_caps.go` from version-only to event/field support:
  - `hooks_feature_flag`
  - `session_start.additional_context`
  - `pre_tool_use.block`
  - `pre_tool_use.updated_input`
  - `pre_tool_use.intercepts_unified_exec`
  - `pre_tool_use.intercepts_websearch`
  - `permission_request.allow_deny`
  - `permission_request.future_fields_fail_closed`
  - `post_tool_use.additional_context`
  - `post_tool_use.block`
  - `post_tool_use.continue_false_replaces_result`
  - `post_tool_use.updated_mcp_tool_output`
  - `user_prompt_submit.additional_context`
  - `user_prompt_submit.matcher_ignored`
  - `stop.system_message`
  - `stop.decision_block_continues_turn`
  - `stop.matcher_ignored`
  - `pre_compact`
  - `post_compact`
- Populate from official docs and local probes, not assumptions.

### WP2 - Local hook probe harness

- Add `slimference hook probe codex`.
- It should create temporary hooks in a temp config layer or isolated test home where possible.
- Probes:
  - Does `updatedInput` alter Bash command? Expected today: no.
  - Does `PostToolUse additionalContext` reach next turn?
  - Does `PostToolUse decision:block` replace the original tool result with feedback?
  - Does `PostToolUse continue:false` replace normal processing of the original result?
  - Does `SessionStart additionalContext` appear as developer context?
  - Does `UserPromptSubmit additionalContext` appear?
  - Does `PermissionRequest` deny work?
  - Does `PreToolUse` see unified shell execution in the installed Codex version?
  - Does any hook see WebSearch or browser/computer-use tool calls?
  - Are `PreCompact`/`PostCompact` emitted in current Codex?
- Probe results must write a machine-readable report and update capability snapshot.

### WP3 - PostToolUse replacement strategy

- Current `posttool` mostly emits `additionalContext` and archives output. Upgrade it:
  - For large Bash output, use documented `PostToolUse` block/`continue:false` behavior to replace the original tool result with compacted feedback.
  - For `apply_patch`, keep full patch metadata and compact only noisy success text.
  - For MCP tools, classify and compact based on tool name + JSON shape.
  - For failed commands, preserve first error, file/line references, exit code, and recovery hint.
  - For long stdout/stderr, archive raw output and provide stable `local-archive://` URI.
- Never hide raw output without reversible archive.
- Never claim coverage for `unified_exec`, WebSearch, browser, computer-use, or unsupported MCP output replacement until probes prove the current Codex version emits the required hook payload.

### WP4 - Session and turn boundary hooks

- `SessionStart` should reset/recover session state and optionally attach Slimference state summary.
- `UserPromptSubmit` should mark a new user turn and reset per-turn dedup state.
- `Stop` should flush analytics, checkpoint session summaries, and emit warnings for negative savings / repeated failures.
- These hooks feed T138.

### WP5 - PermissionRequest

- Map Slimference deny/ask rules to Codex `PermissionRequest`.
- Deny always wins; allow only for policies Slimference owns.
- Add tests that no policy hook silently allows commands that Layer 0 would deny.

### WP6 - Installation modes

- `slimference hook install codex` must clearly say optional hook mode.
- It must not be implied by transparent `proxy install`.
- `integrate install --client codex` remains legacy/config-patch mode and must be labeled as mutating Codex config.
- `hook verify codex` reports each event/matcher/script separately.

### WP7 - Tests

- Snapshot tests for hooks JSON.
- Probe-report parser tests.
- `posttool` fixtures for Bash, apply_patch, MCP, failure, huge output, archive fallback.
- Compatibility tests for old Codex hook payload shapes.

## Acceptance

- [x] Hook capability matrix separates supported, parsed-fail-open, fail-closed, and unprobed fields.
- [x] `updatedInput` remains disabled until live probe proves it works.
- [x] PostToolUse path materially reduces large tool output while preserving reversible raw archive.
- [x] Hook matrix documents fail-open and fail-closed fields exactly.
- [x] Hook strategy explicitly handles concurrent matching hooks and incomplete tool interception.
- [x] SessionStart/UserPromptSubmit/Stop provide local T138 boundary events; durable cross-process state remains T138.
- [x] PermissionRequest maps Slimference policy safely.
- [x] Hook install/verify output is honest about optional mutation and enables only `hooks=true`, not base URL config-patching.
- [x] `go run ./scripts/ci` passes.

## Notes

- This task does not replace transparent mode. It is an optional enhancement for Codex CLI/App sessions that load Codex hooks.
- 2026-05-13 implementation:
  - `internal/hooks/codex.go` now installs seven Codex hook scripts: `SessionStart`, `PreToolUse` Bash/Read, `PermissionRequest`, `PostToolUse`, `UserPromptSubmit`, and `Stop`.
  - `hook install codex` writes `~/.codex/hooks.json`, executable scripts under `~/.slimference/hooks/`, and only the required `[features] hooks = true` flag. It does not write `openai_base_url` and must never write global `~/.codex/AGENTS.md`.
  - `internal/hooks/verify.go` verifies each Codex hook script and event entry separately, including SHA-256 hashes.
  - `internal/hooks/codex_caps.go` now records supported, parsed-fail-open, fail-closed, and unprobed hook fields. `PreToolUse.updatedInput` remains parsed-fail-open and disabled.
  - `cmd/slimference/main.go::handleCodexHookCmd` implements `session-start`, `permission-request`, `user-prompt-submit`, and `stop`.
  - `posttool` originally emitted documented `continue:false` + `stopReason` + `PostToolUse.additionalContext`, so Codex could replace the original large tool result with compact feedback.
  - 2026-05-15 hook ROI correction: default `posttool` mode is `auto`; outputs below 600 original tokens or below 400 saved tokens stay archive-only, but Bash outputs with >=45% savings are replaced by compact feedback. `silent` restores archive-only mode; `compact` / `aggressive` force visible replacement for every changed output.
  - 2026-05-14 hardening: Codex `PostToolUse` installs Bash-only by default, generated shell scripts have a fail-open watchdog before Codex's 600s timeout, Go `posttool` skips missing/non-string response payloads and tiny outputs, and timeout telemetry records `timeout_fail_open`.
  - `PermissionRequest` uses the same Layer-0 deny/ask policy as `slimference filter` and returns Codex's documented allow/deny shape.
  - Local unit coverage added in `cmd/slimference/main_test.go`, `internal/hooks/codex_caps_test.go`, `internal/hooks/status_extra_test.go`, and existing install/verify tests.
  - `go run ./scripts/ci` passes 8/8 with 100.0% total statement coverage after the T135/T136 coverage closure.
- Boundaries:
  - No live Codex CLI/App proof was run in this task. T140 owns live proof for actual hook emission, browser/computer-use gaps, and whether every event reaches current Codex builds as documented.
  - T138 owns durable cross-process session/turn state. The new lifecycle hooks emit the right boundary signals, but no fake shared turn-state protocol is claimed here.
