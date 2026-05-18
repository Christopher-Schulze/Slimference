# TASK 228: Codex Desktop zero-friction launcher

Status: PARTIAL - base-URL launcher shipped as diagnostic; proxy launcher proof moved to T238
Priority: P1 after T238 proof branch decision
Scope: Codex Desktop App only; no Browser ChatGPT or ChatGPT.app routing

## Why

Codex Desktop must be as easy as the CLI path. The user should not repeatedly
kill processes, restart app-servers by hand, or wonder whether the Desktop app
picked up the scoped provider route.

T225/T228 proved the first two scoped Desktop ideas are not enough for current
Codex.app conversation routing: the shared provider block/base-URL override may
affect sideband state, and the launcher can inject env into the app-server, but
the conversation path still stays on hardcoded `chatgpt.com`.

This task now records the diagnostic launcher and the UX target. T238 owns the
next implementation branch: process-local proxy launch.

## Target State

Preferred path:

- `slimference enable` writes the scoped Codex route.
- Slimference detects whether Codex Desktop/app-server has reloaded the route.
- If Desktop is running with stale config, Slimference offers a one-command
  controlled restart that affects only Codex Desktop/app-server.
- The user can disable route from TUI or CLI and start Desktop direct again.

Current diagnostic launcher path:

- `slimference codex launch-desktop --probe` emits the process-local base-URL
  env overlay without spawning.
- `slimference codex launch-desktop` can spawn Codex Desktop with that overlay,
  but current Codex.app ignores it for conversation traffic.
- No `/etc/hosts`, no pfctl, no system proxy, no persistent global env vars.

Next launcher path:

- T238 adds proxy transport only if live proof shows Codex.app honors
  process-local proxy env for conversation WSS.

## Acceptance

- Desktop route proof distinguishes:
  - provider block honored directly;
  - provider block honored only by app-server child after restart;
  - provider block ignored;
  - launcher required;
  - no safe scoped Desktop path.
- Base-URL launcher is documented as diagnostic/future-proof, not as active
  Desktop conversation routing.
- If T238 proves proxy launch works, it is process-local, reversible, and
  visible in `status`.
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
- [x] Build diagnostic `codex launch-desktop` around process-local base-URL env.
- [x] Record that current Codex.app ignores base-URL env for conversation
  traffic despite env reaching the app-server.
- [ ] If T238 proxy launch works, build the launch-center Desktop action around
  that proven mechanism.
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
  version-aware status, direct fallback, no hidden global state, and live
  proof before any savings claim.
- If Desktop exposes no safe scoped path, do not fake it. Keep Desktop out of
  product claim until upstream gives a usable surface.
