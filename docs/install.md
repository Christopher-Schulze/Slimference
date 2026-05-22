# Slimference — install / uninstall

This is the **single source of truth** for installing and removing
Slimference. Both humans and agents can drive the flow from this
document.

## TL;DR

```bash
slimference install      # one-shot, atomic, reversible, Codex-only default
slimference status       # see what's currently armed
slimference status --preflight
slimference codex run -- <prompt>     # scoped one-shot Codex CLI, fail-open
slimference codex run --transport=auto -- <prompt> # WSS-first scoped Codex CLI
slimference codex certify wss --dry-run  # inspect WSS auto-promotion proof
slimference codex recertify wss --dry-run # inspect update repair plan
slimference codex desktop status      # Desktop app-server shim readiness / proof status
slimference codex desktop prove --manual --json # diagnostic Desktop proof start
slimference codex desktop prove --finish --json # diagnostic Desktop proof finish
slimference codex launch-desktop --probe  # inspect process-local Desktop app-server env
slimference enable                    # optional shared Codex CLI/App route
slimference enable --transport=wss    # optional shared WSS route, pre-live-cert
slimference codex status
slimference disable

# Global lab only, not the default because it also routes Browser ChatGPT
# and ChatGPT.app:
slimference lab cert-trust
slimference lab root-arm --global-chatgpt-hosts
slimference lab enable
slimference lab disable
slimference lab root-disarm
slimference uninstall    # full removal, restores backups
```

That's it. No persistent environment variables. No `OPENAI_API_BASE`.
No system-wide `HTTPS_PROXY`. No global `chatgpt.com` host route unless
the operator explicitly asks for the global lab path. The preferred Desktop
launcher uses Codex.app's process-local `CODEX_CLI_PATH` app-server hook and
does not need CA trust or TLS MITM.

## Scoped Codex architecture

Running `slimference` with no arguments opens the Launch Center TUI. The normal
human entrypoints there are:

- Launch Codex CLI
- Launch Codex App
- Savings
- Status
- Manage Slimference

There is no separate "open direct" action. Direct mode is the native launch:
`codex` in a normal shell or Codex.app from Finder/Spotlight. Slimference mode
is the launch path chosen inside the TUI. The Desktop item is visible, but it
launches only after the Desktop app-server shim proof is green. Historical
proxy/CA failures (`desktop_tls_blocked` / `tls_trust_rejected`) are shown as
old diagnostic proof state, never as active Desktop savings. Direct mode is
still available by launching Codex.app normally from Finder/Spotlight.

Launch Center strips inherited `CODEX_*` session variables before starting a
new Codex CLI or proven Codex.app Slimference process. This prevents a
Slimference session that was opened from inside Codex from leaking
`CODEX_THREAD_ID` or other old runtime state into the newly launched app. The
Desktop launch also pins `PWD` to the current folder when the proof gate allows
it.

Slimference's default product path touches only scoped Codex surfaces:

1. **Hook callouts** in `~/.codex/hooks.json` plus
   `~/.codex/config.toml` `[features].hooks=true` (out-of-band
   subprocess calls — never over network).
2. **Scoped Codex CLI traffic** via
   `slimference codex run -- <prompt>`. This launches only
   that Codex CLI process with the local `slimference-codex` provider. It
   does not touch `/etc/hosts`, pfctl, macOS Network Proxy settings,
   Browser ChatGPT, or ChatGPT.app.
3. **Optional shared Codex CLI/App route** via `slimference enable`
   (alias: `slimference codex enable`).
   This writes a marker-owned provider block to `~/.codex/config.toml`:
   `model_provider="slimference-codex"`,
   `base_url="http://127.0.0.1:8990/backend-api/codex"`,
   `requires_openai_auth=true`, transport-dependent
   `supports_websockets=<false|true>`, and `wire_api="responses"`.
   The default transport is stable HTTP (`supports_websockets=false`).
   Explicit `--transport=wss` enables scoped Responses WebSockets and
   routes local Codex WSS upgrades through the Phase-F frame adapter. It
   is reversible with
   `slimference disable` (alias: `slimference codex disable`) and still leaves Browser ChatGPT,
   ChatGPT.app, Claude Code, `/etc/hosts`, pfctl, and system proxy settings
   untouched.
