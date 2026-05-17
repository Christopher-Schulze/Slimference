# TASK 212: Claude Code max hook mode, default-off

Status: PARKED by T217 (2026-05-17)
Priority: none for Slimference product path; use RTK for Claude Code
Scope: `internal/hooks/claude.go`, `internal/filter`, `cmd/slimference hook|rewrite|readhook`, `internal/tui`, install opt-in docs/tests

## Why

Claude Code can support the strongest RTK-style optimization because its PreToolUse hook can transparently rewrite Bash input through `hookSpecificOutput.updatedInput`. That lets Slimference filter command output before it enters the conversation, which is the highest-leverage token-saving position.

The product mandate is now Codex-only for Slimference. Claude Code must stay
untouched by `slimference install`, including when old scripts pass
`--with-claude`. The implementation artifacts remain in-tree for reference,
but public product entrypoints are parked by T217.

## Target State

Claude Code has a complete, opt-in maximum mode:

- Bash PreToolUse rewrites every safe filterable shell command to `slimference filter`.
- Compound commands are rewritten segment-by-segment.
- Commands after pipes are handled conservatively, matching the existing rewrite safety model.
- Read tool hook uses Slimference read-cache/delta/aggressive-read modes where safe.
- Grep/Glob/LS-style Claude tools are covered if Claude exposes hook matchers for them. If Claude does not expose them, the task documents that exact verified limitation and falls back to Bash/wrapper guidance.
- Permission policy supports deny/ask/fail-open without breaking Claude.
- Raw recovery remains available through tee/archive/expand.
- Metrics show rewritten, passed-through, denied, ask-required, read-delta, read-unchanged, and parse-failure counts.
- TUI shows Claude as opt-in and not currently armed unless both hooks and host/policy prerequisites exist.

## Maximum-Possible Check

This task must not stop at the existing Bash + Read hook if Claude exposes more tool surfaces.

Verify:

- Current Claude Code hook contract and local settings shape.
- Whether matchers exist for Bash, Read, Grep, Glob, LS, WebFetch, and any filesystem search/read tools.
- Which tools can return `updatedInput`.
- Which tools can return a replacement result rather than input rewrite.
- Whether Claude built-in Read/Grep/Glob bypass Bash and therefore need dedicated hooks.
- Whether the hook payload includes enough path/command arguments to apply existing Layer-0 filters safely.
- Whether max mode causes visible chat noise; default must remain silent/fail-open.

## Acceptance

- `slimference install` still does not touch Claude by default.
- `slimference install --with-claude` is a parked compatibility no-op.
- A new explicit max-mode path is designed and, if implemented here, guarded by config or flag. No accidental default activation.
- Every hook script degrades to no output / exit 0 on parse errors unless policy explicitly blocks.
- The Claude Bash rewrite path is at least RTK-equivalent for compound commands and opt-out behavior.
- Dedicated non-Bash Claude tool hooks are added only when the verified Claude contract supports them.
- Tests prove max mode can be installed, verified, removed, and does not affect unrelated existing Claude settings.

## Sub-Tasks

- [x] Verify the current Claude Code hook contract from official docs: `PostToolUse` can return `updatedToolOutput`; Bash output replacement must preserve Bash output shape (`stdout`, `stderr`, `interrupted`, `isImage`).
- [x] Confirm Bash rewrite parity scope with RTK: compounds/pipes/env/absolute paths covered; configurable transparent prefixes remain a future RTK-delta item in T211, not needed for Codex-first T209.
- [x] Keep existing Claude Read hook safe and fail-open; no unverified Grep/Glob/LS hooks added.
- [x] Add default-off max-mode env naming: `SLIMFERENCE_CLAUDE_HOOK_MODE=max|compact|aggressive|auto`.
- [x] Retain `claudeposttool` handler code for reference, but park the public
  top-level command in T217.
- [x] Add metrics under `hook_post_claude` flight records and `claude_posttool_updated_output` decision entries.
- [x] Update docs/TUI wording: Claude row remains parked during Codex-only testing.
- [x] Add install/remove/verify/script-shape and handler tests.

## Verification

- `go test ./internal/hooks ./internal/filter ./cmd/slimference -run 'TestClaude|TestRewrite|TestReadHook|TestHook' -count=1 -timeout 120s`
- `go test ./internal/tui -run 'Test.*Claude|Test.*Apps' -count=1 -timeout 120s`
- `go test ./docs -count=1`

## Notes

Claude Code remains out of the live T209 Codex test and out of the Slimference
product path. T217 parks public activation; RTK is the Claude Code optimizer
for now.

Implemented files: `internal/hooks/claude.go`, `internal/hooks/verify.go`, `cmd/slimference/main.go`, `cmd/slimference/help.go`, `cmd/slimference/completion.go`, docs.
