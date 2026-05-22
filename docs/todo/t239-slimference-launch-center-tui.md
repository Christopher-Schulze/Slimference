# TASK 239: Slimference launch center TUI

Status: PARTIAL - launch-center entrypoint implemented in existing TUI
Priority: P0 after T238/T242 Desktop capability branch is known enough to gate
the Launch Codex App menu item honestly
Scope: User-facing Slimference launch and management UX for Codex CLI and
Codex Desktop on macOS arm64

## Why

The user does not want a matrix of modes, lab commands, route patches, proxy
switches, and app-specific toggles. The normal product should feel like one
place to start and manage Slimference:

- Launch Codex CLI
- Launch Codex App
- Savings
- Status
- Manage Slimference

Direct mode does not need its own menu item. Direct mode is launching Codex
normally outside Slimference: `codex` in a normal shell, or Codex.app from
Finder/Spotlight. The Slimference TUI is for the Slimference-launched path and
for health, savings, install, repair, and uninstall.

This reduces complexity: instead of maintaining persistent per-app on/off
switches as the primary UX, the user chooses the launch path. Normal launch is
direct; Slimference launch is optimized. Persistent enable/disable commands
remain available under Manage for supported scoped config routes and recovery,
but they are not the main mental model.

Install is unified: the default product action is "Install Slimference for
Codex" and prepares both CLI and Desktop support. There are no default
checkboxes for "CLI only" versus "Desktop only". Status can say Desktop support
is prepared but blocked/unproven, but it must not create a half-installed
mental model.

## Target State

Running `slimference` opens a compact launch center. The user can start the
optimized CLI or optimized Desktop path, inspect real savings, check health,
and repair or remove Slimference from one place. Nothing in the launch center
arms global lab mode by accident.

The launch center is not a settings maze. It is a cockpit:

1. **Launch Codex CLI** starts the proven CLI path.
2. **Launch Codex App** is a capability-gated TUI menu item: on current
   Codex.app builds it blocks with the proof reason, because the item means
   "start Desktop in Slimference mode" and the prompt-driven Desktop
   Slimference proof is not green. Direct Desktop mode is still available by
   launching Codex.app normally from Finder/Spotlight. A future proven Desktop
   path can replace this branch only after T242 records real bytes, WSS frames,
   and Phase-F mutation through Slimference.
3. **Savings** shows actual measured savings and separates estimates.
4. **Status** shows whether the machine is safe, healthy, and scoped.
5. **Manage Slimference** handles install, repair, uninstall, enable/disable,
   logs, and advanced lab controls behind explicit wording.

CLI WSS must not be blocked by CA trust. The launch center should explain this
plainly: scoped Codex CLI WSS does not need a macOS trusted CA or custom CA env.
Desktop process-local proxy diagnostics first use `CODEX_CA_CERTIFICATE` /
`SSL_CERT_FILE` only for the spawned Codex.app process. Keychain trust belongs
to fallback Desktop/Lab branches only, and is tracked by T245.

## Acceptance

- Top-level TUI has exactly these primary entries:
  - Launch Codex CLI
  - Launch Codex App
  - Savings
  - Status
  - Manage Slimference
- There is no top-level "direct open" action.
- Manage Slimference has one default Install/Repair/Uninstall product flow for
  Codex as a whole. It must not ask the user to choose CLI-only or Desktop-only
  during the default install.
- Launch Codex CLI starts the existing safe Codex CLI product path with
  `transport=auto` and shows WSS certification/fallback state. The Terminal
  launch must scrub inherited `CODEX_*` session variables first so a TUI opened
  from an existing Codex session cannot accidentally resume that old thread.
- Launch Codex App uses the T242 branch decision:
  - if a future `slimference codex desktop prove --finish --json` result is
    `desktop_proxy_phasef_proven`, launch the proven Desktop path;
  - on current Codex.app builds, block the launch and show the Desktop proof
    reason in Status;
  - never start a broken proxy/proof session from the daily TUI launch action;
  - never open direct Codex.app from this menu item, because direct launch is
    outside Slimference;
  - never pretend Desktop savings are active without a green finish proof.
- Savings shows total, today, session, route, and mechanism attribution where
  the data exists; no fake Desktop savings.
- Status shows daemon, CA trust, WSS cert, Codex CLI version, Codex Desktop
  version, route mode, config drift, listener state, and last Desktop/CLI
  observation.
- Status separates CA state from CLI WSS health: missing CA may be yellow for
  Desktop/Lab readiness, but must not make Launch Codex CLI red.
- Manage Slimference contains Install, Repair, Uninstall, enable/disable
  recovery actions, Repair CLI WSS, bounded logs, and lab/advanced controls
  fenced away from normal use.
- Browser ChatGPT, ChatGPT.app, and Claude Code are explicitly shown as
  untouched/direct unless the user enters a lab path.
- All actions are reversible and fail open.
- The TUI never offers a top-level persistent Desktop toggle before T238 proves
  Desktop routing.
- The TUI can be used as the normal entry point without remembering CLI flags.