4. **Process-local Codex Desktop app-server shim** via
   `slimference codex desktop prove` and, only after a future green proof,
   `slimference codex launch-desktop --transport=app-server --replace-existing`.
   This does not
   write Codex config, shell startup files, macOS system proxy, `/etc/hosts`, or
   pfctl. It sets `CODEX_CLI_PATH` only on the spawned Codex.app process tree so
   Codex.app starts Slimference as its app-server shim. Normal
   Finder/Spotlight Codex.app launches remain direct. The proof command
   snapshots daemon WSS state, launches one scoped Codex.app process, observes a
   bounded delta, and cleans up. It exits zero only when Desktop Phase-F savings
   are proven for that observation window. In manual mode it may also report
   `desktop_ready_for_prompt`: the app is open with scoped Slimference env, but
   savings are not claimed until the user sends a prompt and the finish step
   sees bytes, frames, and mutation.

The global transparent TLS-MITM path still exists for lab certification:
local CA in Keychain, `/etc/hosts`, pfctl, and the SNI listener on 8443.
It is deliberately not the default now because `chatgpt.com` routing is
host-wide on macOS. Even with byte-equal passthrough, Browser ChatGPT and
ChatGPT.app enter Slimference's bridge, which violates the scoped product
goal.

### Surface governance

| Surface | Product role | Default install? | Normal command |
|---|---:|---:|---|
| Codex hooks | Signal/local output layer | yes | `slimference install` |
| Scoped Codex provider route | Codex CLI/App traffic layer | optional | `slimference enable` |
| One-shot scoped Codex CLI | Safe test/recovery path | no persistent state | `slimference codex run -- <prompt>` |
| Process-local Codex Desktop proof | Desktop diagnostic gate | no persistent state | `slimference codex desktop prove --manual --json` then `--finish` |
| Process-local Codex Desktop launcher | Desktop proven launch only; currently proof-gated | no persistent state | `slimference codex launch-desktop --transport=app-server --replace-existing` |
| Global transparent MITM | Lab certification only | no | `slimference lab ...` |
| Legacy proxy/env/integrate | Advanced compatibility | no | `slimference proxy ...`, `slimference integrate ...` |
| Base-URL Desktop launcher mode | Diagnostic/future-proof only | no | `slimference codex launch-desktop --transport=base-url --probe` |

Normal `enable` and `disable` never arm global routing. Global lab routing is
always explicit and visually separate.

Claude Code is deliberately **not** part of the product install. Its
hook/parser code stays in tree for reference and possible future work,
but `slimference install` does not write `~/.claude`, does not install
Claude hooks, and `--with-claude` is accepted only as a parked no-op for
old scripts. Use RTK for Claude Code while Slimference focuses on Codex
CLI and Codex Desktop.

## Fail-open guarantees

| Event | What happens |
|---|---|
| Daemon unavailable during `slimference codex run` | The wrapper prints a warning and launches direct Codex. **No CLI breakage.** |
| Daemon unavailable while persistent `codex enable` route is active | Only Codex CLI/App are affected. Run `slimference codex disable`, press `[r]` in TUI Setup, or restart the daemon. Browser ChatGPT, ChatGPT.app, Claude Code, and generic OpenAI clients remain direct. |
| CA missing during Desktop proxy diagnostics | The preferred Desktop app-server shim does not need CA trust. Legacy proxy diagnostics refuse before a savings claim and print the repair command. Direct Codex.app remains native. |
| Codex Desktop already running during scoped proof/TUI launch | `codex desktop prove` and TUI Launch Codex App use `--replace-existing`: they quit the existing Codex.app main process, verify it is gone, then spawn the scoped Slimference instance so macOS cannot reuse a stale env. Raw `codex launch-desktop` still refuses unless `--replace-existing` is passed. Direct Codex.app remains native. |
| Codex Desktop app-server shim fails proof | Desktop proof exits non-zero with an explicit failure class. TUI Launch Codex App blocks instead of opening direct or a broken Slimference session, because the TUI item means "Slimference mode". Normal Finder/Spotlight Codex.app remains direct. CLI savings continue. |
| Codex CLI/Desktop updates | `transport=auto` is WSS-first. It uses certified WSS Phase-F when green, WSS byte-equal bridge when mutation proof is stale but the bridge proof is clean, HTTP only when WSS bridge is unsafe, and direct only when the daemon cannot serve the scoped run. Background recert tries to restore Phase-F savings without blocking the user. |
| `slimference codex disable` while Codex is open | The marker-owned provider block is removed. New Codex CLI/App sessions go direct after config reload / app-server restart. |
| `slimference disable` while global lab traffic is in flight | Engine accepts current connections, reverts daemon SNI mode. Use `root-disarm` to remove privileged hosts/pfctl routing. |
| CA removed from Keychain externally | Only global lab MITM is affected. Scoped Codex provider routing does not need Keychain trust. |

