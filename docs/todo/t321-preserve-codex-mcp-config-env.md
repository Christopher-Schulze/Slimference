# T321 Preserve Codex MCP Config Env On Scoped Launch

## Why

Launching Codex through Slimference must start a fresh Codex session without
leaking old `CODEX_THREAD_ID` / runtime state, but it must not hide Codex's
configuration root. The old TUI CLI launcher and Desktop launcher scrubbed every
`CODEX_*` environment variable. That protected against old session reuse, but it
also removed `CODEX_HOME` and any future config-bearing Codex env. If a user
keeps MCP server definitions in a non-default Codex home, the Slimference-scoped
launch could start without those MCP servers.

## Acceptance

- TUI `Launch Codex CLI` must drop only known volatile Codex runtime/session
  variables and must never use a broad `${!CODEX_@}` wipe.
- Direct `slimference codex run` must apply the same targeted runtime env
  cleanup before execing `codex`.
- Desktop scoped launch and proof env sanitization must preserve `CODEX_HOME`
  and other config-bearing `CODEX_*` keys while still dropping old thread/run
  state.
- Explicit Desktop app-server extras may pass safe config-bearing Codex env
  such as `CODEX_HOME`, but must not override Slimference's own shim variables
  or leak old runtime variables.
- Persistent advanced route insertion must preserve existing
  `[mcp_servers.*]` TOML tables.
- Desktop app-server config-read rewriting must preserve `mcp_servers` and
  `mcpServers` JSON objects.
- Targeted regression tests must cover all of the above.

## Sub-Tasks

- [x] Replace broad `CODEX_*` scrub in the same-terminal CLI launch shell with a
  shared targeted runtime-env drop list.
- [x] Apply the same targeted runtime-env drop list to direct
  `slimference codex run`.
- [x] Reuse the same targeted drop list in Desktop launch env sanitization.
- [x] Allow safe config-bearing Codex env through Desktop app-server extras
  while keeping Slimference shim overrides protected.
- [x] Add regression tests for `CODEX_HOME` / MCP config env preservation.
- [x] Add persistent route test proving `[mcp_servers.*]` tables survive route
  insertion.
- [x] Add Desktop config-read test proving `mcp_servers` / `mcpServers` survive
  provider injection.
- [x] Update install/TUI documentation to say volatile runtime env is scrubbed,
  not all `CODEX_*`.

## Notes

- Root cause: the original implementation treated the `CODEX_` prefix as a
  runtime/session namespace. Current Codex also uses `CODEX_HOME` as a config
  root, so prefix-based scrubbing can remove MCP server visibility.
- The fix intentionally keeps the safety property that a Slimference session
  opened from inside Codex cannot inherit `CODEX_THREAD_ID`, `CODEX_CI`, run IDs,
  or npm-managed runtime markers into the newly launched process.
- `slimference codex run` itself uses process-local `codex -c ...` provider
  overrides plus targeted `env -u` cleanup. Those overrides add the Slimference
  provider without replacing the user's existing config or MCP tables.

## Deviations

- None.
