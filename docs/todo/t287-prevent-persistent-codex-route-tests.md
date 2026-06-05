# TASK 287: Prevent persistent Codex route tests

## Why

Normal `codex` launches must remain direct unless the user explicitly chooses a
Slimference scoped launch path. Leaving `model_provider="slimference-codex"` in
`~/.codex/config.toml` blocks ordinary Codex work when the Slimference daemon is
down and causes reconnect errors. Agents must not use persistent global route
arming as a shortcut for tests or captures.

## Acceptance

- `AGENTS.md` forbids agents from enabling or leaving a persistent
  Slimference-Codex route for tests, captures, or convenience.
- The documented allowed test path is scoped: `slimference codex run -- ...` or
  the Launch Center/TUI Slimference launch.
- The live local Codex config is verified as route-disabled so ordinary
  terminal `codex` starts direct again.

## Sub-Tasks

- [x] Verify current `~/.codex/config.toml` route state.
- [x] Add the persistent-route prohibition to `AGENTS.md`.
- [x] Re-verify `slimference codex status` reports `enabled=false`.

## Notes

- Live status before this task: `slimference codex status` reported
  `enabled=false`.
- `~/.codex/config.toml` contains only an old commented Slimference URL note;
  no active `model_provider="slimference-codex"` route remains.

## Deviations

- None.