## Information Architecture

### Launch Codex CLI

- Shows detected Codex CLI version.
- Shows Slimference WSS certification tuple and fallback reason.
- Defaults to `transport=auto`.
- Lets the user enter or paste a prompt.
- Opens an interactive Terminal session through
  `slimference codex run --transport=auto --`; one-shot `exec ...` remains
  available when the user runs it directly.
- If Slimference daemon is unhealthy, explains fail-open and offers direct run.

### Launch Codex App

- Current behavior: blocks with the explicit Desktop proof reason because
  current Codex.app builds do not produce real Slimference Desktop savings.
  Direct Codex.app launch remains Finder/Spotlight outside Slimference.
- The Desktop launch environment must drop inherited `CODEX_*` session state
  such as `CODEX_THREAD_ID` and must pin `PWD` to the selected current folder.
  This prevents a Slimference/Codex session from leaking an old thread into the
  newly opened Desktop app.
- If T242 later passes: launches Codex.app with the proven process-local proxy
  mode.
- If previous live counters show zero-byte CONNECT sessions: displays
  `tls_trust_rejected` / `desktop_ca_env_rejected` as proof failure and blocks
  rather than opening direct or a known-bad proxy session.
- If T242 failed or is unproven: displays blocked/proof-needed state and why.
- Shows whether process-local custom CA env is available, whether Keychain trust
  is irrelevant/needed/trusted, and whether either state actually proved bytes.
- Shows whether the currently running Codex.app was Slimference-launched or
  direct-launched.
- Does not restart or kill Codex.app without explicit confirmation.

### Savings

- Separates proxy input savings, WSS mutation savings, prompt-cache savings,
  output-reduce savings, hook/readhook savings, and estimates.
- Desktop savings are hidden or marked unavailable until T238/T240 prove them.
- Shows today/week/month/all plus last session when session attribution exists.
- Never mixes local hook savings into proxied Codex traffic totals unless the
  source is clearly labelled.

### Status

- Shows daemon health, binary path/SHA, config path, CA disk/trust state,
  listener state, Codex CLI route, Desktop route, WSS cert, drift fallback,
  and global lab disarmed/armed state.
- Shows Browser ChatGPT, ChatGPT.app, and Claude Code as untouched/direct in
  the normal product path.
- Surfaces exact repair actions instead of vague warnings.

### Manage Slimference

- Product actions: Install Slimference for Codex, Repair Slimference for Codex,
  Uninstall Slimference, Enable scoped route, Disable scoped route, Repair CLI
  WSS, Restart daemon, View logs.
- Advanced/conditional actions: CA trust for Desktop/Lab only, global lab
  enable/disable/root-arm/root-disarm, clearly labelled as lab/global.
- Install/Repair prepares both CLI and Desktop support together. It reports
  Desktop capability as prepared/proven/blocked rather than installed/uninstalled
  separately.
- CA actions must say why they are not required for CLI WSS, show whether the
  process-local `CODEX_CA_CERTIFICATE` path is available, show the certificate
  subject/fingerprint, use SSL-only trust for Keychain fallback, and provide a
  matching remove/repair action through T245.
- Every destructive or global action has a confirmation and shows the blast
  radius before execution.
- Repair CLI WSS is a manual override for T241 auto-recert. It must call the
  same recert core as background auto-repair and `slimference codex recertify
  wss`; no separate TUI-only repair logic is allowed.

## Sub-Tasks

- [x] Design the final launch-center state model using existing
  `/admin/state`, `/admin/status`, `codex status`, and savings surfaces.
- [~] Add a route-mode vocabulary shared by CLI, TUI, and docs:
  direct, slimference-cli-wss, slimference-cli-http, desktop-direct,
  desktop-proxy-proven, desktop-proxy-unproven, lab-global.
- [x] Implement top-level menu entries exactly as accepted; do not add a
  direct-open item.
- [x] Implement Launch Codex CLI as a guided wrapper around
  `slimference codex run --transport=auto --`.
- [x] Implement Launch Codex App as a capability-gated menu item: proven launch
  when green, otherwise blocked with a proof reason. Do not hide it just because
  the current Desktop route is blocked.
- [x] Gate Launch Codex App on the recorded T242 proof result: current live
  result blocks the TUI launch, while future Desktop Slimference requires
  `desktop_proxy_phasef_proven`.
- [~] Fold current install/enable/disable/repair/uninstall controls into Manage
  Slimference with clear product vs lab separation.
- [ ] Make the default Install/Repair flow unified for Codex CLI and Desktop:
  no default per-app checkboxes, no half-installed product state, Desktop shown
  as capability-gated after install.
- [ ] Add Manage Slimference "Repair CLI WSS" as a manual override wired to the
  T241 shared recert core, not a separate implementation.
- [ ] Add Manage Slimference "Desktop custom CA probe" and
  "Repair/Remove Desktop-Lab Keychain Trust" as capability-gated T245 actions;
  do not show either as required for CLI WSS.