## Human walkthrough

### 1. Install

```bash
slimference install
```

What happens:

1. Generates a local CA under `~/.slimference/ca/`.
2. Leaves macOS Keychain untouched by default. CLI WSS does not need CA
   trust, and Desktop proof first uses process-local
   `CODEX_CA_CERTIFICATE` from the launched app process.
3. Installs `~/Library/LaunchAgents/com.slimference.proxy.plist` and
   loads it via `launchctl`.
4. Patches `~/.codex/hooks.json` + writes hook scripts to
   `~/.slimference/hooks/codex-*.sh`.
5. Writes `~/.codex/SLIMFERENCE.md` explaining what was changed and
   how to revert.
6. Does **not** touch Claude Code. `--with-claude` is currently a
   parked no-op for backwards-compatible command lines.
7. Does **not** touch `/etc/hosts` yet.

After install, verify with:

```bash
slimference status
```

You should see CA material present, daemon running, hosts CLEAN.

### 2. Run scoped Codex CLI

This is the normal granular path. It affects only the spawned Codex CLI
process; Browser ChatGPT, ChatGPT.app, and Claude Code stay direct.

```bash
slimference status --preflight
slimference codex run -- "say hi"
```

Expected telemetry: Codex CLI flights appear under the daemon's proxy
records with `provider=codex_chatgpt` and
`path=/backend-api/codex/responses`. No `/etc/hosts`, pfctl, or Keychain
trust is required for this path.

Advanced WSS certification mode:

```bash
slimference codex run --transport=wss -- "say hi"
```

This keeps the same Codex-only scope but asks Codex to use Responses
WebSockets against the local provider. The daemon reads matching Codex
Upgrade requests before `net/http` normalises them, forwards the raw
header upstream with only Host/request-target normalization, and then
runs post-101 frames through `wsmitm.Session` plus the Phase-F WSS
adapter. Known Codex request/response frames can be compacted, including
Responses `response_item.payload` wrappers and split WSS tool-call state;
unknown, binary, control, or malformed frames degrade to byte-equal forwarding.

`--transport=auto` is WSS-first. It evaluates the current Codex/Slimference
tuple and chooses the safest high-savings mode in this order:

1. `wss_phasef`: certified WSS Phase-F mutation for the current tuple. This is
   the max-savings target.
2. `wss_bridge`: native WSS byte-equal bridge when Phase-F proof is stale but a
   clean bridge proof exists. This keeps Codex on WSS and starts repair instead
   of jumping straight to HTTP.
3. `http`: stable scoped Responses HTTP fallback when WSS bridge is unavailable
   or unsafe.
4. `direct`: final fail-open when the daemon cannot serve the scoped run.

T224 capture/diff remains the gate for indistinguishability wording; the local
auto selector is gated by local proof files and never treats an uncertified
mutation path as safe.

After a live scoped WSS run has actually mutated Phase-F frames, issue the
local proof through the CLI, never by hand:

```bash
slimference codex certify wss --dry-run
slimference codex certify wss --operator "operator-name" --notes "live proof"
```

The command reads `/admin/state`, requires zero WSS parser, degradation, and
compression errors, requires `frames_reencoded>0` plus
`compressed_messages_mutated>0`, and writes a version-bound
`~/.slimference/codex-wss-cert.json` only when the daemon is reachable and
the current observation cycle is green. When Codex or Slimference updates,
`slimference codex status --json` reports the current tuple, the certified
tuple, `auto.needs_recert=true`, the recert state path, and
`auto.recert_command`.

Repair is shared by CLI, background auto-recert, and the TUI Manage action:

```bash
slimference codex recertify wss --dry-run --json
slimference codex recertify wss --operator "operator-name" --notes "current tuple repair"
```

`recertify wss` uses a temporary git repo plus real `codex exec` turns through
`slimference codex run --transport=wss`, snapshots `/admin/state` before and
after, and evaluates only the delta window. If Phase-F mutation is green it
writes the normal WSS certification. If mutation is not green but byte-equal
WSS is clean, it writes `~/.slimference/codex-wss-bridge.json` and leaves
`transport=auto` on WSS bridge rather than HTTP. Failed repairs persist
`~/.slimference/codex-wss-recert.json` and a bounded
`~/.slimference/logs/codex-wss-recert.log` with one rotation at 2 MiB. Prompt
bodies, auth tokens, and large tool outputs are not logged.

