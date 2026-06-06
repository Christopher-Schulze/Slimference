# TASK T303: Desktop scoped app status signal

## Why

Codex.app may not show a visible Slimference provider badge even when it was
started through the Slimference app-server shim. Users need a Slimference-owned
runtime signal instead of guessing from Codex.app UI chrome.

## Acceptance

- TUI Launch and Status surfaces show when a live Codex.app process has a
  Slimference `app-server` child.
- The signal does not claim traffic savings before a request is observed; it
  only proves scoped launch mode is active.
- Normal Codex direct mode remains unchanged outside Slimference-launched
  processes.
- Focused tests, CI, build/restart, and preflight checks pass.

## Sub-Tasks

- [x] Add TUI-facing `AppServerActive` state for Codex Desktop.
- [x] Detect a live `slimference app-server` child process.
- [x] Render "scoped app active" in Launch/Status instead of relying on the
  external Codex.app badge.
- [x] Add focused tests for the new state vocabulary.

## Notes

- The current live machine had Codex.app running with `CODEX_CLI_PATH` pointing
  at Slimference and a `slimference app-server` child process. That proves the
  scoped launch path is active. It does not by itself prove a fresh prompt has
  flowed through WSS after daemon restart.

## Deviations

- None.
