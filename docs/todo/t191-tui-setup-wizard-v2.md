# TASK 191: TUI Setup Wizard v2 (install/status/per-app/uninstall)

Status: PLANNING 2026-05-16
Priority: P0 (the user-facing surface for the whole Phase G feature)
Scope: `internal/tui/`, `cmd/slimference/proxy_cmd.go`, `cmd/slimference/
       integrate_cmd.go`, new `internal/control/` (consolidated state),
       refresh of `docs/transparent-mode.md`

## Why

The user described the desired UX:

> "Es muss sowieso so eine Art Proxy haben, dass die TUI, die wir haben, die
> Binary, so eine Art Proxy installieren kann, Status checken kann, kann ihn
> entfernen, kann dann sagen, für welche Anwendungen der laufen soll, welche
> Anwendungen nicht laufen soll, oder kann alles aktivieren in jeder
> Anwendung."

Existing T133 ("TUI daemon control plane") built v1 of this surface. v1 is
based on the system-HTTPS-proxy model that does not work for Codex 0.130's
WebSocket transport (T187). v2 refactors:

- around the transparent :443 listener (not the system HTTPS proxy).
- with explicit per-app state (Codex CLI / Codex Desktop App / Claude Code).
- with the Phase F mechanism statistics inline (output_reduce_counters
  + qualityab cohort).
- with the indistinguishability proof (T190) embedded as a status check.

## Target state - install/uninstall flow

```
$ slimference  (or  slimference setup)

╔══════════════════════════════════════════════════════════════════╗
║ Slimference — Setup                                              ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║ Slimference intercepts Codex CLI / Codex Desktop / Claude Code   ║
║ traffic locally to reduce LLM token usage by 30-50 %.            ║
║                                                                  ║
║ This setup will:                                                 ║
║   1. Generate a local TLS certificate authority.                 ║
║   2. Add it to macOS Keychain as trusted (prompts sudo).         ║
║   3. Install a launchd service for the proxy daemon.             ║
║   4. Configure /etc/hosts to redirect chatgpt.com & friends      ║
║      to 127.0.0.1 (only while the proxy runs).                   ║
║                                                                  ║
║ Reversible at any time via [U]ninstall in this TUI.              ║
║                                                                  ║
║ Per-app activation (you can change later):                       ║
║   [x] Codex CLI                                                  ║
║   [x] Codex Desktop App                                          ║
║   [ ] Claude Code (defaults off; toggle later)                    ║
║                                                                  ║
║                                                                  ║
║       [ Install ]      [ Cancel ]                                ║
╚══════════════════════════════════════════════════════════════════╝
```

After install, the dashboard view is the user-facing console:

```
╔══════════════════════════════════════════════════════════════════╗
║ Slimference — Dashboard                          (v2.0.3)        ║
╠══════════════════════════════════════════════════════════════════╣
║                                                                  ║
║ ▸ Setup                                                          ║
║     CA installed                  ✓  ( Keychain trusted, exp     ║
║                                       2027-05-16 in 1y 12m )    ║
║     Daemon (launchd)              ✓  ( running pid 40860, RSS    ║
║                                       142 MB )                  ║
║     Transparent listener          ✓  ( 127.0.0.1:443 )          ║
║     Network redirect              ✓  ( hosts: chatgpt.com,       ║
║                                       api.openai.com )           ║
║     Indistinguishability proof    ✓  ( codex_cli_rs_0_130        ║
║                                       golden 2026-05-16 )       ║
║                                                                  ║
║ ▸ Per-app integration                                            ║
║     [x] Codex CLI                 ✓  intercepting                ║
║                                       routed: 412 conv, 19 stale ║
║     [x] Codex Desktop App         ✓  intercepting                ║
║                                       routed: 87 conv, 5 stale  ║
║     [ ] Claude Code               -  not enabled                ║
║                                                                  ║
║ ▸ Today's savings                                                ║
║     Input tokens saved              412 318  (-31 %)             ║
║     Output tokens saved              94 207  (-22 %)             ║
║     Total cost savings (est.)         $7.84                      ║
║     Streamcut fires                       11                     ║
║     Repdet rewrites                      37  (44 102 bytes)      ║
║     Stale-read aging                     124 blocks              ║
║     Obsolete-prune                        29 blocks              ║
║     Be-terse cohort (treatment vs control)                       ║
║                              failures   8 % vs   6 %  ⚠ delta 2pp║
║     Quality A/B rolled back        no                            ║
║                                                                  ║
║ [I] Install/repair    [U] Uninstall    [S] Stats detail          ║
║ [A] Per-app config    [P] Probe        [L] Logs                  ║
║ [R] Reload config     [Q] Quit                                   ║
╚══════════════════════════════════════════════════════════════════╝
```

## State model (`internal/control/`)

