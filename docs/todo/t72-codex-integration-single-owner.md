# T72 - Codex Integration Single Owner and Hook Drift Repair

Status: done
Priority: P0
Scope: `internal/hooks/codex.go`, `internal/integrate/codex_toml.go`, `internal/integrate/detect.go`, `cmd/slimference/{hook,integrate}_cmd.go`, `docs/integration.md`
Driver: Deep audit found two competing Codex config writers and partial live hook state.

---

## Problem

Codex integration is split across two ownership paths:

- `slimference hook install codex` in `internal/hooks/codex.go` writes hook
  scripts and patches `~/.codex/config.toml` with the older
  `openai_base_url` + `codex_hooks` behavior.
- `slimference integrate install --client codex` in `internal/integrate/`
  writes the newer fenced block with both `openai_base_url` and
  `chatgpt_base_url`.

This creates drift risk. A user can run one command and believe Codex is fully
wired while only part of the current required state exists. The local machine
already shows this pattern: Codex hooks are present, config is not wired, and
the Read hook entry is absent from `~/.codex/hooks.json`.

## Target State

There is one canonical Codex integration implementation:

- Both `hook install codex` and `integrate install --client codex` converge on
  the same config block and hook manifest shape.
- Codex config always uses a fenced block with both:
  - `openai_base_url`
  - `chatgpt_base_url`
- Hook status verifies all expected Codex hook entries:
  - PreToolUse Bash rewrite guard
  - PostToolUse Bash output filter
  - PreToolUse Read cache hook
- Remove/emergency-off remove the same artifacts they install.

## Implementation Plan

### WP1 - Choose the canonical owner
- Make `internal/integrate` own config TOML editing.
- Make `internal/hooks` own only hook script and hooks.json editing.
- `cmd/slimference` command handlers call the same shared helpers so behavior
  cannot diverge again.

### WP2 - Repair `hook install codex`
- Stop writing legacy top-level `openai_base_url` outside the fenced block.
- Add `chatgpt_base_url`.
- Preserve comments and unrelated keys.
- Refuse conflicting non-Slimference base URLs with a clear remediation message.

### WP3 - Repair detection and verify
- `DetectCodex` must check:
  - config block present
  - both base URLs present inside the block
  - PreToolUse Bash hook present
  - PostToolUse Bash hook present
  - Read hook present
  - hook script files exist and are executable
- `hook verify codex` must fail hard if any one of those is missing.

### WP4 - Migration from old installs
- Detect old installs that have:
  - `codex_hooks = true` only
  - Pre/Post hooks only
  - no `chatgpt_base_url`
  - legacy `AGENTS.md` block only
- `integrate install --client codex --force` should upgrade them into the
  canonical state without dropping unrelated user config.

### WP5 - Tests
- Temp-home tests for every old install shape.
- Idempotence tests: install twice, byte-equal second run.
- Remove tests: every Slimference-owned artifact removed, unrelated hooks kept.

## Acceptance Criteria

- [x] `hook install codex` and `integrate install --client codex` produce the
      same final Codex config and hook state.
- [x] `DetectCodex` reports partial state when any expected hook or base URL is
      missing.
- [x] `hook verify codex` fails when the Read hook is absent.
- [x] Old partial installs migrate cleanly with `--force`.
- [x] Remove/emergency-off cleanly remove all Slimference-owned Codex artifacts.
- [x] `go test -race ./internal/hooks/... ./internal/integrate/... ./cmd/slimference/...` green.

## Out of Scope

- Editing Codex auth files.
- Deleting unrelated user hooks.
- Supporting non-Codex tools in this task.

## Validation

```
go test -race ./internal/hooks/... ./internal/integrate/... ./cmd/slimference/...
slimference integrate install --client codex --dry-run --json
slimference hook verify codex
slimference integrate remove --client codex --dry-run --json
```

## Notes

- 2026-04-29: Started implementation. Initial code read confirms the split:
  `internal/hooks/codex.go` still patches `config.toml` with legacy
  `openai_base_url` + `codex_hooks`, while `internal/integrate/codex_toml.go`
  owns the current fenced `openai_base_url` + `chatgpt_base_url` block.
- 2026-04-29: Completed. `internal/hooks` now owns only hook scripts and
  `hooks.json`; `cmd/slimference` wires Codex hooks through
  `installCodexIntegrationHook` / `removeCodexIntegrationHook`, which call
  the canonical `internal/integrate` Codex block writer/remover. Detection now
  requires the complete fenced config plus executable PreToolUse Bash,
  PostToolUse Bash, and PreToolUse Read hook artifacts. `hook verify codex`
  now verifies only Codex, instead of failing on unrelated missing Claude
  hooks.
- Validation passed:
  `go test -race ./internal/hooks/... ./internal/integrate/... ./cmd/slimference/...`;
  temp-home `go run ./cmd/slimference hook install codex`;
  temp-home `go run ./cmd/slimference hook verify codex`;
  temp-home `go run ./cmd/slimference integrate install --client codex --dry-run --json`;
  temp-home `go run ./cmd/slimference integrate remove --client codex --dry-run --json`.
