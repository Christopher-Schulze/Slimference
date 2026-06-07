# T319 TUI Product Surface Redesign

## Why

The daily TUI had drifted back toward internal diagnostics: launch actions wrote raw command/service text into the main screen, Activity exposed provider and route identifiers, Savings showed parser/archive/debug blocks instead of user accounting, and Setup mixed operational text with unstable row layout. The product surface must be fast, simple, and directly useful for normal users while keeping advanced proof data available through CLI/admin diagnostics.

## Acceptance

- Home remains a simple menu with Launch Codex CLI, Launch Codex App, Activity, Savings, Status, Logs, and Setup.
- Launch actions are asynchronous and show short start/success/failure feedback instead of raw shell or service output.
- Codex CLI same-terminal launch clears the visible raw command and prints a short `[SF]` preamble before exec.
- Activity uses user-facing labels and hides raw provider IDs, backend paths, internal route modes, and stale hook sessions.
- Savings renders product accounting: total original/sent/saved tokens, per-session rows, cache contribution, and safety state.
- Status renders daemon/install/current-use/health only, without direct-route or advanced lab wording.
- Logs render diagnostics export, compact route summary, and recent daemon events without raw plan/layer internals.
- Setup renders a stable four-row install/repair checklist and daemon/autostart state without command spam or advanced lab controls.
- TUI color palette avoids the old turquoise-heavy look and keeps warm, readable product accents.
- Targeted TUI and launcher tests verify the new product surface and block regressions to old internal labels.

## Sub-Tasks

- [x] Convert launch actions to asynchronous result messages with transient user-facing feedback.
- [x] Clean Codex CLI same-terminal startup output and keep the `[SF]` terminal title indicator.
- [x] Replace Activity raw route/provider/path rendering with product labels.
- [x] Replace Savings debug cards with total/session/cache/safety accounting cards.
- [x] Replace Logs flight-recorder internals with route summary and recent events.
- [x] Stabilize Setup rows and remove long command/action text from the product surface.
- [x] Adjust TUI styling palette away from turquoise toward warm product accents.
- [x] Bump product version display to `v0.9.1`.
- [x] Update documentation and TODO surfaces.
- [x] Run targeted TUI, launcher, and version tests.
- [x] Run full repo CI, rebuild/install `v0.9.1`, restart daemon, and recertify Codex WSS for the new version.

## Notes

- Desktop still has no patch-free in-app chip on current Codex Desktop builds. Route confirmation is intentionally surfaced through Activity, Status, Logs, and daemon decisions instead of mutating model metadata or frontend bundles.
- Savings remains evidence-based. If a session has no Slimference flight/token data yet, the UI shows no session data instead of inventing a number.
- Advanced route plans, parser matrices, checkpoints, archive internals, and hook-turn records remain available via CLI/admin/debug paths but are no longer daily TUI content.
- Verification completed: `go test ./internal/tui -count=1`, targeted `cmd/slimference` launcher tests, `go test ./...`, `go test ./docs -count=1`, `go run ./scripts/ci`, `go run ./scripts/build -restart -version 0.9.1`, `slimference status --preflight`, and `slimference codex recertify wss --force --operator codex --notes tui-product-surface-v0.9.1`.

## Deviations

- None.
