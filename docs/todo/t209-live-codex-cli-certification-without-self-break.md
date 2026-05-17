# TASK 209: Scoped Codex CLI live certification without self-break

Status: BLOCKED
Priority: P0 for release confidence
Scope: local macOS operator flow only; no code deletion

## Why

The system must be certified against real Codex CLI traffic without
routing Browser ChatGPT, ChatGPT.app, or Claude Code through Slimference.
Global `/etc/hosts` routing is machine-wide and is no longer acceptable
as the normal T209 path.

## Acceptance

- Run from a terminal that is not this active Codex session.
- `slimference status --preflight` reports daemon healthy, hosts inactive,
  `:8443=false`, Codex policy enabled, Claude policy disabled, DoH OK.
- A real Codex CLI prompt succeeds via:
  `slimference codex run -- <prompt>`.
- `/admin/state`, decision log, or `gain --proxy` shows the Codex CLI
  request through Slimference.
- Browser ChatGPT and ChatGPT.app remain direct: no `/etc/hosts` patch,
  no pfctl anchor, no `:8443` listener required, no Keychain trust needed.
- Claude Code remains untouched and `api.anthropic.com` is not routed.

## Sub-Tasks

- [ ] Prepare external non-Codex terminal.
- [ ] Verify disarmed preflight state.
- [ ] Execute one real scoped Codex CLI conversation with
  `slimference codex run`.
- [ ] Verify routed counters and logs.
- [ ] Verify hosts/pfctl stayed inactive.
- [ ] Verify direct-mode browser/ChatGPT.app unaffected.

## Verification

- Pending user-approved live arm window.
- Pre-live code/docs proof completed 2026-05-17:
  `go run ./scripts/ci` passes all 8 steps. The formal coverage gate is
  aggregate 99.5%; the current run reports 99.8% total. Some individual
  packages can print less than 99.5% while the aggregate gate still
  passes.

## Notes

This task is intentionally not executed during active Codex development.
It is a live-system certification task, not a unit-test substitute.

T208 is complete for the WSS/transparent code path, but T209 now uses
the scoped CLI provider path first through `slimference codex run`.
Do not use global
`cert-trust` / `root-arm --global-chatgpt-hosts` / `enable` unless the
user explicitly approves a separate global lab test.

Pre-run hardening completed 2026-05-17:

- `root-arm` now refuses by default and requires
  `--global-chatgpt-hosts` because it routes `chatgpt.com` and
  `api.openai.com` machine-wide.
- `slimference codex run` wraps the existing provider override and
  falls back to direct Codex if the Slimference daemon health check
  fails.
- `slimference codex enable|disable|status` manages the optional shared
  Codex CLI/App provider block in `~/.codex/config.toml`.
- Per-app policy path is canonical XDG config:
  `~/.config/slimference/apps.toml` (or next to `SLIMFERENCE_CONFIG`).
  Defaults: Codex CLI on, Codex Desktop on, Claude Code off.
- `/admin/state.listener.bound_on_sni_peek` reports the actual SNI
  listener (default 8443). Admin port 8990 no longer counts as
  transparent-MITM health.
- `slimference status` warns loudly when hosts routing is active but no
  SNI listener is reachable; recovery command is `slimference root-disarm`.
- `PhaseFDispatcher` re-routes after the HTTP Upgrade headers are visible,
  so live Codex WSS can move from SNI-only passthrough into
  `MITMConversation` based on path, User-Agent, and
  `Sec-WebSocket-Protocol`.
- This active Codex Desktop coding session, Browser ChatGPT, and
  ChatGPT.app are all inside the same global `chatgpt.com` blast radius
  if root-arm is used. T209 therefore avoids root-arm entirely.

Current expected start point before the live arm window:

- Daemon/admin health may be up on `127.0.0.1:8990`.
- Transparent SNI listener on `:8443` should stay off for scoped CLI.
- `/etc/hosts` should remain inactive for Slimference.
- Codex CLI and Codex Desktop policy should be enabled.
- Claude Code policy should remain disabled and no `api.anthropic.com`
  hosts entry should exist.
- CA material may exist on disk, but Keychain trust is not required for
  scoped CLI T209. Trust is only for the separate global lab path.
