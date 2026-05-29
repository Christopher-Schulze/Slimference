# TASK 225: Codex Desktop scoped proof and launcher

Status: SUPERSEDED - provider/base-URL routes insufficient; proxy branch moved
to T238/T242 and app-server shim branch moved to T246, which is live-blocked
for current Codex.app
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

2026-05-18 update: the shared provider/base-URL branch is no longer the main
Desktop bet. T228 proved process-local env injection reaches Codex.app's
app-server, but current Codex.app still keeps conversation traffic on hardcoded
`chatgpt.com` routes. T238 now owns the next credible branch: process-local
proxy launch using the Desktop binary's proxy/WSS capability surface.

2026-05-22 update: T238/T242 proved the process-local proxy/CA branch reaches
CONNECT but produces zero application bytes. T246 implemented the cleaner
`CODEX_CLI_PATH` app-server shim branch.

2026-05-29 update: T247 proved the app-server shim branch can produce real
Desktop WSS Phase-F mutation on current Codex.app/Codex 0.135.0. Current
product truth: Desktop Slimference savings are available through the scoped
Slimference launcher when the proof gate is green; normal Finder/Spotlight
Codex.app remains direct and no-drawback.

## Acceptance

- `slimference codex enable` is live-tested against Codex Desktop/App-server
  after a full app restart.
- If Desktop uses the provider route, telemetry proves exactly which endpoint
  and transport was captured.
- If Desktop supports scoped WSS through the provider route, WSS becomes the
  preferred Desktop transport after T224-style capture proof.
- If Desktop ignores the route, T238 must prove or reject a scoped process-local
  proxy launcher that affects only Codex Desktop/App-server.
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
- [x] If config route fails, test process-local base-URL launcher candidates:
  env reaches app-server, but current Codex.app conversation routing ignores
  the base-URL override.
- [ ] Hand off remaining Desktop launcher proof to T238 process-local proxy mode.
- [ ] Add a product Desktop launch action only if T238 proves it is non-global
  and catches conversation traffic.
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

Known limits:

- If Codex Desktop hardcodes ChatGPT WSS and ignores all scoped provider/launcher
  options, then no safe scoped Desktop path exists. In that case Desktop remains
  global-lab only until upstream exposes a scoped config surface.
- Current base-URL env injection is already proven insufficient for Codex.app
  0.131.0-alpha.9 conversation routing. Do not re-run that path as the primary
  proof unless the Desktop version changes.

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

Do not claim Desktop savings from provider/base-URL routing. The active next
proof is T238: a process-local proxy launcher that must prove both positive
routing and zero collateral.
