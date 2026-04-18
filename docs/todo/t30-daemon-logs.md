# T30 - `slimference daemon logs` Command (macOS launchd)

Status: open
Priority: low
Scope: cmd/slimference, internal/daemon

---

## Problem

When the proxy runs as a launchd-managed service, its stderr/stdout is
redirected to a file configured in the plist. Today there is no CLI entry
point to tail or inspect that log. Troubleshooting means remembering the file
path by hand.

**Platform scope:** macOS only. No Linux/systemd equivalent is planned.

---

## Desired End State

- `slimference daemon logs` tails the current launchd log (default: follow,
  last 200 lines).
- `slimference daemon logs --since 10m` prints entries newer than a duration.
- `slimference daemon logs --json` emits structured entries when the log is
  slog-JSON.
- `slimference daemon logs --path` prints the log file location and exits.

---

## Work Packages

### WP1 - Log file discovery

- `internal/daemon` already knows the plist path and the redirect target.
- Export a helper `LogFilePath()` returning stdout+stderr locations.

### WP2 - Tail implementation

- Pure Go tail: open file, seek to `len-N*avgLineBytes`, scan forward, then
  watch for new writes using `fsnotify` (already a dependency).
- Handle log rotation: re-open on inode change.

### WP3 - CLI wiring

- New subcommand branch in `cmd/slimference/main.go` under `daemon logs`.
- Flags: `--since duration`, `--json`, `--path`, `--lines N` (default 200).

### WP4 - Tests

- Unit: `tailFile` given a temp file with content and appended lines.
- Unit: JSON-mode passes slog-encoded lines through unchanged.
- Integration: `slimference daemon logs --path` prints the expected path.

---

## Subtasks

- [ ] Implement `LogFilePath()` helper.
- [ ] Implement `tailFile` with fsnotify re-open handling.
- [ ] Wire `daemon logs` CLI with flags.
- [ ] Tests on temp files + integration.
- [ ] Document command in `docs/documentation.md` CLI section.

## Acceptance Criteria

- Running `slimference daemon logs` on a live service streams updates in real
  time until Ctrl-C.
- `--since` and `--json` behave as specified.
- Coverage stays at 100 %.