Preferred repair trigger for local operators:

```bash
slimference codex recertify wss --force --operator "operator-name" --notes "current tuple live repair"
slimference codex status --json | jq '.auto'
```

Expected Phase-F success counters inside the recert delta window:
`frames_reencoded>0`, `compressed_messages_mutated>0`,
`mutation_active=true`, `byte_bridge_only=false`, and
`parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`. The
temporary repo used by `recertify` is only a deterministic way to produce
repeatable Codex tool-output traffic; it does not touch the Slimference
checkout or any global network setting.

### 3. Launch Codex Desktop through the app-server shim

This is the preferred Desktop proof candidate. It is scoped to one spawned
Codex.app process tree and avoids the old TLS-MITM problem entirely. No
Keychain trust, local CA, `HTTPS_PROXY`, Electron proxy argument, `/etc/hosts`,
pfctl, or persistent Codex config mutation is required.

The launcher uses a supported Codex Desktop process boundary:

1. Codex.app honors `CODEX_CLI_PATH` when it starts its bundled Rust
   `codex app-server`.
2. Slimference sets `CODEX_CLI_PATH=<slimference binary>` only for the spawned
   Codex.app process.
3. Codex.app starts `slimference app-server ...`.
4. The hidden Slimference app-server shim immediately `exec`s the real Codex CLI
   binary as `codex app-server`, adding process-local `-c` endpoint and
   provider overrides:
   `openai_base_url=http://127.0.0.1:8990/backend-api/codex`,
   `chatgpt_base_url=http://127.0.0.1:8990/backend-api/`,
   `model_provider=slimference-codex`,
   `model_providers.slimference-codex.base_url=http://127.0.0.1:8990/backend-api/codex`,
   `requires_openai_auth=true`, `supports_websockets=true`, and
   `wire_api=responses`.
5. Codex's own WSS client then opens the local Slimference WSS route. That is
   the same no-CA route already proven by the scoped Codex CLI smoke test.

Inspect the exact scoped environment without launching:

```bash
slimference codex desktop status
slimference codex launch-desktop --transport=app-server --probe
```

The expected probe shows `CODEX_CLI_PATH`, `SLIMFERENCE_CODEX_DESKTOP_ACTIVE`,
`SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN`,
`SLIMFERENCE_CODEX_DESKTOP_BASE_URL`, and `NO_PROXY` only on the spawned
Codex.app process. It must not show proxy variables, CA variables, or old
base-URL env override guesses.

Before making any Desktop Slimference savings claim, run the proof gate:

```bash
slimference codex desktop prove --manual --duration=15s --json
```

The manual proof launches one scoped Codex.app process, snapshots daemon WSS
state, and keeps the app open only when the launch is viable. If the result is
`desktop_ready_for_prompt`, send one prompt in that exact Codex.app window, then
finish the proof:

```bash
slimference codex desktop prove --finish --json
```

The finish step compares current daemon WSS state against the saved manual
baseline. Desktop savings are green only for
`desktop_app_server_phasef_proven`, meaning bytes flowed both directions,
WSS frames reached Phase-F, mutation happened, and parser/degrade/compression
counters stayed zero. WSS byte-equal bridge is useful compatibility evidence
but not a Desktop savings claim. Zero bytes, no WSS delta after the prompt,
launch failure, daemon failure, or unreviewed daemon-wide WSS activity are
diagnostics only.

If a normal Codex.app instance is already running, the proof command quits that
main process, verifies it is gone, then launches the scoped Slimference instance.
This prevents macOS from foregrounding an app that did not inherit the scoped
Slimference env. The raw launcher keeps the safer default and refuses while an
app is running unless `--replace-existing` is passed explicitly:

```bash
slimference codex launch-desktop --transport=app-server --replace-existing
```

Only when the proof gate is green may the daily TUI Launch Codex App item use:

```bash
slimference codex launch-desktop --transport=app-server --replace-existing
```

The launcher starts the app as a detached process-local session, uses the Codex
bundle executable directory as the child working directory, scrubs inherited
`CODEX_*` runtime state, pins `PWD` when the TUI supplies a selected folder, and
waits for a short startup probe. If Codex.app exits immediately, the command
fails and prints the exit status or signal instead of claiming a successful
launch.

Manual external proof can still be collected when diagnosing a new Codex.app
build:

