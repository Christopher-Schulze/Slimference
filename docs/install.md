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
slimference codex run --transport=wss -- <prompt>  # scoped WSS power mode, pre-live-cert
slimference codex enable              # optional shared Codex CLI/App route
slimference codex enable --transport=wss  # optional shared WSS route, pre-live-cert
slimference codex status
slimference codex disable

# Global lab only, not the default because it also routes Browser ChatGPT
# and ChatGPT.app:
slimference cert-trust
slimference root-arm --global-chatgpt-hosts
slimference enable
slimference disable
slimference root-disarm
slimference uninstall    # full removal, restores backups
```

That's it. No environment variables. No `OPENAI_API_BASE`. No
`HTTPS_PROXY`. No global `chatgpt.com` host route unless the operator
explicitly asks for the global lab path.

## Scoped Codex architecture

Slimference's default product path touches only scoped Codex surfaces:

1. **Hook callouts** in `~/.codex/hooks.json` plus
   `~/.codex/config.toml` `[features].hooks=true` (out-of-band
   subprocess calls — never over network).
2. **Scoped Codex CLI traffic** via
   `slimference codex run -- <prompt>`. This launches only
   that Codex CLI process with the local `slimference-codex` provider. It
   does not touch `/etc/hosts`, pfctl, macOS Network Proxy settings,
   Browser ChatGPT, or ChatGPT.app.
3. **Optional shared Codex CLI/App route** via `slimference codex enable`.
   This writes a marker-owned provider block to `~/.codex/config.toml`:
   `model_provider="slimference-codex"`,
   `base_url="http://127.0.0.1:8990/backend-api/codex"`,
   `requires_openai_auth=true`, transport-dependent
   `supports_websockets=<false|true>`, and `wire_api="responses"`.
   The default transport is stable HTTP (`supports_websockets=false`).
   Explicit `--transport=wss` enables scoped Responses WebSockets and
   routes local Codex WSS upgrades through the Phase-F frame adapter. It
   is reversible with
   `slimference codex disable` and still leaves Browser ChatGPT,
   ChatGPT.app, Claude Code, `/etc/hosts`, pfctl, and system proxy settings
   untouched.

The global transparent TLS-MITM path still exists for lab certification:
local CA in Keychain, `/etc/hosts`, pfctl, and the SNI listener on 8443.
It is deliberately not the default now because `chatgpt.com` routing is
host-wide on macOS. Even with byte-equal passthrough, Browser ChatGPT and
ChatGPT.app enter Slimference's bridge, which violates the scoped product
goal.

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
| Codex CLI/Desktop updates | The scoped HTTP provider path avoids the WSS parser. Scoped WSS and global lab WSS both fall back to byte-equal frame bridging on schema drift; savings disappear until the parser is updated, but unknown frames are not blocked. |
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
2. Prepares the Keychain trust step. On modern macOS the explicit
   "Always Trust" decision is interactive, so treat `slimference
   cert-trust` as the supported trust path before live arming.
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
adapter. Known Codex request/response frames can be compacted; unknown,
binary, control, or malformed frames degrade to byte-equal forwarding.

Until the live T224 capture passes, `--transport=auto` is intentionally an
alias for the stable HTTP route. Raw scoped WSS is available for
certification and power users, not yet the default claim.

### 3. Enable shared Codex CLI/App route

Use this only when you want regular Codex CLI and Codex Desktop App
sessions to use Slimference by default:

```bash
slimference codex enable
slimference codex status
```

The route is shared because Codex exposes one active `model_provider`
setting. Treat CLI/App as a single Codex switch until a separate
Desktop-only launcher is live-proven. Disable it with:

```bash
slimference codex disable
```

To persist WSS for both Codex CLI and any Desktop/App-server process that
honors `~/.codex/config.toml`, use:

```bash
slimference codex enable --transport=wss
```

This still does not touch Browser ChatGPT, ChatGPT.app, Claude Code, global
proxy settings, `/etc/hosts`, or pfctl. Desktop behavior remains a proof
item: do not claim Desktop interception until daemon telemetry shows real
Codex Desktop traffic.

### 4. Global transparent lab mode

Do not do this unless you are deliberately testing the machine-wide
transparent MITM path. It routes `chatgpt.com` and `api.openai.com`
for the whole user session, including Browser ChatGPT and ChatGPT.app.

```bash
slimference cert-trust
slimference root-arm --global-chatgpt-hosts
slimference enable
```

What happens:

1. `cert-trust` opens Keychain Access on the local root cert. The
   user must set it to "Always Trust" for SSL.
2. `root-arm --global-chatgpt-hosts` writes the marker-fenced Codex-only IPv4 hosts block and
   installs the pfctl rdr anchor from port 443 to 127.0.0.1:8443. It
   does not write `api.anthropic.com` and does not install IPv6 `::1`
   mappings.
3. `enable` sets `transparent.sni_peek_mode = true` in the resolved
   config path. The canonical default is
   `~/.config/slimference/config.toml`.
4. `enable` sends `SIGHUP` to the running daemon (PID read from
   `~/.slimference/run/daemon.pid`).
5. The daemon's SIGHUP handler reads the new flag and starts or stops
   the SNI-peek listener.

If the daemon is not running, the flag is still written; the next
`slimference daemon start` (or boot via launchd) will apply hosts and
arm the listener.

### 5. Disarm global lab mode

```bash
slimference disable
```

Writes `transparent.sni_peek_mode = false` and SIGHUPs the daemon.
Use `slimference root-disarm` to remove the privileged hosts/pfctl
routing block when you want Codex to go direct again.

### 6. Uninstall

```bash
slimference uninstall
```

Reverses the install plan in LIFO order:

1. `hooks.codex` reverted
2. `notice.codex` removed if still marker-owned
3. `launchd` unloaded + plist removed
4. CA removed from Keychain when supported by the selected Keychain
   runner
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
    - step: ca.keychain
      apply: adds ~/.slimference/ca/root.crt as trusted SSL root in macOS Keychain
      reverse: removes the entry by SHA-1 fingerprint
      inspect: present | absent
      idempotent: true
      privilege: prompts for password
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
      privilege: requires root; driven by `slimference root-arm --global-chatgpt-hosts` via one macOS admin prompt

  commands:
    install:    install_plan.apply
    uninstall:  install_plan.reverse
    enable:     write_config_field(transparent.sni_peek_mode = true) + SIGHUP daemon
    disable:    write_config_field(transparent.sni_peek_mode = false) + SIGHUP daemon
    cert-trust: open Keychain Access on ~/.slimference/ca/root.crt for interactive trust
    root-arm:   advanced global hosts + pfctl activation for Codex hosts; requires --global-chatgpt-hosts
    root-disarm: privileged hosts + pfctl deactivation
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

The WSS transport block is under `/admin/state.wss`:

- `engine_active=true`: a WSS dispatcher is installed in the daemon. This can
  be the scoped Codex WSS bridge or the global SNI-peek dispatcher.
- `frames_forwarded>0 && frames_reencoded=0`: byte-equal bridge only.
- `frames_reencoded>0`: Phase F mutation happened on WSS frames.
- `degraded_sessions>0` or `parse_failures>0`: schema drift or malformed frames
  triggered fail-open byte bridging.

Current pre-live proof stack (2026-05-17):

- `go run ./scripts/ci` passes all 8 steps, including the formal
  `go run ./scripts/coverage -min=99.5` aggregate gate. Reported
  statement coverage is currently `99.8%` total. Package-level coverage
  lines can be below the aggregate threshold without failing the formal
  release check.
- Targeted race check passes:
  `go test ./internal/proxy ./cmd/slimference ./internal/codexroute -race -count=1 -timeout 240s`.
- Scoped raw WSS pre-live checks pass: raw Upgrade header order/casing is
  preserved on the existing `:8990` listener, non-Codex requests replay
  through the normal HTTP server, and the T224 parser can parse a
  synthetic WSS capture without tshark.
- Live Codex certification is still intentionally pending as T209. Do not
  run `cert-trust`, `root-arm --global-chatgpt-hosts`, or `enable` from
  the active Codex Desktop development session.

The transparent listener readiness bit is
`/admin/state.listener.bound_on_sni_peek` (default port 8443). Admin
port 8990 being up is not enough for live interception. If
`hosts_active=true` but `bound_on_sni_peek=false`, run
`slimference root-disarm` from the recovery shell before using Codex.

Before T209 live certification, run:

```bash
slimference status --preflight
```

Expected preflight: DoH resolves `chatgpt.com` and `api.openai.com` to
non-loopback upstream IPs, Codex CLI/Desktop app policy is enabled, Claude
Code remains inactive, and no `api.anthropic.com` hosts route is present in
Codex-only mode. This preflight does not start Codex and does not arm
Keychain, hosts, or pfctl.

T209 starts from disarmed preflight state: admin health can be up on
`127.0.0.1:8990`, but `:8443` should be off, hosts should be inactive,
and CA trust should still be untrusted unless a global lab test is
explicitly approved. The scoped CLI sequence is
`status --preflight` -> `codex run -- <prompt>` ->
`codex run --transport=wss -- <prompt>` -> `/admin/state` telemetry and
T224 capture check. The shared CLI/App proof sequence is
`codex enable --transport=wss` -> restart Codex.app/app-server -> prompt
-> telemetry check -> `codex disable`. The old global sequence is now lab-only:
`cert-trust` -> `root-arm --global-chatgpt-hosts` -> `enable` -> smoke
-> `disable` -> `root-disarm`.

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
  --no-keychain     skip the macOS Keychain trust step
  --system          install CA into the system Keychain (requires admin)
  --help, -h        show help

slimference uninstall [flags]
  --dry-run         show what would happen without changing anything
  --keep-ca         leave CA in Keychain (and on disk)
  --with-claude     compatibility no-op; Slimference does not own ~/.claude
  --system          uninstall from the system Keychain
  --help, -h        show help

slimference enable | disable [flags]
  --config=PATH     override config.toml location. CAUTION: must
                    match the path the daemon was started with
                    (default ~/.config/slimference/config.toml). If the
                    daemon was launched via `slimference --config=X
                    daemon start`, you MUST pass the same X here,
                    otherwise the daemon's SIGHUP reads the wrong
                    file. Use the default path unless you have a
                    specific reason.
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
