# TASK 244: Daemon lifecycle and atomic install hardening

Status: DONE - atomic install, daemon lifecycle hardening, stale-process
classification, product-level Manage wording, and live restart evidence landed
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
- The Launch Center / Manage Slimference surfaces daemon health without
  frightening the user with irrelevant old stuck processes. Old `U`/`UE`
  processes are reported as "requires reboot to clear" when detected, while the
  current healthy daemon PID remains the actionable state.
- Build/install/restart documentation must distinguish:
  - developer local rebuild;
  - product repair/restart;
  - release certification ceremony;
  - reboot-only cleanup for already stuck macOS processes.
- `docs/operation-log.md` records the exact live failure class, current stuck
  PIDs if still present, the atomic-install fix, and remaining lifecycle work.
- T240 release certification includes one rebuild/install/restart proof after
  this task is complete.

## Sub-Tasks

- [x] Replace in-place installed-binary overwrite in `scripts/build` with
  same-directory temp-file plus atomic rename.
- [x] Add build-helper tests for executable replacement and temp cleanup.
- [x] Reject temporary `go run` executables during `slimference install` plan
  resolution and add `--binary=PATH` for explicit stable hook/launchd targets.
- [x] Add daemon lifecycle timeout/diagnostic hardening for `start`, `stop`,
  and restart flows.
- [x] Add a release-safe rebuild command or documented ceremony that avoids
  racing the daemon against binary replacement.
- [x] Add Manage Slimference "Restart daemon" / "Repair daemon" wording that
  uses the hardened lifecycle path and never starts duplicate daemons.
- [x] Ensure install/repair lifecycle state is product-level, not per-app:
  `installed/prepared` covers Codex CLI and Desktop support together, while
  route capability states live under Status.
- [x] Add stale process classifier for old `dyld_start` / uninterruptible
  process evidence: report, do not retry-loop, recommend reboot only when the
  current daemon is healthy but old kernel-state processes remain.
- [x] Add live macOS evidence: build, install, restart daemon, run
  `slimference version`, confirm no new stuck processes.
- [x] Decide whether the old `slimference.dyld-stuck-*` file should be
  deleted after reboot, then document the cleanup command.

## Notes

The atomic install fix addresses the most likely root cause: in-place
truncation/copy of the installed Mach-O while a process starts from that path.
It does not magically kill already stuck kernel-level uninterruptible processes.
Those remain an operator cleanup/reboot concern and must be reported honestly
until gone.

This task is separate from Codex routing. It is release hygiene: no user should
have to understand dyld, PIDs, or half-copied binaries to update Slimference.

Install UX is also deliberately unified at this lifecycle layer. A normal
install prepares Slimference for Codex as a product, including CLI launch,
Desktop launch/probe support, daemon/autostart, bounded logs, and repair state.
It must not create default CLI-only/Desktop-only partial installs. Desktop
capability remains a proof-gated route state, not a separate install state. When
the stored T246/T247 proof is current, Desktop can launch through Slimference;
when proof is stale or invalid, Status/Manage must report the exact reason while
the installed product state remains single and repairable.

T244 is also the guard against false debugging. If the current daemon is healthy
and old stuck processes consume 0 CPU, the product should say exactly that
instead of forcing unnecessary repair or killing attempts that cannot work in
macOS uninterruptible state.

2026-05-29 portable-install slice: install planning now refuses default
`os.Executable()` paths that look like temporary Go build artifacts, because a
fresh developer running `go run ./cmd/slimference install` would otherwise write
hooks and launchd plists pointing at a soon-deleted temp binary. Operators can
still pass an explicit `--binary=PATH` override when they intentionally want a
non-default executable path. Source-checkout installs should build a stable
binary first, then run `~/.local/bin/slimference install`.

2026-05-29 lifecycle-hardening slice: direct `start`, `service install`, and
TUI/adapter daemon starts now reject temporary Go build executable paths before
spawning or registering a daemon. Restart paths now surface daemon state check
errors instead of ignoring them. `StopDaemon` still uses bounded SIGTERM wait
and now reports a hard failure if SIGKILL also leaves the process alive,
explicitly naming the macOS `U`/`UE` / `dyld_start` reboot-only class. The build
helper has `--restart`, which performs the safe local update ceremony:
installed daemon stop -> build -> atomic install -> installed daemon start.

2026-05-29 final lifecycle slice: human `status` and Manage Slimference now
classify old stuck Slimference processes by `ps` state/argv and report them as
reboot-only stale evidence instead of daemon-health failures. Manage wording now
shows restart as the daemon repair action and states that product install
prepares Codex CLI and Desktop together; route and Desktop savings capability
remain Status facts. Cleanup decision: do not delete
`~/.local/bin/slimference.dyld-stuck-*` before reboot while a process might still
hold it; after reboot and a clean `ps` check, remove it with
`rm -f ~/.local/bin/slimference.dyld-stuck-*`.

Final live proof on 2026-05-29: `go run ./scripts/build --restart` stopped PID
8985, built, atomically installed to `~/.local/bin/slimference`, and started PID
11348. Installed `slimference version` returned `v2.0.2`; `status --preflight`
reported daemon `health=true`, `:8990=true`, hosts inactive, Codex auto
`wss_certified=true`; `codex desktop status --json` returned
`desktop_app_server_proven` with last proof `desktop_app_server_phasef_proven`.
`ps -axo pid=,stat=,args=` showed no Slimference process with `U` state.

## Deviations

None yet.