- [~] Show savings truth without mixing hook estimates, proxy savings, cache
  savings, and Desktop-unproven traffic.
- [x] Add tests for menu structure, action routing, status wording, and no
  accidental lab/global activation from product actions.
- [~] Add focused UX tests for the T238 branches: Desktop proven, Desktop
  unproven, Desktop failed by cert trust, Desktop failed by WSS bypass.
- [ ] Add golden text tests for user-facing wording so the app never claims
  Desktop savings before proof.
- [x] Update `docs/install.md` with the human flow: normal launch is direct,
  Slimference launch goes through the launch center.

## Implementation Order

1. Define state structs and route-mode vocabulary.
2. Wire read-only Status and Savings first.
3. Wire Launch Codex CLI using the already-proven command path.
4. Wire Manage Slimference product actions.
5. Wire advanced/lab actions behind explicit lab labels.
6. Wire Launch Codex App only after T238 has a final branch decision.
7. Update install docs and operation log.
8. Hand to T240 for the final zero-drawdown release certification.

## Notes

The user's preferred UX is launch-based, not switch-based:

- Normal app/CLI launch means direct mode.
- Slimference TUI launch means optimized mode.
- Enable/disable remains useful for persistent scoped route and repair, but it
  should not be the primary story if the launch center can own the clean path.

This is lower cognitive complexity than independent always-on toggles for CLI
and Desktop. It also gives a safe fallback when Codex Desktop changes: normal
launch still works while Slimference launch can refuse or downgrade.

The existing `enable` / `disable` commands remain useful, but they move under
Manage. The normal daily decision is simply: launch through Slimference or
launch normally.

T238/T242 implementation provides the Desktop status and diagnostic command
surface that this TUI should report without making a false savings claim:

- `slimference codex desktop status --json` for CA, daemon, WSS counters, and
  Desktop live-proof state.
- `slimference codex desktop prove --manual --json` for the prompt-driven
  Desktop proof start: it launches, observes startup WSS delta, classifies the
  result, and keeps the app open when ready for a user prompt.
- `slimference codex desktop prove --finish --json` for the actual Desktop
  savings gate after the user sends a prompt in the launched app.
- `slimference codex launch-desktop --transport=proxy --with-ca-env` for the
  preferred Desktop proof branch that injects `CODEX_CA_CERTIFICATE` and generic
  CA hints only into the spawned Codex.app process.
- `slimference codex launch-desktop --transport=base-url --probe` for
  diagnostic/future upstream env-hook checks only.

Do not wire the Launch Codex App menu item as a savings success path unless
`codex desktop prove --finish` returns a green Desktop Phase-F savings proof.
The item stays in the TUI because it is the user's steering wheel; current
behavior blocks with the proof reason, while manual proof mode remains an
explicit diagnostic command.

2026-05-19 implementation landing:

- The existing BubbleTea TUI was consolidated, not duplicated. `ViewMain` is now
  the Launch Center and renders exactly the five accepted entries.
- `Launch Codex CLI` opens a new Terminal session running
  `slimference codex run --transport=auto --`, which starts the interactive
  Codex CLI through the scoped wrapper. Normal daily CLI launch can come from
  the TUI without a persistent shell alias. The generated shell command first
  unsets inherited `CODEX_*` variables so Launch Center starts a fresh CLI
  context even when Slimference itself was opened from inside Codex.
- `Launch Codex App` consumes `codex desktop status` and blocks while Desktop
  Slimference is not green. This prevents the daily TUI path from starting a
  known-bad proof/proxy session or silently opening direct mode under a
  Slimference label. Historical `tls_trust_rejected` counters are shown as a
  proof failure, not as green Desktop savings. Normal Finder launch remains
  direct.
- `Savings` opens the existing Stats view. `Status` refreshes daemon, route,
  Desktop, and lab state. `Manage Slimference` opens the existing Setup view
  rather than creating a parallel management UI.
- T241/T243 update: Manage Slimference must gain "Repair CLI WSS" and the
  Launch Center status should show WSS Phase-F active, WSS bridge repairing,
  HTTP fallback, or direct fallback using the shared transport ladder.
- T245 update: Manage Slimference must show custom CA and Keychain trust as
  Desktop/Lab-only. The user should never think installing or trusting a CA is
  required for CLI WSS savings.
- Remaining polish is depth, not architecture: embedded prompt entry for CLI,
  richer Status/Manage rows, full Desktop branch matrix tests, and final T240
  live release certification.

2026-05-22 follow-up:

- The Desktop proxy launcher now passes Electron
  `--proxy-server=http://127.0.0.1:8990` and
  `--proxy-bypass-list=localhost;127.0.0.1;::1` arguments in addition to proxy
  and CA env. This fixed the Chromium NetworkService direct-socket bypass for
  the launched process tree.
- A visible prompt in that launched app still produced only one CONNECT/MITM
  session with zero application bytes, zero WSS frames, and zero Phase-F
  mutation. Therefore the Launch Codex App menu item remains blocked until a
  future proof reaches `desktop_proxy_phasef_proven`.

## Deviations

None.
