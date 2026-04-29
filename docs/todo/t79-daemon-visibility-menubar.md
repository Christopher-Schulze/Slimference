# TASK 79: Daemon visibility surface

Status: todo
Priority: P1
Scope: `cmd/slimference/`, `internal/tui/`, optional new `cmd/slimference-menubar/`
Driver: When the daemon runs under launchd, the operator has no live signal that compression is working. Stats live in `slimference stats today` or the TUI, but nobody opens the TUI when the daemon is running detached. Result: compression is invisible until it breaks.

---

## Problem

Today the only live status surfaces are `/admin/status` (machine-readable, opaque) and the TUI (requires a foreground process). Once `slimference service install` puts the daemon under launchd, the operator sees nothing. This produces a "did I install it correctly?" anxiety loop and hides degradation when it happens.

## Target State

Two complementary surfaces:

1. **`slimference watch`**: a headless, terminal-friendly live-ticker (mirrors a top-style refresh of savings and provider state). One-liner you can drop in any terminal pane.
2. **macOS menubar app**: optional standalone binary (`slimference-menubar`) that shows the daemon up/down, today's saved tokens / EUR, current compression ratio, prompt-cache state, and any active alarm (T83 degradation, T77 quality alarm).

The menubar app stays optional and in a separate binary to keep the core daemon footprint unchanged.

## Implementation Plan

### WP1 - `slimference watch`
- New subcommand in `cmd/slimference`.
- Polls `/admin/status` at `--interval` (default 2s).
- Renders compact one-screen view: savings, provider state, alarms.
- Quits on Ctrl-C.

### WP2 - Menubar binary
- New `cmd/slimference-menubar/` Go binary using a macOS menubar library that does not require Cgo if possible (fallback: Cgo with NSStatusBar via existing Go bindings).
- Pulls from `/admin/status` every N seconds.
- Click menu: "Open TUI", "Toggle Bypass", "Open Logs", "Quit".

### WP3 - Build + release
- Add `slimference-menubar` to release script (`scripts/release/main.go`) as an optional artifact.
- Document install path in `docs/integration.md`.

### WP4 - Tests
- Unit tests for the `watch` formatter against a mocked admin response.
- Snapshot tests for the menubar status string formatter (logic only; native UI outside test scope).

## Acceptance Criteria

- [ ] `slimference watch` runs and refreshes against a live or mocked daemon.
- [ ] Menubar binary builds on darwin/arm64 and renders savings + bypass state from `/admin/status`.
- [ ] Click "Toggle Bypass" in menubar successfully toggles via the admin endpoint.
- [ ] Release pipeline produces both binaries.
- [ ] Coverage 100% on the formatter logic.

## Out of Scope

- Linux tray support.
- Real-time SSE push (polling is sufficient).
- Notifications / banners (would belong in T83 visibility instead).

## Validation

```
slimference watch --interval=1s
./build/slimference-menubar --self-test
go run ./scripts/ci
```
