# Slimference — install / uninstall

This is the **single source of truth** for installing and removing
Slimference. Both humans and agents can drive the flow from this
document.

## TL;DR

```bash
slimference install      # one-shot, atomic, reversible, Codex-only default
slimference cert-trust   # one interactive macOS trust click for the local CA
slimference root-arm     # privileged hosts + pfctl routing; do not run while testing in Codex
slimference enable       # arm daemon-side transparent mode
slimference disable      # disarm (restores /etc/hosts)
slimference uninstall    # full removal, restores backups
slimference status       # see what's currently armed
```

That's it. No environment variables. No `OPENAI_API_BASE`. No
`HTTPS_PROXY`. The transparent MITM layer is universal.

## 2-surface architecture

Slimference touches exactly TWO external surfaces:

1. **Hook callouts** in `~/.codex/hooks.json` plus
   `~/.codex/config.toml` `[features].hooks=true` (out-of-band
   subprocess calls — never over network).
2. **Transparent TLS-MITM** on port 443 via:
   - local CA in macOS Keychain (root cert)
   - `/etc/hosts` entries marker-fenced as `# slimference:start … #
     slimference:end`.

The user's Codex traffic to `chatgpt.com` and `api.openai.com` is
redirected to loopback, terminated with our
locally-signed leaf cert, inspected by `internal/proxy/transparent`,
forwarded to the real upstream, and routed through the existing Phase F
reducers when the frame schema is recognised. There is no
`openai_base_url`, no system HTTPS proxy, no environment magic.
**Everything is one on/off switch (`slimference enable` /
`slimference disable`).**

Claude Code is deliberately **not** part of the product install. Its
hook/parser code stays in tree for reference and possible future work,
but `slimference install` does not write `~/.claude`, does not install
Claude hooks, and `--with-claude` is accepted only as a parked no-op for
old scripts. Use RTK for Claude Code while Slimference focuses on Codex
CLI and Codex Desktop.

## Fail-open guarantees

| Event | What happens |
|---|---|
| Daemon process dies cleanly | daemon-side hosts patch is reverted on shutdown → Codex talks direct to chatgpt.com. **No breakage.** |
| `kill -9` daemon | launchd KeepAlive restarts within seconds; hosts is dirty for that window. On restart, daemon idempotently re-applies hosts. |
| Codex CLI/Desktop updates | Frame parser falls back to byte-equal bridge on schema drift. sniroute defaults unknown paths to `PassthroughTLS`. **No breakage.** |
| `slimference disable` while traffic in flight | Engine accepts current connections, reverts /etc/hosts. New connections go direct. |
| CA removed from Keychain externally | TLS handshake to chatgpt.com:443 (intercepted) fails until you re-`install` or fully `uninstall`. |
| `slimference enable` while daemon down | Config flag is written; the next `slimference daemon start` picks it up. Privileged routing still requires `root-arm` or an equivalent root setup. |

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

### 2. Arm transparent mode

Do not do this from an active Codex session unless you are deliberately
testing interception and have a recovery shell open.

```bash
slimference cert-trust
slimference root-arm
slimference enable
```

What happens:

1. `cert-trust` opens Keychain Access on the local root cert. The
   user must set it to "Always Trust" for SSL.
2. `root-arm` writes the marker-fenced Codex-only IPv4 hosts block and
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

### 3. Disarm

```bash
slimference disable
```

Writes `transparent.sni_peek_mode = false` and SIGHUPs the daemon.
Use `slimference root-disarm` to remove the privileged hosts/pfctl
routing block when you want Codex to go direct again.

### 4. Uninstall

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
      privilege: requires root; driven by `slimference root-arm` via one macOS admin prompt

  commands:
    install:    install_plan.apply
    uninstall:  install_plan.reverse
    enable:     write_config_field(transparent.sni_peek_mode = true) + SIGHUP daemon
    disable:    write_config_field(transparent.sni_peek_mode = false) + SIGHUP daemon
    cert-trust: open Keychain Access on ~/.slimference/ca/root.crt for interactive trust
    root-arm:   privileged hosts + pfctl activation for Codex hosts
    root-disarm: privileged hosts + pfctl deactivation
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

When transparent mode is armed, it intercepts Codex traffic to
`chatgpt.com` and `api.openai.com`
that originates on this machine. Setting `OPENAI_API_BASE` or
`HTTPS_PROXY` on top of that would be redundant at best and could
cause clients to dial a proxy on a port that doesn't exist (just
`/etc/hosts`, no listener). The transparent path is universal — and
that universality is what catches Codex 0.130's hardcoded WSS URL
too.

## Verification

```bash
slimference status              # human-readable table
slimference status --preflight  # adds DoH upstream checks without Codex traffic
slimference status --json | jq  # machine-readable
curl http://127.0.0.1:8990/_slimference/admin/state | jq
```

The WSS transport block is under `/admin/state.wss`:

- `engine_active=true`: the SNI-peek dispatcher is installed in the daemon.
- `frames_forwarded>0 && frames_reencoded=0`: byte-equal bridge only.
- `frames_reencoded>0`: Phase F mutation happened on WSS frames.
- `degraded_sessions>0` or `parse_failures>0`: schema drift or malformed frames
  triggered fail-open byte bridging.

Current pre-live proof stack (2026-05-17):

- `go run ./scripts/ci` passes all 8 steps, including the formal
  `go run ./scripts/coverage -min=100` gate. Reported statement coverage:
  `100.0%` total. This is an aggregate gate; package-level coverage
  lines can be below `100.0%` without failing the formal release check.
- Targeted race check passes:
  `go test ./internal/proxy ./internal/summarization ./internal/filter ./internal/transparent ./internal/control/apps ./internal/install/installsteps ./internal/tui -race -count=1 -timeout 300s`.
- Live Codex certification is still intentionally pending as T209. Do not
  run `cert-trust`, `root-arm`, or `enable` from the active Codex Desktop
  development session.

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
and CA trust should still require the explicit `cert-trust` step. The
live sequence is `cert-trust` -> `root-arm` -> `enable` -> Codex CLI
smoke -> `/admin/state` telemetry check -> `disable` -> `root-disarm`.

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
