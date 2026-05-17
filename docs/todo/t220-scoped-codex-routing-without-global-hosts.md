# TASK 220: Scoped Codex routing without global ChatGPT hosts

Status: IMPLEMENTED except live Codex Desktop proof
Priority: P0 product safety
Scope: Codex CLI and Codex Desktop only; Browser ChatGPT, ChatGPT.app, and Claude Code must remain direct

## Why

`/etc/hosts` is host-scoped, not app-scoped. Routing `chatgpt.com` to
loopback therefore catches Browser ChatGPT and ChatGPT.app too. That is
not acceptable for the current product goal: Slimference should optimize
Codex CLI first and Codex Desktop later, while the user's regular ChatGPT
surfaces behave normally.

macOS pf can match socket owner user/group, not bundle id or process name.
Because Codex, ChatGPT.app, and browsers run as the same user, pf cannot
make a clean "Codex only" rule. The global transparent MITM path remains
useful for lab certification, but it must be explicit and never the
normal default.

## Acceptance

- Bare `slimference root-arm` refuses and explains the machine-wide
  `chatgpt.com` blast radius.
- Global lab routing requires `slimference root-arm --global-chatgpt-hosts`.
- Normal CLI proof uses `slimference codex run -- <prompt>`.
- Shared Codex route uses `slimference codex enable|disable|status` and
  writes only a marker-owned `slimference-codex` provider block in
  `~/.codex/config.toml`.
- Scoped CLI proof leaves `/etc/hosts`, pfctl, `:8443`, Browser ChatGPT,
  ChatGPT.app, and Claude Code untouched.
- Codex Desktop scoped routing is not claimed until a live test proves a
  non-global launcher/config path that catches Desktop conversation
  traffic without affecting Browser ChatGPT or ChatGPT.app.
- Docs and help distinguish "scoped CLI" from "global lab" everywhere a
  human or agent would start T209.

## Sub-Tasks

- [x] Verify current live state is disarmed: hosts inactive, pf anchor
  empty, `:8443=false`, daemon healthy on `:8990`.
- [x] Verify global root-arm cannot be app-scoped by `/etc/hosts`.
- [x] Verify pf process-name isolation is not available for same-user
  Codex vs ChatGPT.app/browser traffic.
- [x] Promote scoped CLI path in docs/help:
  `slimference codex run -- <prompt>`.
- [x] Guard `root-arm` behind `--global-chatgpt-hosts`.
- [x] Add scoped Codex route manager:
  `internal/codexroute` writes/removes the marker-owned provider block.
- [x] Add CLI UX:
  `slimference codex run|enable|disable|status`.
- [x] Add direct fail-open for `slimference codex run` when daemon health
  check fails.
- [x] Add TUI Setup `[r]` toggle for the scoped Codex route so persistent
  `codex enable` can be disabled without touching global hosts/pf.
- [ ] Run T209 scoped real Codex CLI smoke from a non-Codex terminal.
- [ ] Prove or reject shared scoped Codex Desktop route:
  `slimference codex enable` -> restart Codex.app/app-server -> prompt
  -> telemetry -> `slimference codex disable`.
- [ ] If Desktop ignores the provider block, test launcher candidates
  without global hosts/pf: app-specific proxy launch flags or inherited
  env proxy for app-server.
- [ ] If Desktop scoped proof fails, keep Desktop behind explicit global
  lab mode and document that limitation plainly.

## Notes

Current safe live state before any T209/T220 live smoke:

- Daemon healthy on `127.0.0.1:8990`.
- No SNI listener on `:8443`.
- Slimference hosts block is inert/commented.
- pf anchor has no Slimference rdr rule.
- Codex CLI/Desktop policy is enabled, Claude Code policy is disabled.
- CA material may exist on disk, but Keychain trust is not required for
  scoped CLI.

Candidate Desktop scoped approaches must be verified against real
Codex.app traffic before any product claim:

- Electron/Chromium `--proxy-server` can affect renderer network traffic,
  but may not affect the bundled Codex app-server process.
- `HTTPS_PROXY`/`ALL_PROXY` inherited by a launcher can affect child
  processes, but only counts if it is process-local and leaves all other
  apps untouched.
- Persistent `~/.codex/config.toml` mutation is acceptable only through
  explicit `slimference codex enable`; it is marker-owned, backed up,
  visible in `slimference codex status`, and reversed by
  `slimference codex disable`.
- Coverage policy changed during this task from a 100.0% aggregate gate
  to a 99.5% aggregate gate. The scoped route has behavior tests for
  enable/disable/status, missing config, conflicts, legacy keys, daemon
  fail-open, backup failures, and route rendering; remaining uncovered
  lines are OS-dependent atomic-write cleanup branches.
- A separate macOS user/group would make pf owner matching possible, but
  it is operationally heavy and not a good default unless every lighter
  path fails.

## Deviations

This task intentionally reclassifies the previous Phase H global
transparent path as lab-only. The reason is user-visible product safety:
byte-equal passthrough is not enough if Browser ChatGPT and ChatGPT.app
are still forced through Slimference's TLS bridge.
