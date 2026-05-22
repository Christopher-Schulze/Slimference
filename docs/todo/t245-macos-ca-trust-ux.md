# TASK 245: Desktop custom CA and macOS trust UX

Status: PARTIAL - default install is Keychain-free; Desktop app-server shim does
not need CA but is live-blocked for current Codex.app; TUI CA management remains
planned for legacy proxy/lab diagnostics
Priority: P1 lab-polish unless T240 exercises legacy Desktop proxy diagnostics
Scope: macOS arm64 Keychain fallback/lab UX, install/repair/remove semantics,
and status truth for Desktop process-local proxy diagnostics and global lab
paths only

## Why

The user wants one simple Slimference entry point that can launch Codex CLI and
Codex Desktop without degrading native behavior. Product install should prepare
both Codex surfaces together. It should not ask the user to choose "CLI only"
versus "Desktop only" during the normal install flow, because that creates
half-installed state, confusing status, and avoidable repair branches.

Unified install means one product install/repair/uninstall surface. It does not
mean every powerful trust primitive must be installed for every user up front.

Scoped Codex CLI WSS does not require macOS Keychain CA trust or a local CA env.
The CLI product path is:

1. Codex CLI is launched through Slimference.
2. Codex talks to the local Slimference route.
3. Slimference talks upstream to OpenAI/ChatGPT using its own upstream TLS
   client.

There is no local TLS interception of the Codex CLI process in that scoped path,
so the macOS CA is not a prerequisite for WSS Phase-F savings, WSS byte-equal
bridge, auto-recert, or the T243 fallback ladder.

CA material is only relevant when Slimference terminates TLS for a client that
expects a normal `chatgpt.com` certificate. In the current product plan that is
limited to:

- Codex Desktop process-local proxy diagnostics from T238/T242;
- global transparent lab mode;
- future diagnostic branches if Codex.app can be made to trust the local CA or
  another safe root-store hook.

The T246 Desktop app-server shim does not terminate TLS for Codex.app and
therefore does not need a local CA or Keychain trust. Current live proof still
ends as `desktop_connect_only_no_app_server_bytes`, so CA trust would not repair
that branch.

Therefore the correct UX is unified install with conditional trust:

1. Product install prepares daemon, CLI path, Desktop launcher/probe path,
   logs/status/repair, and Slimference-owned CA material.
2. Product install does not force Keychain trust by default.
3. Desktop proof first uses the T246 app-server shim, which needs no CA but is
   currently blocked for application bytes.
4. Legacy Desktop proxy diagnostics may try process-local
   `CODEX_CA_CERTIFICATE` / `SSL_CERT_FILE` env from the launcher. This is
   scoped to the launched Codex.app process and avoids a Keychain prompt if
   Codex honors it.
5. Keychain trust remains an explicit fallback/lab action only when a chosen
   Desktop/Lab branch actually requires OS trust.

## Acceptance

- Normal `slimference` install/launch center works for Codex CLI WSS without
  requiring Keychain CA trust.
- Normal install prepares both Codex CLI and Codex Desktop support together.
  There are no default CLI/App install checkboxes and no "Desktop not
  installed" state. Desktop can be `prepared but blocked`, `diagnostic`, or
  `proven`, but not half-installed.
- `Launch Codex CLI` and `slimference codex run --transport=auto -- ...` never
  fail solely because CA trust is missing.
- `Manage Slimference` can show CA state as:
  - not needed for CLI WSS;
  - not needed for the current Desktop app-server shim diagnostic;
  - process-local custom CA env available for legacy Desktop proxy diagnostics;
  - Keychain not needed for current product path;
  - Keychain needed only for Desktop/Lab fallback;
  - trusted in Keychain;
  - installed but not trusted;
  - stale/mismatched;
  - remove available.
- Process-local custom CA env must be explicit in status/probe output but does
  not require OS authorization because it affects only the spawned Codex.app
  process tree.
- Keychain trust install is never silent. It must require an explicit user
  action and an OS authorization prompt when writing to the System Keychain.
- Trust scope is as narrow as macOS allows for this use case:
  `security add-trusted-cert -d -r trustRoot -p ssl ...` or an equivalent
  Keychain flow. Do not request code-signing or all-purpose trust.
- The TUI uses the existing command surfaces instead of a parallel flow:
  Desktop `codex desktop prove --manual --json` as the launch-readiness gate,
  then `codex desktop prove --finish --json` as the savings gate after a user
  prompt;
  `cert-trust` / `lab cert-trust` if they remain the Keychain owner, or a
  consolidated `manage trust-ca` command if code is later unified.
- The TUI prints the exact blast radius before trust:
  "This only helps Desktop/Lab TLS interception diagnostics. CLI WSS and the
  current Desktop app-server shim diagnostic do not need it. Browser ChatGPT and
  ChatGPT.app remain direct unless you explicitly enter global lab mode."
- The TUI offers removal/repair:
  - repair regenerates or re-adds the current Slimference CA;
  - remove deletes only Slimference-owned CA entries by exact subject/fingerprint;
  - dry-run shows the exact certificate subject, fingerprint, and Keychain.
- T246/T242 must not classify Desktop as green merely because custom CA env is set
  or Keychain trust is present. Desktop success still requires real bytes/WSS
  counters through Slimference.
- T240 release certification records whether CA trust was absent, present, or
  removed, and proves CLI WSS behavior is independent of that state.
