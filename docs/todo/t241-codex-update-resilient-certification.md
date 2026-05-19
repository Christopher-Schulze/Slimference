# TASK 241: Codex update-resilient certification

Status: PLANNED
Priority: P0 after T238 hardening, before T240 release seal
Scope: Codex CLI WSS auto-promotion resilience across Codex CLI and
Slimference updates

## Why

The WSS cert deliberately binds to the Codex CLI version and Slimference
version. That is the correct zero-drawdown safety guard: after `codex-cli`
updated from 0.130.0 to 0.131.0, `transport=auto` fell back to HTTP instead of
silently running an uncertified WSS parser.

The missing product layer is not "ignore drift". The missing layer is a smooth
recertification path so updates do not surprise the user or leave savings off
longer than necessary.

## Acceptance

- `slimference codex status` and the TUI clearly show version drift, the old
  tuple, the current tuple, and the exact recert action.
- `slimference codex run --transport=auto` remains safe: never WSS after tuple
  drift unless a new cert exists.
- A guided recert flow triggers a deterministic mutation-producing Codex CLI
  session, verifies `parse_failures=0`, `degraded_sessions=0`,
  `compression_errors=0`, `frames_reencoded>0`, and
  `compressed_messages_mutated>0`, then runs `slimference codex certify wss`.
- The recert flow leaves `~/.codex/config.toml` bit-identical after disable.
- Failure falls back to HTTP and reports why without breaking Codex CLI output.
- Operation log records version before/after, cert path, counters, binary SHA,
  config hash, and final auto decision.

## Sub-Tasks

- [x] Add a `needs_recert` field to Codex status JSON with current/expected
  Codex and Slimference versions.
- [ ] Add a one-command guided recert entry point, likely
  `slimference codex recertify wss`, that drives a known mutation trigger and
  then calls the existing certify path.
- [ ] Keep certify criteria unchanged; do not lower the gate for convenience.
- [ ] Add TUI Status/Manage wording for "WSS savings paused after Codex update"
  with the exact repair action.
- [ ] Add tests for Codex drift, Slimference drift, missing daemon, mutation
  not observed, and successful recert.
- [ ] Run one live recert against the current `codex-cli 0.131.0` and append
  evidence.

## Notes

This task makes updates boring. It must not weaken the version tuple guard.
The desired behavior is: updates never make Codex worse; they may temporarily
drop WSS savings until the new tuple is proven.

2026-05-19 status hardening landed:

- `codexroute.AutoDecision` now carries the current Codex/Slimference tuple,
  the certified tuple, `needs_recert`, and `recert_command`.
- `slimference codex status --json` exposes those fields through its existing
  `auto` object.
- Human `slimference codex status` prints the current tuple, certified tuple,
  fallback reason, and recert action when version drift pauses WSS savings.
  This keeps the strict tuple guard intact while making the repair path visible.

## Deviations

None yet.
