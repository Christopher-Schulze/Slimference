# T313 TUI keyboard responsiveness hotpath

Status: Done.

## Why

The TUI felt laggy when moving through menu entries. The hot key path was
writing the persisted preference file on pure cursor movement, even though
cursor positions are not part of `PersistedState`. That made repeated keypresses
pay needless filesystem I/O.

## Acceptance

- Main, Savings, Logs, and Setup cursor movement does not write
  `~/.slimference/tui_state.json`.
- Numeric Setup step selection does not write state.
- Real view changes and explicit preference saves still persist.
- Regression test proves cursor movement does not create a state file.
- TUI tests and full CI are green.
- Installed binary is rebuilt from the fixed tree.

## Sub-Tasks

- [x] Remove `persistStateBestEffort` calls from cursor-only key paths.
- [x] Remove `persistStateBestEffort` calls from Setup step selection.
- [x] Keep persistence for real view/config changes and quit/save commands.
- [x] Add regression coverage for no-write navigation.
- [x] Run focused TUI tests and full CI.
- [x] Rebuild and install the local binary.

## Notes

- This is a direct latency fix: less synchronous disk I/O per keypress.
- The persisted state schema does not store cursor positions, so no user-visible
  preference is lost by skipping those writes.

## Deviations

None.
