# TASK 200: Phase H — Single Entry Point + 2-Surface Consolidation (EPIC)

Status: DONE 2026-05-17 (Codex-only default; live arm certification split to T209)
Priority: P0 — blocks user-facing usability + reliability story
Scope: cross-cutting; the epic that ties install/uninstall/enable/disable
       into ONE atomic, reversible, fail-open surface

## Why

Today's legacy wiring to Codex CLI / Codex Desktop / Claude Code touched **four**
external surfaces:

1. **Hooks** — out-of-band subprocess signal (PreCompact/PostCompact)
2. **`openai_base_url` / env** — Codex/OpenAI client URL redirect
3. **System `HTTPS_PROXY`** — CONNECT proxy for HTTP traffic
4. **Transparent SNI-MITM** — `/etc/hosts` + CA + port 443 (only path
   that catches Codex 0.130's hardcoded WSS conversation URL)

This is too many touchpoints. The user wants:

- **One install command, one uninstall command, one README**
- **Daemon down → Codex works normally** (no breakage)
- **Codex update → fall back to byte-equal passthrough**, not breakage
- **Atomic, reversible** — Plan.Apply + Plan.Reverse via existing
  `internal/control/reversibility/`

Analysis: 4 surfaces → 2 surfaces, zero technical drawback:

- Hooks STAY (signal-in, out-of-band, irreducible).
- Transparent SNI-MITM STAYS (universal, only path catching 0.130 WSS).
- `openai_base_url` / env REMOVED from the install path (transparent
  MITM subsumes its function). Code lives on as an advanced/legacy
  knob.
- `HTTPS_PROXY` REMOVED from the install path (same reason). Code lives
  on as opt-in fallback.

2 → 1 reduction (drop hooks) was considered and rejected: it would
force schema-dependent stream parsing for PreCompact/PostCompact
detection, which is fragile across Codex updates.

## Target architecture

```
            Codex CLI / Desktop
                  │              │
                  │ subprocess   │ network traffic
                  │ (hook signal)│ (HTTP + WSS)
                  ▼              ▼
              Hooks         /etc/hosts → 127.0.0.1:443
            (PreCompact)    transparent.Engine (TLS-MITM)
                  │              │
                  └──────┬───────┘
                         ▼
                  slimference daemon
                  (apps.Manager + Phase F)
```

## Operative surface (final)

```
slimference install      # Plan.Apply: CA + launchd + Codex hooks (no hosts)
slimference cert-trust   # interactive macOS CA trust helper
slimference root-arm     # privileged Codex hosts + pfctl helper
slimference enable       # SNIPeekMode on, daemon reload
slimference disable      # SNIPeekMode off, daemon reload
slimference root-disarm  # remove privileged Codex hosts + pfctl helper
slimference uninstall    # Plan.Reverse: hosts (if present) + launchd + hooks + CA-rotated-aside
slimference status       # pretty-print SetupState

# Diagnostics
slimference doctor       # existing - checks config + hooks (unchanged)
```

## Acceptance for Phase H

- `slimference install` exits 0 and SetupState shows: CA installed,
  launchd loaded, hooks present, hosts CLEAN (not yet patched).
- `slimference enable` exits 0 and the daemon is intercepting:
  `/admin/state.network_redirect.hosts_active == true`, transparent.
  Engine counters tick on next Codex conversation.
- `slimference disable` exits 0 and `/etc/hosts` is byte-equal to its
  pre-enable backup. Codex talks direct to chatgpt.com again.
- `slimference uninstall` exits 0 and every touched file is restored:
  `/etc/hosts` clean, `~/.codex/config.toml` clean, launchd plist gone,
  CA rotated aside (not deleted, for forensic).
- **Daemon-down scenario**: kill the daemon process, run Codex — works
  normally (hosts patch is reverted as part of shutdown). Restart
  daemon → hosts re-armed automatically.
- **Codex-update scenario**: simulate by injecting an unknown WS frame
  schema; Session degrades to byte-equal, conversation completes
  without error.
- README at `docs/install.md` documents both the human and
  agent-readable install flows. An agent can read the YAML spec block
  and execute the install end-to-end without other documentation.

## Sub-tasks

1. **T201** — Install/Uninstall/Enable/Disable CLI subcommands +
   shared install plan. Detail: `docs/todo/t201-install-uninstall-cli.md`
2. **T202** — Daemon-lifecycle hosts patching (apply on start, revert
   on shutdown). Detail: `docs/todo/t202-daemon-hosts-lifecycle.md`
3. **T203** — `docs/install.md` README with agent-readable YAML spec.
   Detail: `docs/todo/t203-install-readme-agent-spec.md`
4. **T204** — Default-config consolidation: remove URL-redirect and
   HTTPS_PROXY auto-config from `slimference install` defaults; mark
   them as advanced. Detail: `docs/todo/t204-default-2-surface-consolidation.md`

## Sequencing

1. **T201 first** — the CLI subcommand IS the entry point. Without it,
   the rest of Phase H has nothing to expose.
2. **T202 next** — once `enable` exists, the daemon lifecycle must
   honour it on every start/stop. Otherwise crashes leave dirty state.
3. **T203 alongside T201+T202** — README is written as the commands
   solidify, not afterwards. Truth-table at the end of T202.
4. **T204 last** — only after T201/T202 are solid, remove the legacy
   defaults so we don't break existing users mid-flight.

## Current reality 2026-05-17

Phase H's default operational surface is Codex-only:

- `slimference install` installs CA, launchd, and Codex hooks only.
- Claude hooks remain in code but require `--with-claude`.
- `slimference enable` writes the canonical XDG config path by default.
- `slimference root-arm` handles privileged Codex-only hosts + pfctl.
- `slimference cert-trust` handles the unavoidable macOS Keychain GUI
  step.
- TUI visible controls call the Phase H lifecycle commands.

Live arming is intentionally not performed while coding from Codex;
that operator certification is tracked as T209.

## After Phase H

- **T199 Phase C2** (Frame-Adapter) — runs Phase F mutations through
  the new wsmitm.Session bridge.
- **T197 TUI** (revised scope) — TUI exposes the 2-button model:
  Install/Uninstall + Enable/Disable toggle. Setup-wizard retired.
- **T198 tshark probe** — operator audit tool, unchanged.

## Notes

- `reversibility.Plan` is already proven (T196). Phase H is the
  consumer wiring, not new plan engine work.
- Existing `serviceControlAdapter.InstallTransparent` (cmd/slimference/
  main.go:3631) is the pre-Plan path. It gets retired in T201 -
  replaced by a Plan-backed implementation that the CLI AND the TUI
  both call.
- `internal/hooks/codex.go` (Codex hook install path) already handles
  the `~/.codex/config.toml` patch idempotently via marker fence. It
  becomes one Step in the install Plan, not a separate subcommand
  surface.

## Deviations

(none yet)
