# TASK 202: Daemon-lifecycle hosts patching (fail-open guarantee)

Status: PLANNED 2026-05-16
Parent: T200 (Phase H epic)
Scope: `cmd/slimference/phase_g_wiring.go`, `cmd/slimference/main.go`,
       `cmd/slimference/headless.go`, `internal/install/install.go`,
       new `internal/install/hosts_lifecycle.go`

## Why

The user's hard requirement: **daemon down → Codex works normally**.

Today's plan (T201) makes `slimference enable` apply the hosts patch.
That alone is not enough: if the daemon then crashes, hosts is still
pointing at 127.0.0.1 → port 443 nobody is listening → Codex breaks.

Two ways to satisfy fail-open:

1. **Tie hosts patch to listener lifecycle.** Patch hosts on every
   successful listener bind; revert on every shutdown. Crashes are
   covered by launchd KeepAlive restarting the daemon within seconds.
2. **Conditional pfctl rdr rule.** Use firewall NAT redirect that
   evaluates "is port 443 open on 127.0.0.1?" before redirecting. If
   not, traffic flows direct.

Option 2 needs sudo for `pfctl` and is finicky with rule lifetime.
**Option 1 is simpler, sudo-free, and works on macOS without admin
elevation for the hosts patch (yes /etc/hosts needs root to write, but
we have an installer Step that does this once via the launchd label).**

## Target state

### Daemon startup hook

`cmd/slimference/phase_g_wiring.go`:

```go
// After wirePhaseG, before starting transparent.Engine:
if cfg.Transparent.SNIPeekMode {
    if err := installHostsPatch(cfg); err != nil {
        slog.Warn("hosts patch failed - transparent mode degraded",
            "err", err)
        // continue running; just no MITM
    } else {
        startProxyHostsCleanup = func() {
            uninstallHostsPatch(cfg)
        }
    }
}
```

### Daemon shutdown hook

Already exists in `headless.go`:

```go
if startProxySNICancel != nil {
    startProxySNICancel()
}
// NEW: revert hosts before exit
if startProxyHostsCleanup != nil {
    startProxyHostsCleanup()
}
ctx, cancel := context.WithTimeout(...)
defer cancel()
shutdownFn(ctx)
```

### Panic / crash coverage

Go's `defer` does not run on `os.Exit` or hard crashes. Coverage for
those:
- **launchd KeepAlive**: relaunches daemon. On relaunch, daemon's
  startup hook re-applies hosts patch (idempotent). Window of
  exposure: < 1s typically.
- **Process group orphan-cleanup**: not viable on macOS without root.
- **Atomic-write marker**: the hosts patch is marker-fenced. A
  half-written hosts file is impossible because we write to a temp
  file and rename.

### `slimference enable` semantics (revised)

```
slimference enable
  1. Write `cfg.Transparent.SNIPeekMode = true` to config.toml.
  2. If daemon is running: send SIGHUP. The daemon's reload handler
     reads the new flag, applies the hosts patch, starts the engine.
  3. If daemon is not running: print "config armed, start the daemon
     via `slimference service start` to begin intercepting."
  4. Exit 0.

slimference disable
  1. If daemon is running: send SIGHUP. Daemon reverts hosts, stops
     the engine, sets cfg in-memory.
  2. Write `cfg.Transparent.SNIPeekMode = false` to config.toml.
  3. Exit 0.
```

The hosts patch is **never** applied by the CLI directly. The CLI
sets the config and signals the daemon; only the daemon's startup or
SIGHUP path applies it. This keeps the "hosts is daemon-lifecycle"
contract honest.

### Edge case: enable + daemon-not-running

CLI prints a warning AND does not patch hosts. The next time the
daemon starts (via `slimference service start` or boot via launchd), the
hosts patch will be applied. Codex traffic in the gap goes direct -
exactly the fail-open behavior the user wants.

## Implementation plan

1. **New file `internal/install/hosts_lifecycle.go`**:
   - `ApplyHostsPatch(cfg) error` (calls steps.HostsPatch.Apply)
   - `ReverseHostsPatch(cfg) error` (calls steps.HostsPatch.Reverse)
   - Both idempotent.

