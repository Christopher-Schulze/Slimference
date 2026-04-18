# T33 - Hook Drift Detection Watchdog

Status: open
Priority: medium
Scope: internal/hooks, cmd/slimference (`hook verify`, new `hook check-upstream`)

---

## Problem

Hook integration relies on **static contracts** with Claude Code and Codex
CLIs:

- Claude Code: `~/.claude/hooks/*.sh` + `hookSpecificOutput` JSON schema.
- Codex: `~/.codex/hooks.json` PreToolUse/PostToolUse + `.codex/config.toml`
  patches (openai_base_url, [features] codex_hooks=true).

Both upstream CLIs evolve. A quiet field rename, a new required parameter, or
a changed output shape silently breaks our integration. We currently only
catch it when a user complains.

---

## Desired End State

A watchdog that runs as part of `slimference doctor` and on an optional cron
timer:

1. Detect the installed upstream CLI versions.
2. Compare against a known-good version map in `internal/hooks/compat.go`.
3. Run a real invocation of the hook binary under controlled input and
   compare output shape against a **contract fixture** stored in
   `internal/hooks/testdata/contracts/`.
4. Report drift clearly, with actionable next steps.

Fixtures are **generated, not hand-written**: `slimference hook capture`
records a real invocation into the fixtures tree. Regenerating on a new CLI
version produces a diff that a human reviews.

---

## Work Packages

### WP1 - Version detection

- `claude --version`, `codex --version` parsing in `internal/hooks`.
- Known-good version map keyed by CLI, with notes on breaking changes.

### WP2 - Contract fixtures

- `internal/hooks/testdata/contracts/claude_pretool.json`,
  `claude_posttool.json`, `codex_pretool.json`, `codex_posttool.json`.
- JSON Schema or a sample payload + expected reply structure.

### WP3 - Live probe

- `slimference hook check-upstream` runs each supported CLI with a dummy
  payload, captures the response, and structurally diffs against the
  fixture.
- Output: per-hook OK / drift report with exact field paths.

### WP4 - Integration into `doctor` and CI

- `slimference doctor` calls the probe and surfaces issues.
- Optional opt-in in `scripts/ci`: run probe on every PR against a pinned
  upstream CLI version.

### WP5 - Regenerate workflow

- `slimference hook capture` writes new fixtures, emits a diff summary.
- Human reviews and commits the fixture change.

---

## Subtasks

- [ ] Version detection helpers + known-good map.
- [ ] Capture initial contract fixtures from current CLI versions.
- [ ] Implement `hook check-upstream` live probe.
- [ ] Wire into `doctor`.
- [ ] Document the regenerate workflow.
- [ ] Add golden-file test for probe output.

## Acceptance Criteria

- A synthetic change to the fixture (simulating upstream drift) is detected
  and reported by the probe.
- `slimference doctor` surfaces drift with zero noise when none is present.
- Coverage stays at 100 %.