```go
type SetupState struct {
    CA            CAState
    Daemon        DaemonState
    Listener      ListenerState
    NetworkRedir  NetworkRedirState
    Indist        IndistState
    Apps          map[AppID]AppState
    Savings       SavingsSnapshot
    LastError     string
    UpdatedAt     time.Time
}

type CAState struct {
    Installed       bool
    InKeychain      bool
    Fingerprint     string
    NotBefore       time.Time
    NotAfter        time.Time
    DaysUntilExpiry int
}

type DaemonState struct {
    Installed   bool      // launchd plist exists
    Autostart   bool      // launchd Disabled flag
    Running     bool
    PID         int
    HealthOK    bool
    RSSBytes    int64
    UptimeSec   int64
}

type ListenerState struct {
    BoundOn443  bool      // direct or via pfctl rdr
    Method      string    // "privileged-port", "pfctl-rdr", "alt-port"
    BoundOn8990 bool      // legacy
}

type NetworkRedirState struct {
    HostsActive bool
    HostsEntries []string  // ["chatgpt.com", "api.openai.com"]
    PFCtlActive bool
    PFCtlRules  []string
}

type IndistState struct {
    GoldenLocked bool
    GoldenSHA    string
    LastVerified time.Time
    Drift        []string // empty if OK
}

type AppID string
const (
    AppCodexCLI     AppID = "codex_cli"
    AppCodexDesktop AppID = "codex_desktop_app"
    AppClaudeCode   AppID = "claude_code"
)

type AppState struct {
    ID        AppID
    Enabled   bool
    Detected  bool       // app binary present on disk
    Routed    int        // conversation turns routed
    Bypassed  int        // sideband turns passthrough'd
    LastSeen  time.Time
}

type SavingsSnapshot struct {
    InputTokensSaved   int64
    OutputTokensSaved  int64
    CostUSD            float64
    StreamcutFired     int64
    RepdetRewrites     int64
    RepdetBytesSaved   int64
    StaleReadBlocks    int64
    ObsoletePruneBlocks int64
    QualityABFailures  struct {
        ControlPct   float64
        TreatmentPct float64
        DeltaPP      float64
        RolledBack   bool
    }
}
```

The state is assembled once per render via `BuildSetupState(ctx)`. Each
sub-state is built by a small probe function that wraps the underlying
system call:

- `probeKeychain()` → `security find-certificate -c "Slimference Root CA"`.
- `probeLaunchd()` → `launchctl print system/com.slimference.proxy`.
- `probeListener()` → `lsof -i :443` + `:8990`.
- `probeHosts()` → read `/etc/hosts`, look for our marker comments.
- `probePFCtl()` → `pfctl -s nat`.
- `probeIndist()` → run `slimference proxy verify --indist`.
- `probeApps()` → check `~/.codex/config.toml`, `/Applications/Codex.app`,
  `~/.claude/settings.json`.

## TUI actions

| Key | Action                                                            |
|-----|-------------------------------------------------------------------|
| I   | Install (or repair if partially installed)                        |
| U   | Uninstall (full reverse) - confirm dialog                         |
| A   | Per-app config screen: toggle each app independently              |
| S   | Stats detail screen: deep-dive into counters + per-session log    |
| P   | Probe / verify all components, indistinguishability check         |
| L   | Logs (tail proxy log + recent decisions)                          |
| R   | Reload config (HUP daemon)                                        |
| Q   | Quit (does NOT stop the daemon)                                   |

## Sub-Tasks

- [ ] State model + probes (`internal/control/state.go`)
- [ ] Install action: generate CA, prompt for sudo, add to keychain,
      install launchd, modify /etc/hosts, start daemon, verify each step,
      roll back on any failure.
- [ ] Uninstall action: stop daemon, remove launchd, restore /etc/hosts,
      remove CA from keychain, leave the CA files in `~/.slimference/ca/`
      for the user to manually delete (avoids accidental loss).
- [ ] Per-app toggle: writes to `~/.config/slimference/apps.toml`, hot-
      reloads via SIGHUP.
- [ ] Stats detail screen.
- [ ] Probe / verify dispatcher.
- [ ] Confirm dialogs for destructive actions.
- [ ] Help screen (`?` key) listing every action.
- [ ] Non-TTY mode: `slimference setup --auto` runs install non-interactively
      with reasonable defaults.

## Acceptance

- Fresh-Mac install via TUI takes ≤ 3 minutes wall-clock; one sudo
  prompt; no other interaction.
- Uninstall via TUI reverses every install step. Snapshot-diff of
  /etc/hosts, Keychain trusted-roots, and launchd plist directory shows
  no Slimference artefacts after uninstall (CA file in
  ~/.slimference/ca/ remains for user-controlled cleanup).
- Per-app toggle: turning Codex Desktop App off (while CLI on) is
  reflected in the next intercepted request: Desktop App requests
  passthrough, CLI requests MITM. Counter increments by app.
- All probe functions complete in ≤ 100 ms cumulatively (TUI render not
  blocked).
- TUI accessible via `slimference` with no args.

## Notes

- The CA files in `~/.slimference/ca/{root.key, root.crt}` are not
  automatically deleted on uninstall - they are private keys. The
  uninstall removes the keychain trust entry; the on-disk files require
  explicit `slimference ca purge` (which we add as a separate action).
- The TUI talks to the daemon over the existing `/admin/*` HTTP endpoints.
  Add `/admin/state` aggregate endpoint that returns `SetupState` in one
  JSON snapshot for cheap rendering.

## Deviations

(none yet)
