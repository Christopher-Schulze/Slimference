# T309 Patch-free scoped session indicators

Status: Done.

## Why

Users need a durable, visible way to tell whether the current Codex surface was
started through Slimference. The old in-composer Desktop provider chip is not a
stable current Codex Desktop contract, model metadata mutation is forbidden, and
the floating overlay experiment was rejected as bad UX. The replacement must be
patch-free, process-scoped, and must not affect normal Finder/Spotlight
Codex.app or normal shell `codex`.

## Acceptance

- Codex Desktop scoped app-server sessions start a patch-free macOS menu bar
  status item (`● SF`) for the lifetime of the hidden Slimference shim.
- Normal Codex.app launches remain direct and do not start the indicator.
- Codex CLI sessions launched through `slimference codex run` set a `[SF]`
  terminal-title prefix only while the proxied process is active.
- Direct CLI fallback and `--direct` mode do not set the title.
- No Codex renderer bundle patching, overlay helper, model-list mutation, model
  ID/display-name mutation, or service-tier metadata mutation is introduced.
- Documentation explains that the visual indicator proves scoped launch state
  only; savings proof remains status, shim flight logs, and daemon decisions.
- Tests cover indicator script shape, shim start/stop ownership, CLI title
  ownership, and removal of the old TUI shell-title hack.

## Sub-Tasks

- [x] Add the macOS menu bar status-item helper used only by the scoped Desktop
  app-server shim.
- [x] Wire the helper into `runCodexDesktopAppServerMediated` with fail-open
  no-op behavior when macOS/osascript is unavailable or explicitly disabled.
- [x] Move CLI title ownership from the TUI shell wrapper into
  `slimference codex run` itself.
- [x] Add regression tests for Desktop indicator script shape, shim lifetime,
  and CLI title behavior.
- [x] Update `docs/documentation.md`, `docs/install.md`, and the TODO index.
- [x] Run focused tests for the changed Desktop/CLI paths.

## Notes

- The status item is intentionally outside Codex Desktop's renderer. It is
  stable across Codex app bundle updates because it does not patch or inspect
  the frontend bundle.
- The status item is a route/session indicator, not a savings-proof badge.
  Savings still require the existing Desktop status/proof/log surfaces.
- The helper can be disabled with `SLIMFERENCE_CODEX_DESKTOP_MENUBAR=0` for
  operator debugging; failure to start it never blocks Codex.

## Deviations

None.
