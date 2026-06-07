# T310 Same-terminal Codex CLI launch

Status: Done.

## Why

The TUI `Launch Codex CLI` action must not always open Apple Terminal. If the
user is running Slimference TUI in Ghostty, the scoped Codex CLI should open in
Ghostty; if the user is running Apple Terminal, it should open in Apple
Terminal. The launched shell must start in the same working directory as the TUI
and run the scoped Slimference command, while normal shell `codex` stays direct.

## Acceptance

- TUI launch detects `TERM_PROGRAM=ghostty` and opens a new Ghostty tab through
  the same app, then runs `slimference codex run --transport=auto --`.
- TUI launch detects `TERM_PROGRAM=Apple_Terminal` and opens a new Apple
  Terminal tab in the front window, then runs the same scoped command.
- The launch command scrubs inherited Codex runtime/session variables before
  starting the new Codex CLI session while preserving config-bearing values such
  as `CODEX_HOME` for MCP server visibility.
- The launched CLI starts in the TUI's current working directory.
- Direct/manual `codex` invocations remain untouched.
- Tests cover terminal detection, Ghostty launch script shape, Terminal launch
  script shape, unknown-terminal rejection, and error propagation.

## Sub-Tasks

- [x] Add terminal-app detection for Ghostty and Apple Terminal.
- [x] Add a Ghostty same-app new-tab launcher using patch-free macOS
  automation.
- [x] Add an Apple Terminal same-app new-tab launcher using Terminal
  AppleScript.
- [x] Route TUI `Launch Codex CLI` through the terminal-aware launcher.
- [x] Add regression tests for both terminal targets and launcher errors.
- [x] Update install and technical documentation.

## Notes

- Local Ghostty 1.1.3 exposes no supported macOS CLI launch action for spawning
  a terminal surface from a shell; same-app new-tab launch therefore uses
  AppleScript/System Events. If macOS Accessibility automation is denied, the
  TUI surfaces the error instead of silently falling back to the wrong terminal
  app.
- The `[SF]` title is still owned by `slimference codex run`, not by the TUI
  shell wrapper.
- Unknown terminal apps fail fast instead of silently opening the wrong app,
  because the product promise here is same-terminal launch.

## Deviations

None.
