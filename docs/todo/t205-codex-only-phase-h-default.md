# TASK 205: Codex-only Phase H default

Status: DONE 2026-05-17
Priority: P0
Scope: `internal/install/`, `cmd/slimference/`, `internal/tui/`, `docs/install.md`, `AGENTS.md`

## Why

The active product target is Codex CLI first and Codex Desktop next. Claude Code must remain in the tree but default-off. The default install and routing surface must not touch Claude files or Anthropic hosts unless the operator explicitly opts in.

## Acceptance

- `slimference install` installs Codex hooks and Codex notice only by default.
- `--with-claude` is accepted only as a parked compatibility no-op; Claude
  hook and notice steps are not part of the product plan.
- Default transparent hosts targets are Codex-only: `chatgpt.com`, `api.openai.com`.
- TUI visible text and status panels present Codex as the active target and Claude as off / opt-in.
- Generated `SLIMFERENCE.md` notice points to local docs only, not GitHub.

## Sub-Tasks

- [x] Add `install.Options.WithClaudeHooks`.
- [x] Remove `hooks.claude` and `notice.claude` from default `install.Plan()`.
- [x] Add CLI parsing and help text for `--with-claude` as a parked no-op.
- [x] Change `DefaultHostsTargets()` to Codex-only.
- [x] Change `root-arm` generated hosts block to Codex-only.
- [x] Update TUI copy and Claude toggle behavior to default-off.
- [x] Update `docs/install.md` and `AGENTS.md` to Codex-only default.
- [x] Add and update tests for default vs opt-in plan composition.

## Verification

- `go test ./internal/install/... ./docs/ -count=1 -timeout 120s`
- `go test ./internal/tui ./cmd/slimference -count=1 -timeout 180s`
- `go run ./scripts/ci` — passes all 8 steps with formal `100.0%`
  statement coverage.
- Targeted touched-package race check:
  `go test ./internal/proxy ./internal/summarization ./internal/filter ./internal/transparent ./internal/control/apps ./internal/install/installsteps ./internal/tui -race -count=1 -timeout 300s`

## Notes

Claude Code support is intentionally not deleted. T217 parks every public
Slimference activation path; RTK is the Claude Code optimizer for now.