- Browser ChatGPT, ChatGPT.app, Claude Code, `/etc/hosts`, pfctl, and macOS
  system proxy are not touched by CA install alone.

## Sub-Tasks

- [x] Audit the current CA commands and status probes:
  `slimference cert-trust`, any `lab cert-trust` alias, `status --preflight`,
  and `codex desktop status --json`.
- [x] Audit `codex launch-desktop --with-ca-env` and ensure
  `CODEX_CA_CERTIFICATE` is the first-class Codex-specific CA variable.
- [x] Resolve CA probe inconsistency: current preflight may report
  `in_keychain=false` while Desktop status reports `trusted=true`. Establish
  one authoritative trust probe or explicitly label the difference.
- [x] Rework install/status vocabulary:
  "Slimference for Codex: installed/prepared" covers both CLI and Desktop
  support; Desktop capability state is separate from install state.
- [ ] Add TUI Manage rows:
  "CA: not needed for CLI", "Desktop custom CA env: available/missing",
  "Keychain trust: not needed/needed/trusted/stale", "Repair CA material",
  "Trust CA in Keychain", and "Remove Slimference CA".
- [ ] Ensure Keychain actions are hidden or labelled advanced unless a legacy
  proxy/lab diagnostic is running a Desktop/Lab probe that actually needs OS
  trust.
- [ ] Implement dry-run output for CA add/remove with subject, fingerprint,
  Keychain path, and SSL-only trust policy.
- [ ] Implement or verify exact removal by Slimference CA subject and
  fingerprint; never delete arbitrary local root certificates.
- [~] Add tests for CLI launch/status not requiring CA trust. Current patch
  covers default install and Desktop launch gating; explicit CLI/TUI wording
  tests remain.
- [x] Add tests for TUI state wording: missing CA must not make CLI WSS red.
- [~] Add tests for Desktop/Lab custom-CA-env wording, Keychain fallback
  wording, and reversible removal.
- [x] Update `docs/install.md` so users understand:
  one install prepares CLI and Desktop support; CLI WSS and the current Desktop
  app-server shim diagnostic need no CA; legacy Desktop/Lab Keychain trust is
  explicit fallback/lab; normal app/browser launches remain native/direct.
- [ ] Feed final CA state and commands into T240 evidence table.

## Notes

This task deliberately avoids "security theatre". A trusted local CA is a real
powerful primitive. It is justified only when Slimference is actually doing
local TLS termination for a scoped Desktop/Lab experiment. It is not needed for
the scoped CLI WSS route that currently delivers the real savings.

The installer should be unified. Unified means one product console and one
repair surface, not per-app install checkboxes and not one unconditional trust
mutation. The right model is:

- product install prepares Slimference for Codex CLI and Desktop together;
- CLI WSS works immediately without CA;
- Desktop Slimference launch is capability-gated by T246 and uses the
  app-server shim first, without CA;
- legacy Desktop proxy diagnostics may try process-local custom CA env;
- Keychain trust is prompted only when the chosen Desktop/Lab path requires it;
- removal is one explicit Manage action.

T238/T242 proved current Codex.app cannot use the local proxy/CA branch for
savings even with `CODEX_CA_CERTIFICATE`. Keychain trust is therefore lab-only
or diagnostic unless a future Codex.app build changes root-store behavior.

2026-05-22 live update:

- The process-local Desktop proof with `CODEX_CA_CERTIFICATE` and generic CA
  env reached Slimference CONNECT but finished as `desktop_ca_env_rejected` /
  `tls_trust_rejected` after the user sent a prompt.
- Keychain trust did not turn this into Desktop savings on the current
  Codex.app build. CA trust remains useful for Desktop/Lab diagnostics, but it
  is not part of the scoped CLI WSS savings path and must not block CLI launch,
  WSS auto-recert, or TUI Launch Codex CLI.
- TUI Launch Codex App should block until a future Desktop proof is green,
  because that menu item means "Slimference mode". Normal direct Codex.app
  launch remains Finder/Spotlight outside Slimference. CA management belongs
  under Manage Slimference as an explicit advanced Desktop/Lab action, not as
  the default daily launch path.

2026-05-22 app-server shim update:

- T246 found a cleaner Desktop diagnostic route that does not need CA: launch
  Codex.app with process-local `CODEX_CLI_PATH=<slimference>`, then the hidden
  shim execs the real `codex app-server` with local provider overrides.
- Current live proof still ends as `desktop_connect_only_no_app_server_bytes`.
  This confirms CA trust is not the blocker for the app-server shim. T245
  remains useful for explicit legacy proxy diagnostics and global lab mode only.

2026-05-20 non-live closure:

- `codex desktop status` now treats missing CA material as relevant only for
  legacy proxy diagnostics, not as a gate for the Desktop app-server shim
  diagnostic. The diagnostic launch command is
  `slimference codex launch-desktop --transport=app-server`; the proxy command
  remains `slimference codex launch-desktop --transport=proxy --with-ca-env`.
- The proof commands remain the Desktop savings gate:
  `slimference codex desktop prove --manual --json`, then
  `slimference codex desktop prove --finish --json` after a user prompt.
  Current TUI Launch Codex App blocks because that gate is not green.
  Historical or live `tls_trust_rejected` counters are a proof failure, not a
  permanent global failure and not a savings claim.
- Aggregate health no longer requires Keychain trust; Keychain is a separate
  Desktop/Lab fallback signal. CLI WSS remains independent of CA trust.

## Deviations

None yet.
