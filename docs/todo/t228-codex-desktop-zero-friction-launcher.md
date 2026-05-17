# TASK 228: Codex Desktop zero-friction launcher

Status: PLANNED
Priority: P1 after T225 proof branch decision
Scope: Codex Desktop App only; no Browser ChatGPT or ChatGPT.app routing

## Why

Codex Desktop must be as easy as the CLI path. The user should not repeatedly
kill processes, restart app-servers by hand, or wonder whether the Desktop app
picked up the scoped provider route.

T225 proves whether the shared `~/.codex/config.toml` provider block is enough.
This task is the implementation plan for the fallback if Desktop needs a
launcher, plus the polish path even if the provider block works.

## Target State

Preferred path:

- `slimference enable` writes the scoped Codex route.
- Slimference detects whether Codex Desktop/app-server has reloaded the route.
- If Desktop is running with stale config, Slimference offers a one-command
  controlled restart that affects only Codex Desktop/app-server.
- The user can disable route from TUI or CLI and start Desktop direct again.

Launcher fallback path:

- `slimference codex app-run` or equivalent opens Codex Desktop with a
  process-local environment/config overlay.
- No `/etc/hosts`, no pfctl, no system proxy, no persistent global env vars.
- The launcher supports `auto|wss|http|direct`.
- The launcher can close/restart only the child it owns or explain when manual
  restart is required.

## Acceptance

- Desktop route proof distinguishes:
  - provider block honored directly;
  - provider block honored only by app-server child after restart;
  - provider block ignored;
  - launcher required;
  - no safe scoped Desktop path.
- If Desktop uses the provider block, no launcher is shipped.
- If launcher is needed, it is process-local, reversible, and visible in
  `status`.
- User never has to remember repeated manual restarts for normal usage.
- TUI shows Desktop observation state:
  `unknown`, `not_running`, `direct`, `scoped_http`, `scoped_wss`,
  `stale_config`, `launcher_owned`.
- A stale Desktop route produces a precise action:
  `restart Desktop`, `switch to HTTP`, `disable`, or `run launcher`.
- Tests cover config writing, route observation parsing, stale state wording,
  launcher command rendering, and disable safety.

## Sub-Tasks

- [ ] During T225, capture process tree: Codex Desktop app, app-server child,
  command line, env inheritance, config reload behavior.
- [ ] Add Desktop observation probe:
  process present, app-server present, last observed route mode, last observed
  user-agent/client marker, last telemetry timestamp.
- [ ] If config reload works, add an app restart helper that is opt-in and
  Codex Desktop-specific.
- [ ] If config reload does not work, build `codex app-run` around the smallest
  process-local mechanism proven live.
- [ ] Add launcher direct mode for safe recovery.
- [ ] Add TUI action: `restart Codex Desktop route` only when stale state is
  detected.
- [ ] Add docs that Desktop route is shared with CLI unless launcher isolation
  is proven and implemented.
- [ ] Add integration tests with fake app-server processes; do not require the
  real GUI in CI.

## Benefits

- Makes Codex Desktop product-grade instead of a manual lab exercise.
- Keeps Browser ChatGPT and ChatGPT.app untouched.
- Preserves WSS-first goal for Desktop if the app honors the provider route.

## Drawdowns and Guards

- Restarting GUI apps can steal focus. Guard: do not auto-restart without an
  explicit user action.
- Process-local launchers can become fragile across Codex releases. Guard:
  version-aware status, direct fallback, and no hidden global state.
- If Desktop exposes no safe scoped path, do not fake it. Keep Desktop out of
  product claim until upstream gives a usable surface.

