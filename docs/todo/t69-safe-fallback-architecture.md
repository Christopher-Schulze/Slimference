# T69 - Safe Fallback Architecture + Emergency Off

Status: todo
Priority: P1
Scope: cross-cutting; mostly docs + small code touches in `cmd/slimference/`
Driver: "Wenn der Daemon abkackt oder deaktiviert ist soll der traffic auch
normal durchgehen" - make the failure modes predictable and recoverable.

---

## Problem

The integration introduced by T65-T68 creates dependencies between shell
environment, config files, hooks, launchd, and the running process. Every
one of these must have a clear "get me out" path so the user is never stuck
with a dead daemon and a broken CLI.

## Failure modes and documented recovery

### 1. Daemon process crashes

- **Detection**: launchd re-launches within 2 s (T68 KeepAlive).
- **Client behaviour**: SDK sees one `ECONNREFUSED`, retries, succeeds.
- **User action**: none required.

### 2. Daemon crashes repeatedly (launchd throttles)

- **Detection**: `slimference service status` shows restart_count climbing.
- **Client behaviour**: some requests fail.
- **User action**: `slimference integrate remove` + `exec $SHELL -l` → direct
  to upstream while the user debugs.

### 3. Slimference binary moved / deleted

- **Detection**: launchd can't spawn.
- **Client behaviour**: persistent ECONNREFUSED.
- **User action**: `slimference integrate remove` via the still-installed
  binary OR manually delete `~/.codex/config.toml` block + shell rc block
  + launchctl unload. `docs/integration.md` has the copy-paste commands.

### 4. Want to disable compression without uninstalling

- **Detection**: user decision.
- **Mechanism**: `B` hotkey in TUI (T67) or `slimference bypass on` CLI.
- **Effect**: proxy keeps accepting connections but forwards bytes byte-equal.
- **Recovery**: `B` again or `slimference bypass off`.

### 5. Emergency: "undo everything now"

New CLI command: `slimference integrate emergency-off`.

```
slimference integrate emergency-off
→ stop daemon
→ uninstall launchd plist
→ strip shell rc block
→ strip codex config block
→ strip hooks from claude + codex
→ print: "All Slimference wiring removed. Reload your shell to continue."
```

Idempotent and safe to run from any state. Used as the operator's panic button.

## Implementation Plan

### WP1 - Document the failure-mode table in `docs/integration.md`.
### WP2 - Ship `slimference integrate emergency-off` subcommand (light wrapper over `integrate remove` + `service uninstall`).
### WP3 - Add `slimference bypass on|off|status` CLI mirroring the TUI bypass from T67.
### WP4 - `doctor` reports each failure-mode condition in a new "Fallbacks" section.

## Acceptance Criteria

- [ ] `slimference integrate emergency-off` unwires everything in < 5 s.
- [ ] `slimference bypass` toggles the same state the TUI hotkey does.
- [ ] `doctor` detects common broken states (launchd gone, shell rc mis-edited).
- [ ] `docs/integration.md` contains the full recovery matrix.

## Out of Scope

- Cross-user recovery (single-user product).
- Remote daemon recovery.
