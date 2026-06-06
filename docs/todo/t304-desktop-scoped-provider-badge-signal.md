# T304 Desktop Scoped Provider Badge Signal

## Why

Codex Desktop launched through Slimference was routed correctly by the app-server
shim, but the blank Codex.app start screen could still hide the visible
`Slimference` provider chip. That made the UX ambiguous: a user could launch
through the TUI and still not know whether the app instance was the scoped
Slimference instance.

The fix must not reintroduce persistent shared routing. Normal Codex CLI and
Finder/Spotlight Codex.app launches must remain direct. The badge signal belongs
only to the process-local app-server path started by Slimference.

## Acceptance

- The Desktop app-server shim still rewrites only default `thread/start`
  `modelProvider` values to `slimference-codex`.
- The shim augments only the matching `config/read` response observed through
  the same scoped JSON-RPC stream, setting the process-local
  `slimference-codex` provider config for the UI badge.
- Unknown stdout frames, notifications, malformed responses, error responses,
  and non-JSON data pass through byte-identically.
- No persistent `~/.codex/config.toml` route is written for the badge.
- Docs explain that the badge is scoped to Slimference-launched Codex.app.
- Targeted shim tests pass, then the full local gate passes before release use.

## Sub-Tasks

- [x] Add request-ID tracking inside the app-server mediator for `config/read`.
- [x] Add stdout mediation for the matching `config/read` response only.
- [x] Preserve byte-identical pass-through for unrelated app-server frames.
- [x] Add unit tests for badge config injection and pass-through behavior.
- [x] Update install/product docs.

## Notes

Implemented in `cmd/slimference/codex_desktop_app_server_shim.go`. The route
rewrite remains on stdin; the visual badge signal is stdout-side and scoped to
the same child app-server process. This keeps the product rule intact: normal
Codex launches stay direct, Slimference TUI launches show a visible Slimference
provider signal.

## Deviations

None.
