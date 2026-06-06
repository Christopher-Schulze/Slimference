# TASK T302: TUI live log window

## Why

The Logs view read the tail of the global rotating slog file. That file also
contains historical local test and CI output, so old synthetic failures could
appear as current product failures in the TUI.

## Acceptance

- Remote TUI log view only shows daemon log lines written after the TUI adapter
  starts.
- Historical test/CI log tail noise does not appear as a current live product
  error.
- Existing log export still exports the visible live window.
- Focused tests, repo CI, build/restart, and status checks pass.

## Sub-Tasks

- [x] Add a live cutoff to the remote file-backed session logger.
- [x] Update tests for old-vs-live log filtering.
- [x] Run gates, rebuild, install, restart, and commit.

## Notes

- The live daemon itself was healthy during the report. The displayed
  `sni_peek` and `phase_g` failures were old/test-tail lines, not current
  active route failures.

## Deviations

- None.
