# TASK 203: `docs/install.md` — single README, human + agent readable

Status: PLANNED 2026-05-16
Parent: T200 (Phase H epic)
Scope: `docs/install.md` (new); update entry-point references from
       README.md and `agents.md`

## Why

User requirement: **one document, an Agent can read it and execute the
install end-to-end**.

Today install knowledge is scattered across:
- `docs/transparent-mode.md` (developer-facing)
- README.md (high-level)
- inline comments in `internal/hooks/codex.go`
- TUI Setup-Wizard help-text
- `slimference help install` (per-subcommand text)

Phase H needs a **single source of truth** with two reading modes:
human prose + machine-readable spec block.

## Target structure

```markdown
# Slimference install / uninstall

## TL;DR (humans)

```bash
slimference install      # one-shot, atomic, reversible
slimference enable       # arm transparent MITM
slimference disable      # disarm (Codex back to direct)
slimference uninstall    # full removal, restores backups
slimference status       # see what's currently armed
```

## What it touches

Slimference's install path touches exactly TWO classes of external
surface:

1. **Hook callouts** in `~/.codex/config.toml` and (optionally)
   `~/.claude.json`. Out-of-band subprocess calls — never over network.
2. **Transparent TLS-MITM** on port 443 via:
   - local CA in macOS Keychain (root cert)
   - `/etc/hosts` entries marker-fenced as `# slimference:start ... #
     slimference:end`

That's it. No `OPENAI_API_BASE` env. No `HTTPS_PROXY`. No
`openai_base_url` field in Codex config. Slimference does not need any
of those when the transparent layer is armed.

## Fail-open guarantees

| Event | What happens |
|---|---|
| Daemon process dies | hosts patch is reverted on shutdown → Codex talks direct to chatgpt.com. No breakage. |
| `kill -9` daemon | launchd KeepAlive restarts within seconds; hosts is dirty for that window. On restart, daemon idempotently re-applies. |
| Codex CLI/Desktop updates | Frame parser falls back to byte-equal bridge on schema drift. Decision router defaults unknown paths to PassthroughTLS. No breakage. |
| `slimference disable` while traffic in flight | Engine accepts current connections, applies hosts revert. New connections go direct. |
| CA removed from Keychain | TLS handshake against chatgpt.com:443 (intercepted) fails until you `slimference uninstall` or re-`install`. |

## Step-by-step (humans)

1. **Install**: `slimference install`
   - Generates a local CA under `~/.slimference/ca/`
   - Adds the root cert to your macOS Keychain (prompts for password)
   - Installs `~/Library/LaunchAgents/com.slimference.proxy.plist` and
     loads it
   - Patches `~/.codex/config.toml` `[hooks]` (idempotent, marker-fenced)
   - Does NOT touch `/etc/hosts` yet
2. **Arm transparent mode**: `slimference enable`
   - Sets `cfg.Transparent.SNIPeekMode = true` in config.toml
   - Sends SIGHUP to the running daemon
   - Daemon patches `/etc/hosts` and starts the SNI-peek listener
3. **Disarm**: `slimference disable`
4. **Uninstall**: `slimference uninstall`

## Agent-readable spec

For LLMs / scripts that automate the install:

```yaml
schema_version: 1
slimference_install:
  # The install plan is a sequence of named Steps. Each Step has
  # Apply / Reverse / Inspect semantics. All are idempotent.
  install_plan:
    - step: ca.generate
      apply: writes CA cert + key under ~/.slimference/ca/
      reverse: moves files aside to ~/.slimference/ca.bak.<unix>/
      inspect: present | absent | rotated
    - step: ca.keychain
      apply: adds ~/.slimference/ca/root.crt as trusted root in macOS Keychain
      reverse: removes the entry by fingerprint
      inspect: present | absent
    - step: launchd.install
      apply: writes ~/Library/LaunchAgents/com.slimference.proxy.plist; loads it
      reverse: unloads + removes plist
      inspect: present | absent
    - step: hooks.codex
      apply: marker-fenced patch in ~/.codex/config.toml [hooks]
      reverse: removes patch block byte-equal to pre-state
      inspect: present | absent | other (file modified outside our fence)
    - step: hooks.claude
      apply: marker-fenced patch in ~/.claude.json hooks section
      reverse: removes patch block
      inspect: present | absent | other

  # The hosts plan is separate from install — it runs at daemon
  # lifecycle boundaries, not at install time.
  hosts_plan:
    - step: hosts.patch
      apply: patches /etc/hosts with marker fence; redirects
             chatgpt.com, api.openai.com, api.anthropic.com → 127.0.0.1
      reverse: removes marker-fenced block
      inspect: present | absent

  # Commands map to plan operations.
  commands:
    install:    install_plan.apply
    uninstall:  install_plan.reverse
    enable:     write_config_field(transparent.sni_peek_mode = true) + SIGHUP daemon
    disable:    write_config_field(transparent.sni_peek_mode = false) + SIGHUP daemon
    status:     emit /admin/state JSON

  # Exit codes.
  exit_codes:
    0: success
    1: invalid argument / config error
    2: privilege required (e.g. /etc/hosts write needs root for once-only)
    3: rollback required (Apply partially failed)
    4: prerequisite missing (e.g. enable without install)

  # File paths owned by Slimference.
  owned_paths:
    - ~/.slimference/         # state dir
    - ~/.slimference/ca/      # CA material
    - ~/.slimference/backups/ # snapshot dir
    - ~/.slimference/run/     # PID file, sockets
    - ~/Library/LaunchAgents/com.slimference.proxy.plist
  # Marker-fenced edits in shared files (reversible).
  fenced_edits:
    - file: /etc/hosts
      marker_start: "# slimference:start"
      marker_end:   "# slimference:end"
    - file: ~/.codex/config.toml
      marker_start: "# slimference:hooks:start"
      marker_end:   "# slimference:hooks:end"
    - file: ~/.claude.json
      marker_field: "_slimference_hooks"

  # NO automatic edits to these (advanced users may set them manually,
  # but slimference install does not touch them):
  not_touched:
    - OPENAI_API_BASE, OPENAI_BASE_URL, CHATGPT_BASE_URL env vars
    - HTTPS_PROXY, HTTP_PROXY env vars
    - openai_base_url in ~/.codex/config.toml
    - macOS system network proxy settings
