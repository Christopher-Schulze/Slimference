# TASK 201: Install / Uninstall / Enable / Disable CLI subcommands

Status: PLANNED 2026-05-16
Parent: T200 (Phase H epic)
Scope: `cmd/slimference/install_cmd.go` (new), `cmd/slimference/main.go`
       (subcommand dispatch), `internal/install/` (new shared package)
Dependencies: `internal/control/reversibility/` (T196, complete),
              `internal/control/reversibility/steps/` (T196, complete),
              `internal/hooks/codex.go` (existing, becomes a Step
              wrapper)

## Why

The user wants ONE entry point. Today there are six:
- TUI Setup-Wizard buttons (InstallTransparent / EnableTransparent /
  UninstallTransparent)
- `slimference hook install/remove` (Codex hooks subcommand)
- `slimference integrate` (cmd/slimference/integrate_cmd.go)
- `slimference service install/uninstall`
- `slimference trust` (CA install in keychain)
- Manual env tweaks (`OPENAI_API_BASE`, `HTTPS_PROXY`)

Phase H consolidates into one CLI surface, atomic via reversibility.
Plan.

## Target state

### Subcommands

```bash
slimference install        # CA + launchd + hooks (NO hosts patch)
slimference install --no-hooks      # only daemon, no hook signal
slimference install --no-autostart  # skip launchd plist install
slimference install --dry-run       # show Plan.Inspect output

slimference uninstall      # Reverse the install Plan
slimference uninstall --keep-ca     # keep CA in keychain (rotated aside)
slimference uninstall --dry-run

slimference enable         # arm transparent MITM (hosts + SNIPeekMode)
slimference disable        # disarm (revert hosts, SNIPeekMode off)

slimference status         # render SetupState as a colored table
slimference status --json  # machine-readable
```

### Shared install package: `internal/install/`

```go
package install

// Plan returns the reversibility.Plan that describes a Slimference
// installation. Callers may inspect / Apply / Reverse it.
//
// The Plan is composed of Steps:
//   1. ca.generate     — local CA under ~/.slimference/ca
//   2. ca.keychain     — install root cert in macOS Keychain
//   3. launchd.install — ~/Library/LaunchAgents/com.slimference.proxy.plist
//   4. hooks.codex     — patch ~/.codex/config.toml [hooks]
//   5. hooks.claude    — patch ~/.claude.json (Claude Code agent hooks)
//
// /etc/hosts is NOT part of the install Plan - it is daemon-lifecycle
// state managed by T202.
func Plan(opts Options) (*reversibility.Plan, error)

type Options struct {
    Home          string  // override HOME (test injection)
    SkipHooks     bool    // --no-hooks
    SkipAutoStart bool    // --no-autostart
    BinaryPath    string  // self-resolution by default
}

// HostsPlan returns the reversibility.Plan for the runtime hosts-patch
// step. Separate from Plan() because hosts is daemon-lifecycle (T202),
// not install-time.
func HostsPlan(opts HostsOptions) (*reversibility.Plan, error)

type HostsOptions struct {
    Targets []string  // default: chatgpt.com, api.openai.com, api.anthropic.com
    Address string    // default: 127.0.0.1
    Home    string    // for backup path
}
```

### Single source of truth

Both the CLI (`cmd/slimference/install_cmd.go`) and the TUI
(`internal/tui/model.go` setup-wizard / Apps view) consume the same
`install.Plan()` function. No duplication. No drift.

### Status command output

```
$ slimference status
Slimference status (data: ~/.slimference)

  CA              ✓ installed, in Keychain (fingerprint a3:f2:…:9c)
                    not after 2027-05-16
  Daemon          ✓ running (pid 12483, RSS 18 MB, uptime 2h14m)
                    HTTPS listener: 127.0.0.1:8990
                    SNI-peek listener: 127.0.0.1:8443
  Network         ✗ hosts file: CLEAN (transparent mode disarmed)
                    → run `slimference enable` to arm
  Apps            codex_cli         ENABLED   routed   12,453   bypassed     0
                  codex_desktop_app ENABLED   routed       43   bypassed     0
                  claude_code       DISABLED  routed        0   bypassed   847
  Savings         output tokens saved 421,932 (~ $2.53)
                    streamcut fires 1,243   repdet rewrites 387
                    quality A/B  control 1.2%  treatment 1.1%  (healthy)

Overall: TRANSPARENT MODE DISARMED — daemon up, but Codex 0.130 WSS
not intercepted. Run `slimference enable` to start intercepting.
```

## Implementation plan