```bash
lsof -nP -p <codex-app-server-pid> -iTCP -sTCP:ESTABLISHED
curl -s http://127.0.0.1:8990/_slimference/admin/state | jq '.wss'
slimference codex desktop status --json
```

The proof must show Desktop-specific WSS counter deltas after the prompted app
session, not merely historical daemon counters. Relaunching Codex.app from
Finder/Spotlight must return to native direct ChatGPT routing and must not use
the app-server shim.

Important: `/_slimference/admin/state` `.wss` counters are daemon-wide. They
can include Codex CLI recertification or smoke-test traffic.
`slimference codex desktop status` therefore reports `wss_counters_scope=
daemon_cumulative_not_desktop_proof` and keeps `conversation_observed=false`
until a Desktop-specific pre/post delta is tied to the spawned app-server proof
session.

The old process-local proxy branch remains available only for advanced
diagnostics:

```bash
slimference codex launch-desktop --transport=proxy --with-ca-env --probe
slimference codex launch-desktop --transport=proxy --with-ca-env
```

That branch is not the preferred product route. The 2026-05-22 live proxy proof
showed CONNECT reached Slimference and Electron proxy args removed Chromium's
direct-socket bypass, but a real prompt still produced zero application bytes
and zero WSS frames. The app-server shim exists specifically to avoid that
TLS/root-store barrier.

`--transport=base-url` remains available only as a diagnostic/future-proof probe
for upstream Codex versions that might later add a conversation base-URL env
hook. It is not the current Desktop product route.

### 4. Enable shared Codex CLI/App route

Use this only when you want regular Codex CLI and Codex Desktop App
sessions to use Slimference by default:

```bash
slimference enable
slimference codex status
```

The route is shared because Codex exposes one active `model_provider`
setting. Treat CLI/App as a single Codex switch until a separate
Desktop-only launcher is live-proven. Disable it with:

```bash
slimference disable
```

To persist WSS for both Codex CLI and any Desktop/App-server process that
honors `~/.codex/config.toml`, use:

```bash
slimference enable --transport=wss
```

This still does not touch Browser ChatGPT, ChatGPT.app, Claude Code, global
proxy settings, `/etc/hosts`, or pfctl. Desktop behavior remains a proof
item: do not claim Desktop interception until daemon telemetry shows real
Codex Desktop traffic.

### 5. Global transparent lab mode

Do not do this unless you are deliberately testing the machine-wide
transparent MITM path. It routes `chatgpt.com` and `api.openai.com`
for the whole user session, including Browser ChatGPT and ChatGPT.app.

```bash
slimference lab cert-trust
slimference lab root-arm --global-chatgpt-hosts
slimference lab enable
```

What happens:

1. `lab cert-trust` opens Keychain Access on the local root cert. The
   user must set it to "Always Trust" for SSL.
2. `lab root-arm --global-chatgpt-hosts` writes the marker-fenced Codex-only IPv4 hosts block and
   installs the pfctl rdr anchor from port 443 to 127.0.0.1:8443. It
   does not write `api.anthropic.com` and does not install IPv6 `::1`
   mappings.
3. `lab enable` sets `transparent.sni_peek_mode = true` in the resolved
   config path. The canonical default is
   `~/.config/slimference/config.toml`.
4. `lab enable` sends `SIGHUP` to the running daemon (PID read from
   `~/.slimference/run/daemon.pid`).
5. The daemon's SIGHUP handler reads the new flag and starts or stops
   the SNI-peek listener.

If the daemon is not running, the flag is still written; the next
`slimference daemon start` (or boot via launchd) will apply hosts and
arm the listener.

### 6. Disarm global lab mode

```bash
slimference disable
```

Writes `transparent.sni_peek_mode = false` and SIGHUPs the daemon.
Use `slimference root-disarm` to remove the privileged hosts/pfctl
routing block when you want Codex to go direct again.

### 7. Uninstall

```bash
slimference uninstall
```

Reverses the install plan in LIFO order:

1. `hooks.codex` reverted
2. `notice.codex` removed if still marker-owned
3. `launchd` unloaded + plist removed
4. CA trust removed from Keychain when present and supported by the selected
   Keychain runner
5. CA material rotated aside to `~/.slimference/ca.bak.<unix>/`

`--with-claude` is accepted for old automation but does not remove
Claude files. Slimference does not own `~/.claude` in Codex-only mode.

Backups live in `~/.slimference/backups/` and are NOT removed by
uninstall — they remain for forensic recovery.

## Agent-readable specification

Agents and scripts that automate the install consume this YAML block:

