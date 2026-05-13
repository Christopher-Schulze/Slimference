# TASK 133: Transparent daemon/TUI control plane and certificate-magnet UX

Status: DONE (local implementation completed 2026-05-13; live Codex/App proof remains T140)
Priority: P0
Scope: `internal/tui/`, `cmd/slimference/proxy_cmd.go`, `cmd/slimference/service*`, `internal/daemon/`, `internal/transparent/`, `internal/proxy/`, `internal/config/`, `docs/transparent-mode.md`, `docs/documentation.md`.

## Why

The product path should be: install Slimference once, trust its local CA once, keep the daemon available through launchd, and use the TUI as the control surface. Codex CLI/App should not need to be config-patched for the default path. The "magnet" is the macOS HTTPS proxy + local CA: when armed, allowlisted LLM HTTPS traffic flows through Slimference; when disarmed, Codex and other apps talk directly to OpenAI/Anthropic again.

The existing code has pieces: proxy CLI, CA/keychain/networksetup/launchd helpers, TUI provider/layer toggles, daemon status, and transparent runtime wiring. The missing product is one coherent operator flow with exact state, reversible actions, repair hints, and no accidental Codex mutation.

## Target State

Slimference TUI acts as an operator console:

1. Install/remove/reinstall daemon and local CA.
2. Start/stop/restart daemon.
3. Enable/disable autostart.
4. Arm/disarm system HTTPS proxy.
5. Toggle full pipeline and individual layers while armed.
6. Show exact daemon, launchd, CA, keychain, networksetup, proxy, layer, bypass, and health state.
7. Show current traffic mode: direct, transparent armed, transparent disabled, broken armed-with-daemon-down, or config-patch legacy.
8. Never patch `~/.codex/config.toml` or `~/.codex/hooks.json` in the default transparent path.
9. Provide explicit emergency-off and repair commands.

## Work Packages

### WP1 - State model

- Add a single `TransparentControlState` snapshot assembled from launchd, daemon health, CA files, keychain trust, macOS network services, proxy config, and runtime admin status.
- State fields:
  - `daemon_installed`
  - `daemon_running`
  - `daemon_pid`
  - `daemon_health`
  - `autostart_enabled`
  - `ca_exists`
  - `ca_trusted`
  - `ca_fingerprint`
  - `proxy_armed`
  - `armed_services`
  - `broken_proxy_services`
  - `transparent_enabled_in_config`
  - `pipeline_enabled`
  - `layers_enabled`
  - `providers_enabled`
  - `bypass_mode`
  - `last_error`
- Snapshot must not shell out repeatedly inside render loops. Cache with short TTL or refresh on action.

### WP2 - TUI actions

- Add actions:
  - Install daemon + CA.
  - Remove daemon + CA.
  - Reinstall daemon + CA.
  - Start daemon.
  - Stop daemon.
  - Restart daemon.
  - Enable autostart.
  - Disable autostart.
  - Arm transparent proxy.
  - Disarm transparent proxy.
  - Emergency off: disarm proxy, set bypass, keep logs.
  - Toggle Layer 1, Layer 2, Layer 3, output reduce, server state, provider switches.
- Destructive actions require a confirm state in the TUI, but routine arm/disarm/start/stop do not.
- All action results must surface exact command-equivalent text for CLI parity.

### WP3 - UX layout

- Add a "CONTROL" view or extend Setup/Dashboard with four stable panels:
  - Installation: daemon, CA, trust, autostart.
  - Traffic: direct vs armed, services, daemon reachability.
  - Pipeline: master, layers, providers, output-reduce, server-state.
  - Diagnostics: last request, last error, log path, decision log path, repair command.
- Status language must be operational, not marketing:
  - "direct"
  - "armed"
  - "armed but daemon down"
  - "trusted CA missing"
  - "daemon running but transparent disabled"
- Avoid nested cards; keep dense operator-console layout.

### WP4 - CLI parity

- Every TUI action must map to an existing or new CLI subcommand:
  - `slimference proxy install|enable|disable|status|uninstall`
  - `slimference daemon start|stop|restart|status`
  - `slimference service install|remove|status`
  - layer/provider/bypass commands.
- If a TUI action needs new CLI plumbing, add the CLI first and make TUI call the same service adapter.

### WP5 - Safety and reversibility

- `proxy disable` must never require the daemon to be alive.
- `uninstall` must first disarm network proxy settings, then remove CA trust, then remove daemon/autostart artifacts.
- If uninstall partially fails, status must show remaining artifacts and exact repair commands.
- A crash while armed must be recoverable by `slimference proxy disable`.
- System proxy state must be read from `networksetup`, not guessed from config.

### WP6 - Layer control semantics

- Distinguish three switches:
  - Daemon running: process availability.
  - Proxy armed: system traffic routes through daemon.
  - Pipeline enabled: traffic is compressed vs passthrough/log-only.
- Add a "log-only transparent" mode: traffic flows through Slimference but all compression layers are disabled; decision logging and provider/cache measurement still run where safe.
- Layer toggles must apply live through admin API and persist to config/TUI state only after confirmation of success.

### WP7 - Tests

- Unit-test state classification from fixture snapshots.
- TUI tests for every action label and state transition.
- CLI service adapter tests for success/partial-failure/repair output.
- macOS networksetup helpers stay mocked in CI.
- No live keychain/networksetup mutation in default tests.

## Acceptance

- [x] TUI shows installed/running/autostart/CA/trust/proxy/layers/providers accurately from a cached transparent-status snapshot instead of shelling out from render loops.
- [x] Default transparent path does not touch `~/.codex/config.toml` or `~/.codex/hooks.json`.
- [x] Operator can install, arm, disarm, restart, disable autostart, and uninstall from TUI.
- [x] Broken armed-with-daemon-down state is detected and repairable.
- [x] Layer/provider toggles work independently of daemon install state.
- [x] `go run ./scripts/ci` passes.
- [x] Manual macOS E2E is explicitly deferred to T140; T133 is local-control-plane complete.

## Notes

- This task is the product shell around T131/T122. It does not claim compression quality; it makes the system controllable.
- Voice/WebRTC bypass is certified in T140, not here.
- Implemented TUI control plane:
  - Setup wizard now starts with transparent install and arm steps; Codex/Claude hooks remain legacy fallback steps.
  - Dashboard has an `Arm transparent proxy` / `Disarm transparent proxy` operation.
  - Setup shortcuts: `[a]` arm/disarm transparent, `[u]` uninstall transparent, `[p]` daemon start/stop, `[o]` restart, `[e]` enable autostart, `[w]` disable autostart.
  - Status classifies not installed, partial install, installed/disarmed, armed/reachable, armed/daemon-unreachable, and networksetup-unavailable states.
  - TUI status is cached with a short TTL and force-refreshed after actions; render paths do not call `networksetup`, `security`, or `launchctl`.
- `cmd/slimference` service control now reuses the existing `proxy install|enable|disable|uninstall` command path through the same `proxyEnv` dependencies, so CLI and TUI stay behaviour-identical.
- Verification: `go run ./scripts/ci` passed all 8 steps with total coverage 100.0%.
