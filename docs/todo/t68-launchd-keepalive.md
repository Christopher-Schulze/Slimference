# T68 - launchd KeepAlive + Health-Aware Restart

Status: todo
Priority: P1
Scope: `internal/daemon/`, `scripts/service/macos/`
Driver: "Daemon abkackt → Traffic läuft direkt durch" - requires daemon to
restart in under a second so the SDK retry covers the gap.

---

## Problem

Current `slimference service install` writes a launchd plist with
`RunAtLoad=true` but no `KeepAlive`. If the proxy process crashes:

- launchd does not restart it.
- Clients keep hitting `127.0.0.1:8990` and see `ECONNREFUSED` forever.
- User notices only when a request fails and must manually `slimference service
  restart`.

Claude Code's SDK retries 3x with exponential backoff on `ECONNREFUSED`, so a
sub-2 s restart window is enough to make a crash invisible.

## Target State

launchd plist shipped by `slimference service install` contains:

```xml
<key>KeepAlive</key>
<dict>
    <key>SuccessfulExit</key>
    <false/>
    <key>Crashed</key>
    <true/>
</dict>
<key>ThrottleInterval</key>
<integer>2</integer>
```

- Restart on crash.
- Don't restart on `exit 0` (clean shutdown via SIGTERM from the user).
- 2 s minimum throttle so a pathological crash-loop does not hammer the CPU.

Post-install health probe:

```
slimference service install
→ plist written
→ launchctl load
→ wait up to 10 s for /admin/health 200
→ report restart count + current uptime
```

`slimference service status` now reports:

```
Slimference daemon
  state:         running
  pid:           78432
  uptime:        04:13:22
  launchd_id:    com.slimference.proxy
  restarts:      2  (last: 14:02:11)
  keepalive:     enabled
  health:        http 200 in 3.2 ms
```

## Implementation Plan

### WP1 - Update plist template in internal/daemon with the KeepAlive block.
### WP2 - Parse `launchctl list` output to extract restart count + uptime.
### WP3 - Extend `slimference service status` renderer.
### WP4 - Health probe helper in internal/daemon + call it post-install.
### WP5 - Tests via table-driven `launchctl list` stdout fixtures.

## Risks

- Users on Linux (systemd unit already has restart-on-failure via T48) stay
  unaffected - this task touches macOS only.
- If the proxy crashes during its own `Shutdown` path, KeepAlive won't fire
  (exit 0). Acceptable: user initiated the stop.

## Acceptance Criteria

- [ ] Plist carries KeepAlive{Crashed=true, SuccessfulExit=false}.
- [ ] Install runs health probe + reports.
- [ ] Status reports restart count + uptime.
- [ ] Kill -9 the daemon → relaunch within 2 s measured by a test.
- [ ] `slimference service stop` cleanly stays stopped.

## Out of Scope

- Cross-platform supervisor abstraction (macOS launchd only; Linux systemd has
  its own task T48).
