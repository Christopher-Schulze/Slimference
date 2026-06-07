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

- Codex Desktop scoped app-server sessions do not start an external indicator
  on current Desktop builds; route visibility lives in TUI Activity/Status and
  the scoped shim flight log.
- Normal Codex.app launches remain direct and do not start the indicator.
- Codex CLI sessions launched through `slimference codex run` set a `[SF]`
  terminal-title prefix only while the proxied process is active.
- Direct CLI fallback and `--direct` mode do not set the title.
- No Codex renderer bundle patching, overlay helper, model-list mutation, model
  ID/display-name mutation, or service-tier metadata mutation is introduced.
- Documentation explains that Desktop has no external indicator in current
  builds; savings proof remains status, shim flight logs, and daemon decisions.
- Tests cover CLI title ownership and removal of the old TUI shell-title hack.

## Sub-Tasks

- [x] Retract the macOS menu bar status-item helper from the product path.
- [x] Keep the Desktop app-server shim focused on scoped routing only.
- [x] Move CLI title ownership from the TUI shell wrapper into
  `slimference codex run` itself.
- [x] Add regression tests for Desktop indicator script shape, shim lifetime,
  and CLI title behavior.
- [x] Update `docs/documentation.md`, `docs/install.md`, and the TODO index.
- [x] Run focused tests for the changed Desktop/CLI paths.

## Notes

- Desktop is intentionally left without an external indicator on current
  builds. Savings still require the existing Desktop status/proof/log surfaces.
- T312 removed the status-item helper after product review rejected it as the
  wrong UX surface.

## Deviations

None.
