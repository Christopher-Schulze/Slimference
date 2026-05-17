# TASK 209: Live Codex CLI certification without self-break

Status: BLOCKED
Priority: P0 for release confidence
Scope: local macOS operator flow only; no code deletion

## Why

The system must be certified against real Codex CLI traffic, but arming transparent MITM while the active coding session runs inside Codex can break the current session if CA trust, hosts, pfctl, or daemon state is wrong. The user explicitly said not to make Codex sharp while coding in Codex.

## Acceptance

- Run from a recovery shell outside the active Codex session.
- `slimference cert-trust` completed interactively and `status` reports CA trusted.
- `slimference root-arm` applied Codex-only hosts and pfctl.
- `slimference enable` turns on SNI-peek mode and daemon listener.
- A real Codex CLI prompt succeeds through Slimference.
- `/admin/state` shows Codex app detected, routed counter incremented, and no Claude routing.
- `slimference disable` plus `slimference root-disarm` returns Codex to direct mode.
- Re-running status confirms hosts inactive and daemon still healthy.

## Sub-Tasks

- [ ] Prepare external recovery terminal.
- [ ] Trust CA via Keychain GUI.
- [ ] Run `root-arm` and verify hosts/pfctl.
- [ ] Run `enable` and verify listener/state.
- [ ] Execute one real Codex CLI conversation.
- [ ] Verify routed counters and logs.
- [ ] Run `disable` and `root-disarm`.
- [ ] Verify fail-open direct mode.

## Verification

- Pending user-approved live arm window.
- Pre-live code/docs proof completed 2026-05-17:
  `go run ./scripts/ci` passes all 8 steps, formal coverage reports
  `100.0%`, and targeted race passes for the touched runtime packages.

## Notes

This task is intentionally not executed during active Codex development. It is a live-system certification task, not a unit-test substitute.

T208 is complete, so the code side is ready for certification: Codex
WSS frames can route, mutate through Phase F, and report transport
state under `/admin/state.wss`. Do not start this task until the user
has an external recovery shell open and explicitly approves arming
`cert-trust` / `root-arm` / `enable`.

Pre-run hardening completed 2026-05-17:

- `root-arm` is Codex-only and IPv4-only: `chatgpt.com` +
  `api.openai.com` map to `127.0.0.1`; `api.anthropic.com` and
  `::1` are deliberately not written.
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
- This active Codex Desktop coding session is inside the same systemwide
  `chatgpt.com` routing blast radius. T209 must be run from a separate
  recovery terminal with a known-good direct-mode fallback.
