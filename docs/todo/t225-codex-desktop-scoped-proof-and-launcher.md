# TASK 225: Codex Desktop scoped proof and launcher

Status: PREPARED PRE-LIVE
Priority: P1 after Codex CLI scoped WSS proof
Scope: Codex Desktop App only; Browser ChatGPT and ChatGPT.app stay direct

## Why

T220 prepares a shared `~/.codex/config.toml` provider route, but Codex Desktop
must be live-proven before any product claim. If Desktop ignores the provider
block or loads it only in a child app-server, a launcher may be required.

The goal is to route Codex Desktop conversation traffic through Slimference
without global hosts/pfctl and without affecting Browser ChatGPT or ChatGPT.app.
The preferred Desktop end-state is also WSS-first, because Desktop is likely to
benefit most from the native conversation stream shape. HTTP is acceptable only
as a fallback or if Desktop itself chooses it.

## Acceptance

- `slimference codex enable` is live-tested against Codex Desktop/App-server
  after a full app restart.
- If Desktop uses the provider route, telemetry proves exactly which endpoint
  and transport was captured.
- If Desktop supports scoped WSS through the provider route, WSS becomes the
  preferred Desktop transport after T224-style capture proof.
- If Desktop ignores the route, build or document a scoped launcher that affects
  only Codex Desktop/App-server.
- The launcher must not set global environment variables, macOS System Proxy, or
  `/etc/hosts`.
- The launcher must have a visible off switch and a direct-mode fallback.
- The launcher must support explicit `auto|wss|http|direct` transport choice if
  Desktop exposes enough control; otherwise it must report the limitation.
- If no scoped Desktop path works, docs say Desktop requires explicit global lab
  mode and is not a default product claim.

## Sub-Tasks

- [x] Identify the pre-live proof boundary: Desktop cannot be certified without
  restarting/observing the real app-server process during the live window.
- [ ] Identify the active Codex Desktop binary/app-server process and how it
  reads `~/.codex/config.toml`.
- [ ] Test `slimference codex enable` with full app restart and minimal prompt.
- [ ] Inspect admin/debug telemetry to determine HTTP vs WSS route.
- [ ] Prefer WSS if Desktop exposes both transports; keep HTTP as fallback.
- [ ] If config route fails, test process-local launcher candidates:
  app-specific env, Chromium/Electron proxy flags, app-server child process env,
  or wrapper script.
- [ ] Add `slimference codex app-run` only if a launcher is proven necessary and
  non-global.
- [ ] Ensure any launcher can revert to direct Desktop without deleting user
  config or touching global network state.
- [ ] Add status/preflight checks that warn when Desktop route is configured but
  not observed.
- [ ] Document final Desktop truth in `docs/install.md`.

## Notes

Benefit:

- Keeps the product promise honest: Codex Desktop only when actually proven.
- Avoids falling back to global `chatgpt.com` hijack just to make Desktop work.
- Aligns Desktop with the same WSS-first maximum-savings target as CLI.

Known limit:

- If Codex Desktop hardcodes ChatGPT WSS and ignores all scoped provider/launcher
  options, then no safe scoped Desktop path exists. In that case Desktop remains
  global-lab only until upstream exposes a scoped config surface.

Pre-live Desktop procedure:

1. Start from scoped disarmed state: daemon up on `127.0.0.1:8990`, no global
   hosts/pfctl, no Keychain trust requirement.
2. Run `slimference codex enable --transport=wss`.
3. Fully quit Codex Desktop and its app-server child, then restart the app.
4. Send one minimal Codex Desktop conversation prompt.
5. Check `/admin/state` and the debug flight recorder for
   `route_mode=websocket_raw_phasef` or `route_mode=websocket_phasef` on
   `/backend-api/codex/responses`.
6. If no telemetry appears, repeat once with `slimference codex enable` (HTTP)
   to determine whether Desktop honors the provider block at all.
7. Always finish with `slimference codex disable` unless the operator wants to
   leave shared Codex routing active.

Do not build a launcher until this proof says it is needed. The launcher path
is a fallback only; the preferred product is the marker-owned
`~/.codex/config.toml` provider route because it avoids Browser ChatGPT and
ChatGPT.app collateral without another surface.