```yaml
schema_version: 1

slimference_install:
  install_plan:
    - step: ca.generate
      apply: writes CA cert + key under ~/.slimference/ca/
      reverse: moves files aside to ~/.slimference/ca.bak.<unix>/
      inspect: present | absent | rotated
      idempotent: true
    - step: launchd.install
      apply: writes ~/Library/LaunchAgents/com.slimference.proxy.plist; loads it
      reverse: unloads + removes plist
      inspect: present | absent
      idempotent: true
    - step: hooks.codex
      apply: marker-fenced patch in ~/.codex/hooks.json; writes scripts under ~/.slimference/hooks/
      reverse: removes patch block + scripts byte-equal to pre-state
      inspect: present | absent
      idempotent: true
    - step: notice.codex
      apply: writes ~/.codex/SLIMFERENCE.md with marker, explaining what we changed and how to revert
      reverse: removes the file ONLY if our marker line is still present (human-replaced files are left alone)
      inspect: present | absent
      idempotent: true
  hosts_plan:
    - step: hosts.patch
      apply: patches /etc/hosts with marker fence; redirects chatgpt.com, api.openai.com → 127.0.0.1
      reverse: removes marker-fenced block
      inspect: present | absent
      idempotent: true
      privilege: requires root; driven by `slimference lab root-arm --global-chatgpt-hosts` via one macOS admin prompt

  commands:
    install:    install_plan.apply
    install --with-keychain: install_plan.apply plus optional ca.keychain trust step for Desktop/lab fallback
    uninstall:  install_plan.reverse
    enable:     alias for codex enable; writes marker-owned shared Codex CLI/App provider route
    disable:    alias for codex disable; removes marker-owned shared Codex CLI/App provider route
    lab cert-trust: open Keychain Access on ~/.slimference/ca/root.crt for interactive trust
    lab root-arm:   advanced global hosts + pfctl activation for Codex hosts; requires --global-chatgpt-hosts
    lab enable:     write_config_field(transparent.sni_peek_mode = true) + SIGHUP daemon
    lab disable:    write_config_field(transparent.sni_peek_mode = false) + SIGHUP daemon
    lab root-disarm: privileged hosts + pfctl deactivation
    codex run:  one-shot scoped Codex CLI provider route with direct fallback
    codex enable: write marker-owned shared Codex CLI/App provider route
    codex disable: remove marker-owned shared Codex CLI/App provider route
    codex status: inspect shared Codex provider route + daemon health
    status:     emit /admin/state JSON

  exit_codes:
    0: success
    1: invalid argument / config error
    2: privilege required (uncommon — install handles its own privilege escalation)
    3: rollback required (Apply partially failed)
    4: prerequisite missing (e.g. enable without install)

  owned_paths:
    - ~/.slimference/              # state dir
    - ~/.slimference/ca/           # CA material
    - ~/.slimference/backups/      # snapshot dir (preserved on uninstall)
    - ~/.slimference/run/          # PID file, sockets
    - ~/Library/LaunchAgents/com.slimference.proxy.plist

  fenced_edits:
    - file: /etc/hosts
      marker_start: "# slimference:start"
      marker_end:   "# slimference:end"
    - file: ~/.codex/hooks.json
      marker_field: slimference_managed
    - file: ~/.codex/config.toml
      marker_start: "# >>> slimference codex route >>>"
      marker_end:   "# <<< slimference codex route <<<"
      purpose: optional shared Codex CLI/App provider route

  not_touched:
    - env: OPENAI_API_BASE
    - env: OPENAI_BASE_URL
    - env: CHATGPT_BASE_URL
    - env: HTTPS_PROXY
    - env: HTTP_PROXY
    - file: ~/.codex/config.toml openai_base_url field
    - macos: system network proxy settings
    - file: ~/.claude/*
    - host: api.anthropic.com
```

### Why we don't touch env vars or HTTPS_PROXY

The scoped CLI path uses a per-process Codex provider override and does
not require persistent env vars. Persistent `OPENAI_API_BASE` or
`HTTPS_PROXY` would leak beyond the single intended process and recreate
the multi-surface ambiguity Phase H removed.

The transparent lab path is universal. That universality is why it can
catch Codex 0.130's hardcoded WSS URL, and exactly why it is no longer
the default for a machine where Browser ChatGPT and ChatGPT.app must stay
direct.

## Verification

```bash
slimference status              # human-readable table
slimference status --preflight  # adds DoH upstream checks without Codex traffic
slimference codex status        # scoped Codex provider route + daemon health
slimference status --json | jq  # machine-readable
curl http://127.0.0.1:8990/_slimference/admin/state | jq
```

