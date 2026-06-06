# T306 Desktop chip drift and service-control split

Status: Done.

## Why

After a Codex Desktop update, Slimference-launched Codex.app sessions still used
the scoped app-server route, but the visible provider chip could disappear from
the start screen. The route proof alone was not enough for mass-market UX: users
need a visible process-local signal that a launched Desktop window is running
through Slimference.

`cmd/slimference/main.go` also remained the largest production file. The TUI
service-control adapter was a cohesive block inside the CLI entrypoint, so it
was a low-risk monolith split candidate: same package, same symbols, no runtime
semantics changed.

## Acceptance

- Scoped Desktop config-response augmentation sets both snake_case and camelCase
  provider fields so older Codex Desktop UI shapes can render the `Slimference`
  provider signal.
- Scoped Desktop `model/list` responses get a visible `Slimference ` prefix on
  `displayName` only, so current Codex Desktop builds still expose the route
  signal when the separate provider chip is no longer rendered.
- The provider entry includes stable display/name/base-url/auth/websocket/wire
  fields in snake_case and camelCase forms.
- Model IDs, selected model values, explicit providers, and routing semantics
  remain unchanged; the display-only prefix cannot change model selection.
- A minimal process-local flight log records Desktop shim rewrite events under
  `~/.slimference/logs/desktop-shim.jsonl` without payloads or secrets.
- Direct Finder/Spotlight/normal terminal Codex starts stay direct; only the
  Slimference-launched app-server shim mutates scoped JSON-RPC frames.
- `serviceControlAdapter`, its TUI launch helpers, hook install/remove methods,
  route/status helpers, and transparent proxy adapter bridge live outside
  `main.go`.
- No duplicate `serviceControlAdapter`, `proxyCommandEnv`, or
  `tuiLaunchDirectory` implementations remain.
- Focused Go tests, full CI, latest binary install, and scoped Desktop launch
  verification pass.

## Sub-Tasks

- [x] Re-check current largest Go files and identify safe monolith split
  boundary.
- [x] Inspect current Codex.app bundle enough to confirm Desktop uses
  `modelProvider`/camelCase signals around thread start and model list
  `displayName` for the picker/start-screen model label.
- [x] Harden scoped Desktop config-response badge injection against
  snake_case/camelCase drift.
- [x] Harden scoped Desktop model-list display injection for current Codex
  Desktop UI without mutating model IDs.
- [x] Move TUI service-control adapter out of `cmd/slimference/main.go` into
  `cmd/slimference/tui_service_control.go`.
- [x] Update shim tests for both provider key families and provider entry
  shapes.
- [x] Update docs and run verification gates.

## Notes

- The live process route was already scoped; the failure mode was the visual
  provider signal, not the loopback app-server route.
- Current Codex Desktop no longer reliably renders the old provider chip from
  config alone. The robust visible signal is now the process-local
  `model/list` display-name prefix. Live shim logs showed both
  `config_read_rewrite` and `model_list_rewrite` for the scoped Desktop app.
- The Codex Desktop app bundle is extracted only to `/tmp` during inspection.
  No bundle files are copied into the repository.
- The monolith split reduced `cmd/slimference/main.go` from 4387 lines to 3974
  lines and moved the 426-line service-control adapter into a domain file.
- `research/rtk-ai/rtk/` was not touched.

## Deviations

None.