2. **Hook into `phase_g_wiring.go`**:
   - In `startSNIPeekEngine`: if cfg.Transparent.SNIPeekMode is true,
     call `install.ApplyHostsPatch` BEFORE binding the listener (so
     if hosts fails we don't leave a half-armed state).
   - Return a cleanup func; store in `startProxyHostsCleanup`.

3. **Hook into `headless.go`**:
   - Already calls `startProxySNICancel()` on shutdown.
   - Add `startProxyHostsCleanup()` call right before.
   - On SIGHUP: re-read config; if SNIPeekMode flipped on/off, apply
     or revert hosts accordingly.

4. **SIGHUP reload extension**:
   - Currently SIGHUP only reloads apps.Manager.
   - Extend to also reload the relevant cfg.Transparent fields.
   - **Important**: full config reload is risky (lots of fields). Read
     only the two fields we care about (`SNIPeekMode`, `SNIPeekPort`)
     from the on-disk config, compare to current, take action.

5. **CLI subcommands** (T201 dependency):
   - `enable` / `disable` write the config field via a small
     `config.PatchTransparentSNI(path, enabled bool)` helper that
     surgically edits the TOML (NOT a full rewrite) to preserve
     comments and unrelated settings.
   - Then send SIGHUP via `kill -HUP <pid-from-pidfile>`.

6. **PID file**:
   - `~/.slimference/run/daemon.pid` written by daemon at startup,
     removed on clean shutdown.
   - CLI reads it for SIGHUP and graceful interactions.

7. **Tests**:
   - `internal/install/hosts_lifecycle_test.go`: round-trip
     hosts-apply / hosts-revert on a temp file is byte-equal.
   - `cmd/slimference/phase_g_wiring_hosts_test.go`: starting the
     engine with SNIPeekMode true triggers hosts apply; cancel runs
     the cleanup.
   - SIGHUP reload test: write `SNIPeekMode = false` after enabling
     it, send SIGHUP, observe hosts revert.

## Failure semantics

| Failure mode | Behavior |
|---|---|
| `/etc/hosts` not writable (no root, no admin escalation) | Hosts apply fails; daemon logs warning; engine still binds; sniroute decisions are made but no traffic arrives because hosts is unpatched → effectively passthrough. **No breakage.** |
| Daemon crashes between hosts-apply and shutdown | hosts stays dirty. launchd KeepAlive restarts daemon → idempotent re-apply (no-op) → shutdown-revert on next clean exit. Forensic: backup file in `~/.slimference/backups/hosts.bak.<ts>` preserves pre-patch state. |
| Disk full mid-write | atomic-write helper writes to `hosts.tmp` then rename; on disk-full the rename never happens, hosts stays original. |
| Concurrent CLI `enable` and `disable` | SIGHUP coalescing in the daemon: latest config-disk state wins. CLI is idempotent so double-enable is harmless. |
| User manually edits `/etc/hosts` between our marker fences | We re-apply the patch on next start; the user's edits inside our fences are lost. Edits **outside** our marker fences are preserved (stripManagedBlock idempotency, T196). |

## Acceptance

- Daemon up + SNIPeekMode on + `cat /etc/hosts` shows the marker-fenced
  patch.
- `kill -TERM` the daemon. `cat /etc/hosts` shows the patch is GONE.
  Run Codex → it talks to real chatgpt.com.
- Daemon up + SNIPeekMode off + `cat /etc/hosts` shows NO patch.
- `slimference enable` + send SIGHUP + observe hosts patched within
  100ms.
- `slimference disable` + send SIGHUP + observe hosts reverted within
  100ms.
- Simulated daemon crash (`kill -9`) leaves hosts dirty for the launchd
  KeepAlive interval (default 1s). On restart, daemon re-applies (no
  change); on clean shutdown, daemon reverts.
- Concurrent enable/disable sequence is idempotent (last write wins).

## Sub-Tasks

- [ ] `internal/install/hosts_lifecycle.go`
- [ ] `startProxyHostsCleanup` package-level var + wiring in
      phase_g_wiring.go
- [ ] Shutdown-handler call in headless.go before SNI cancel
- [ ] SIGHUP-reload extension for SNIPeekMode/SNIPeekPort
- [ ] `config.PatchTransparentSNI` surgical TOML editor
- [ ] PID file write + read (`~/.slimference/run/daemon.pid`)
- [ ] Tests for the round-trip + SIGHUP-driven flip

## Notes

- The pidfile is also useful for T201's `status` command.
- Atomic write helper already exists in `internal/control/reversibility/
  steps/hosts_patch.go`. Reuse, don't rebuild.
- `config.PatchTransparentSNI` must be **surgical** (single-key edit),
  per agents.md "no full-file overwrite" rule.

## Deviations

(none yet)