The WSS transport block is under `/_slimference/admin/state` at `.wss`:

- `engine_active=true`: a WSS dispatcher is installed in the daemon. This can
  be the scoped Codex WSS bridge or the global SNI-peek dispatcher.
- `frames_forwarded>0 && frames_reencoded=0`: byte-equal bridge only.
- `frames_reencoded>0`: Phase F mutation happened on WSS frames.
- `compressed_messages_inspected>0`: negotiated `permessage-deflate` payloads
  were decoded successfully.
- `compressed_messages_mutated>0`: a decoded compressed message was changed and
  re-encoded with RSV1.
- `compression_errors>0`: the codec failed and the session fell back to
  byte-equal forwarding for that compressed direction. This also covers
  compressed/inflated message size caps; normal Codex traffic should keep it 0.
- `phasef_requests`, `phasef_text_deltas`, `phasef_terminal_responses`, and
  `phasef_mutations`: request/response envelopes reached the Phase F adapter
  and whether one of them changed the frame body.
- `degraded_sessions>0` or `parse_failures>0`: schema drift or malformed frames
  triggered fail-open byte bridging.

WSS streamcut is intentionally not part of the scoped WSS product path yet. The
HTTP/SSE streamcut relay remains enabled, but Codex WSS early-cut needs the
terminal-safe T236 proof before it can be enabled.

The scoped product route block is under `/admin/state.codex_route`:

- `enabled=true && complete=true && daemon_reachable=true`: shared Codex
  CLI/App route is configured and the daemon is reachable.
- `transport=http|wss`: the currently written marker-owned provider route.
- `auto_mode=wss_phasef|wss_bridge|http|direct`, `auto_transport=http|wss`,
  `wss_certified`, `wss_bridge_available`, `needs_recert`, and
  `fallback_reason`: how `--transport=auto` resolves right now.
- `certification_path`, `bridge_proof_path`, and `recert_state_path`: local
  proof files consumed by `slimference codex certify wss`,
  `slimference codex recertify wss`, and `--transport=auto`.
- `daemon_error` or `fallback_reason` non-empty: do not promote WSS by default
  until the reason is cleared or live-certified.

Current scoped proof stack (2026-05-18):

- `go run ./scripts/ci` uses the formal
  `go run ./scripts/coverage -min=95.0` aggregate gate. Behavior-critical
  product paths still require real tests; the project does not chase
  artificial coverage for impossible OS edge branches.
- Targeted race check passes:
  `go test ./internal/proxy ./cmd/slimference ./internal/codexroute -race -count=1 -timeout 240s`.
- Scoped raw WSS pre-live checks pass: raw Upgrade header order/casing is
  preserved on the existing `:8990` listener, non-Codex requests replay
  through the normal HTTP server, and the T224 parser can parse a
  synthetic WSS capture without tshark.
- Historical live scoped Codex CLI WSS certification is complete for Codex CLI
  `0.130.0` plus Slimference `2.0.2`: real WSS Phase-F mutation produced
  `frames_reencoded=1`, `compressed_messages_mutated=1`,
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`, and
  `transport=auto` resolves to WSS for that tuple. New Codex CLI versions need
  `slimference codex recertify wss` to restore `wss_phasef`; until then auto
  prefers `wss_bridge` if a current clean bridge proof exists, then HTTP. Do not
  run global lab commands (`lab cert-trust`, `lab root-arm
  --global-chatgpt-hosts`, `lab enable`) from the active Codex Desktop
  development session.

The transparent listener readiness bit is
`/admin/state.listener.bound_on_sni_peek` (default port 8443). Admin
port 8990 being up is not enough for live interception. If
`hosts_active=true` but `bound_on_sni_peek=false`, run
`slimference root-disarm` from the recovery shell before using Codex.

Before scoped live certification or Desktop proof, run:

```bash
slimference status --preflight
```

Expected preflight: DoH resolves `chatgpt.com` and `api.openai.com` to
non-loopback upstream IPs, Codex CLI/Desktop app policy is enabled, Claude
Code remains inactive, and no `api.anthropic.com` hosts route is present in
Codex-only mode. This preflight does not start Codex and does not arm
Keychain, hosts, or pfctl.

Scoped live tests start from disarmed preflight state: admin health can be up
on `127.0.0.1:8990`, but `:8443` should be off, hosts should be inactive,
and CA trust should still be untrusted unless a global lab test is explicitly
approved. The scoped CLI sequence is
`status --preflight` -> `codex run -- <prompt>` ->
`codex run --transport=auto -- <prompt>` -> `/admin/state` telemetry and
T224 capture check. The shared CLI/App proof sequence is
`enable --transport=wss` -> restart Codex.app/app-server -> prompt
-> telemetry check -> `disable`. The old global sequence is now lab-only:
`lab cert-trust` -> `lab root-arm --global-chatgpt-hosts` ->
`lab enable` -> smoke -> `lab disable` -> `lab root-disarm`.

## Dry-run

Every install/uninstall command supports `--dry-run`:

```bash
slimference install --dry-run --json
slimference uninstall --dry-run
```

This calls `Plan.Inspect()` on the underlying reversibility plan and
prints per-step state without touching anything.

## Recovery

If something went wrong:

```bash
# Most common: rollback a partial install
slimference uninstall