```

## Verification (after install)

```bash
slimference status              # human table
slimference status --json | jq  # machine-readable
curl http://127.0.0.1:8990/_slimference/admin/state | jq
```

## Recovery (if something went wrong)

```bash
# Most common: rollback a partial install
slimference uninstall

# Nuclear option: if uninstall itself fails, restore from backups
ls ~/.slimference/backups/
sudo cp ~/.slimference/backups/hosts.bak.<latest> /etc/hosts
cp ~/.slimference/backups/codex_config.bak.<latest> ~/.codex/config.toml
# CA: rotate aside via:
mv ~/.slimference/ca ~/.slimference/ca.attic.$(date +%s)
# Keychain: open Keychain Access, search "Slimference", delete.
```

## Why we don't touch env vars or HTTPS_PROXY

When the transparent layer is armed (`slimference enable`), it
intercepts ALL traffic to chatgpt.com / api.openai.com / api.anthropic.
com that originates on this machine. Setting `OPENAI_API_BASE` or
`HTTPS_PROXY` on top of that would be redundant at best and could
cause clients to dial the proxy on a port that doesn't exist (no
listener, just hosts). The transparent path is universal — and that
universality is what catches Codex 0.130's hardcoded WSS URL too.

## See also

- `docs/transparent-mode.md` — internals, schema details, debugging
- `docs/todo/t200-phase-h-single-entry-point-epic.md` — design history
```

## Acceptance

- `docs/install.md` exists and is non-empty.
- Following the human TL;DR commands produces a working install
  (verified by `slimference status` showing GREEN on CA, Daemon,
  Listener, Hosts).
- An agent fed the YAML spec block can produce a series of subprocess
  calls equivalent to `slimference install + enable`. Validated by a
  meta-test (T203 sub-task: a small Go test that parses the YAML and
  asserts every named Step exists in `internal/install.Plan()`).
- `README.md` and `agents.md` link to `docs/install.md` as the
  install source of truth; their inline install snippets get removed.

## Sub-Tasks

- [ ] Write `docs/install.md` per the structure above.
- [ ] Update root `README.md`: remove install snippets, point to
      `docs/install.md`.
- [ ] Update `agents.md` §1 "Normative Dokumente" to list `docs/install.
      md` as the install SSOT.
- [ ] Add a Go test that parses the YAML block out of `docs/install.md`
      (regex extraction) and asserts each named Step is present in
      `internal/install.Plan()`.

## Deviations

(none yet)
