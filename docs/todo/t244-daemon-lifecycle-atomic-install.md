# TASK 244: Daemon lifecycle and atomic install hardening

Status: OPEN - atomic build install is landed; daemon restart/stop hardening and
release evidence remain
Priority: P0 before T240 release seal
Scope: local macOS arm64 developer/product lifecycle for rebuilding, installing,
stopping, starting, and restarting Slimference without stranding daemon or
control-command processes

## Why

During T241/T243 live verification, `go run ./scripts/build --install`
overwrote `~/.local/bin/slimference` while Slimference commands were being
started or stopped. Several processes became stuck in macOS `dyld_start`
uninterruptible state:

- `/Users/christopher/.local/bin/slimference stop`
- `/Users/christopher/.local/bin/slimference start`
- `/Users/christopher/.local/bin/slimference daemon`
- `/Users/christopher/.local/bin/slimference version`

The product path still recovered by moving the damaged installed binary aside
and copying a fresh repo binary, but this is not acceptable release ergonomics.
Rebuild/install must be boring: no partially overwritten executable can be
observed by new processes, and daemon lifecycle commands must time out,
diagnose, and recover without hanging the operator.

## Acceptance

- `scripts/build --install` installs by writing the new binary to a temporary
  file in the destination directory, syncing it, chmodding executable, closing
  it, and atomically renaming it over `~/.local/bin/slimference`.
- The installed binary is never truncated in place. A concurrently starting
  `slimference version`, `start`, `stop`, or `daemon` process should see either
  the old complete binary or the new complete binary.
- Temporary install files are removed on copy, chmod, sync, close, or rename
  failure.
- The build helper has tests for executable mode, content replacement, and no
  leftover temporary files.
- Daemon lifecycle commands have bounded waits and useful diagnostics:
  - `stop` cannot wait forever;
  - `start` refuses stale/unhealthy PID state with a clear reason;
  - `restart` or documented rebuild flow does stop/build/install/start in a
    safe order;
  - stuck uninterruptible macOS processes are classified explicitly as OS-level
    `dyld_start` hang requiring reboot, not silently retried.
- `docs/operation-log.md` records the exact live failure class, current stuck
  PIDs if still present, the atomic-install fix, and remaining lifecycle work.
- T240 release certification includes one rebuild/install/restart proof after
  this task is complete.

## Sub-Tasks

- [x] Replace in-place installed-binary overwrite in `scripts/build` with
  same-directory temp-file plus atomic rename.
- [x] Add build-helper tests for executable replacement and temp cleanup.
- [ ] Add daemon lifecycle timeout/diagnostic hardening for `start`, `stop`,
  and restart flows.
- [ ] Add a release-safe rebuild command or documented ceremony that avoids
  racing the daemon against binary replacement.
- [ ] Add live macOS evidence: build, install, restart daemon, run
  `slimference version`, confirm no new stuck processes.
- [ ] Decide whether the old `slimference.dyld-stuck-*` file should be
  deleted after reboot, then document the cleanup command.

## Notes

The atomic install fix addresses the most likely root cause: in-place
truncation/copy of the installed Mach-O while a process starts from that path.
It does not magically kill already stuck kernel-level uninterruptible processes.
Those remain an operator cleanup/reboot concern and must be reported honestly
until gone.

This task is separate from Codex routing. It is release hygiene: no user should
have to understand dyld, PIDs, or half-copied binaries to update Slimference.

## Deviations

None yet.