# Nuclear option: if uninstall itself fails, restore from backups
ls ~/.slimference/backups/

# Hosts file (requires root):
sudo cp ~/.slimference/backups/hosts.bak.<latest> /etc/hosts

# Codex hooks (no root needed):
cp ~/.slimference/backups/codex_config.bak.<latest> ~/.codex/config.toml

# Rotate CA aside manually:
mv ~/.slimference/ca ~/.slimference/ca.attic.$(date +%s)

# Keychain: open `Keychain Access`, search "Slimference", delete the
# entry. Or:
security delete-certificate -c "Slimference Root CA" \
    ~/Library/Keychains/login.keychain-db
```

## Flags reference

```
slimference install [flags]
  --dry-run         show what would happen without changing anything
  --json            machine-readable output (with --dry-run)
  --no-hooks        skip the hook integrations
  --with-claude     compatibility no-op; Claude Code is parked
  --no-autostart    skip the launchd plist install
  --with-keychain   opt into macOS Keychain trust for Desktop/lab fallback
  --no-keychain     compatibility no-op; default install already skips Keychain
  --system          with --with-keychain, install CA into System Keychain
  --help, -h        show help

slimference uninstall [flags]
  --dry-run         show what would happen without changing anything
  --keep-ca         skip Keychain trust cleanup; CA material still rotates aside
  --no-keychain     skip Keychain trust cleanup
  --with-claude     compatibility no-op; Slimference does not own ~/.claude
  --system          uninstall from the system Keychain
  --help, -h        show help

slimference enable | disable [flags]
  --transport=auto|http|wss  scoped Codex route transport. auto resolves
                    wss_phasef -> wss_bridge -> http -> direct.
  --host=HOST       Slimference daemon host (default 127.0.0.1)
  --port=PORT       Slimference daemon port (default 8990)
  --dry-run         print marker-owned Codex config block only
  --help, -h        show help

slimference codex certify wss [flags]
  --dry-run         print the certification JSON without writing it
  --operator NAME   record the local operator that verified the live proof
  --notes TEXT      record short local proof notes
  --host=HOST       Slimference daemon host (default 127.0.0.1)
  --port=PORT       Slimference daemon port (default 8990)

slimference codex recertify wss [flags]
  --dry-run         print the repair plan without live Codex calls
  --json            machine-readable result
  --force           bypass cooldown, but not the active recert lock
  --no-write        run the proof without writing cert/bridge proof files
  --operator NAME   record the local operator
  --notes TEXT      record short local proof notes
  --timeout=DUR     timeout per live Codex trigger command
  --host=HOST       Slimference daemon host (default 127.0.0.1)
  --port=PORT       Slimference daemon port (default 8990)

slimference lab enable | disable [flags]
  --config=PATH     override config.toml location. CAUTION: must match
                    the path the daemon was started with (default
                    ~/.config/slimference/config.toml). If the daemon
                    was launched via `slimference --config=X daemon
                    start`, you MUST pass the same X here, otherwise
                    the daemon's SIGHUP reads the wrong file. Use the
                    default path unless you have a specific reason.
  --help, -h        show help

slimference status [flags]
  --json            machine-readable JSON output
  --preflight       perform DoH upstream checks for Codex hosts
  --help, -h        show help
```

## See also

- [`AGENTS.md`](../AGENTS.md) — repository-wide rules for agents and
  human developers.
- `docs/transparent-mode.md` — internals, schema details, debugging
  the transparent MITM layer.
- `docs/todo/t200-phase-h-single-entry-point-epic.md` — design history
  for the 2-surface consolidation.