1. **Create `internal/install/install.go`**
   - `Plan(opts Options) (*reversibility.Plan, error)` — composes the
     5 Steps listed above
   - `HostsPlan(opts HostsOptions) (*reversibility.Plan, error)`
   - Helper: `binaryPath() string` returns absolute self-path
   - Helper: `homeOrErr(opts) (string, error)`

2. **Migrate existing Step constructors**
   - `ca.generate` → already exists at `steps.CAGenerate`
   - `ca.keychain` → new Step wrapping `internal/tlsca` Keychain helpers
     OR `internal/trust` package (check what exists; reuse not rebuild)
   - `launchd.install` → already exists at `steps.LaunchdInstall`
   - `hooks.codex` → wrap `internal/hooks/codex.go`'s Install function
     in a Step interface so it's part of the Plan
   - `hooks.claude` → ditto for Claude

3. **Add `cmd/slimference/install_cmd.go`**
   - Parses subcommand args
   - Calls into `internal/install`
   - Prints progress: each Step prefixed with `[apply N/5]`
   - On error: report which Step failed; offer `slimference uninstall`
     to roll back
   - `--dry-run` calls `Plan.Inspect()` and prints the table

4. **Wire into `cmd/slimference/main.go` dispatch**
   ```go
   case "install":   handleInstallCmd(args[1:])
   case "uninstall": handleUninstallCmd(args[1:])
   case "enable":    handleEnableCmd(args[1:])
   case "disable":   handleDisableCmd(args[1:])
   case "status":    handleStatusCmd(args[1:])
   ```
   - The existing `integrate`, `hook install`, `service install`,
     `trust` subcommands stay as-is (backwards compatibility for older
     scripts), each gaining a deprecation hint pointing to `install`.

5. **TUI consolidation (separate sub-task in T197)**
   - Replace `InstallTransparent` / `EnableTransparent` /
     `UninstallTransparent` adapter methods with one
     `RunInstallPlan(action string) error` that calls into
     `internal/install`.

6. **Tests**
   - `internal/install/install_test.go`: assert Plan composition has
     the expected 5 Steps in order; HostsPlan has 1 Step.
   - `cmd/slimference/install_cmd_test.go`: with a fake home + stubbed
     Step implementations, `slimference install --dry-run` prints the
     5-row table; `install` then `uninstall` is byte-equal in the
     filesystem.
   - `slimference enable` → `disable` round-trip is byte-equal in
     `/etc/hosts` (tested against a temp file via `HostsOptions.Home`).

## Failure semantics

| Failure mode | Behavior |
|---|---|
| One Step in Plan fails on Apply | Plan.Apply returns error; previously-applied Steps stay (LIFO rollback is the user's call via `uninstall`). CLI prints offending Step + suggests `uninstall`. |
| Apply succeeds, Reverse fails on one Step | Plan.Reverse continues for other Steps; CLI reports partial-success with `--show-skipped`. |
| `enable` called before `install` | CLI errors: "run `slimference install` first" — does NOT auto-install (explicit user intent). |
| `enable` while daemon down | hosts patch goes in but no listener answers. CLI warns: "hosts armed but daemon not running - `slimference service start`." |
| Daemon already running on `disable` | sends SIGHUP after revert; daemon's hosts-lifecycle (T202) catches the config change. |

## Acceptance

- `slimference install` + `slimference uninstall` round-trip leaves
  `~/.codex/config.toml`, launchd plist directory, and CA directory
  byte-equal to pre-install state (modulo CA rotation files).
- `slimference enable` + `slimference disable` round-trip leaves
  `/etc/hosts` byte-equal.
- `slimference status --json` produces a parseable JSON document
  matching `control.SetupState` shape exactly.
- 100% test coverage on `internal/install/`.
- `slimference install --dry-run` prints the 5-Step plan with current
  Inspect state and exits 0 without touching disk.

## Sub-Tasks

- [ ] `internal/install/install.go` with `Plan` + `HostsPlan`
- [ ] Step wrapper for Codex hooks install (wrap existing function)
- [ ] Step wrapper for Claude hooks install (wrap existing function)
- [ ] Step for CA → Keychain install (wrap existing trust helper)
- [ ] `cmd/slimference/install_cmd.go`
- [ ] Subcommand dispatch in `main.go`
- [ ] `status` command with text + JSON output formats
- [ ] Deprecation notices on `integrate`, `hook install`, `service`,
  `trust` subcommands
- [ ] Tests (install_test, install_cmd_test, enable_disable_test)
- [ ] Update `docs/install.md` (T203 alongside)

## Deviations

(none yet)
