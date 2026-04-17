# T15 - Daemon Service Productionization

Status: open
Priority: high
Scope: `internal/daemon/*`, service install/remove flow, local secret handling

---

## Problem

The current service path is not safe enough for production:

- launchd plist generation persists `MINIMAX_API_KEY` in plaintext
- install/remove behavior is only partially implemented
- the code and comments do not yet match the true service lifecycle

---

## Desired End State

1. No plaintext MiniMax secret is written into a launchd plist.
2. Service install, load, unload, start, stop, and removal are real and tested.
3. The daemon feature is honest about prerequisites and failure modes.

---

## Work Packages

### WP1 - Secret-handling model

Choose and implement one production-worthy approach:

- macOS Keychain-backed lookup
- dedicated local env file with `0600` permissions and explicit ownership
- another approach that avoids plaintext plist persistence

The selected approach must be documented and testable.

### WP2 - Real launchctl lifecycle

- implement load/bootstrap on install
- implement unload/bootout on remove
- verify status and error handling
- remove placeholder-only behavior

### WP3 - Permission and file-mode hardening

- launch agent files
- pid/log paths
- secret material files if used

### WP4 - Test coverage

- plist generation tests
- secret-handling tests
- install/remove lifecycle tests with injected command runner

---

## Design Rule

If secure secret handling cannot be implemented cleanly in one pass, the daemon
must fail safely and clearly instead of pretending to be production-ready.

---

## Subtasks

- [ ] Replace plaintext plist secret persistence with a secure model.
- [ ] Implement real launchctl lifecycle behavior.
- [ ] Add injectable command execution for deterministic tests.
- [ ] Add permission/mode tests for generated files.
- [ ] Document operational constraints and supported platforms.

---

## Acceptance Criteria

- Installing the service does not persist the MiniMax API key in plaintext into
  a world-readable or user-readable plist.
- Install/remove behavior matches the code comments and user-facing behavior.
- The daemon path has enough tests to justify production use.
