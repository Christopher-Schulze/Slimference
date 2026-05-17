# TASK 206: Config path single source

Status: DONE 2026-05-17
Priority: P0
Scope: `cmd/slimference/install_cmd.go`, `cmd/slimference/install_helpers.go`, `cmd/slimference/hosts_lifecycle.go`, tests

## Why

The loader resolves the active config through env, XDG, then legacy paths. The Phase H commands previously wrote or read `~/.slimference/config.toml` in several places, while the daemon was reading `~/.config/slimference/config.toml`. That split made `enable`, `status`, SIGHUP reloads, and tests disagree.

## Acceptance

- `SLIMFERENCE_CONFIG` is always honored first.
- Default `enable` / `disable` writes the canonical XDG path `~/.config/slimference/config.toml`.
- Existing legacy `~/.slimference/config.toml` no longer silently wins for new default writes.
- Admin-port lookup and SIGHUP reload read through the same resolver instead of hard-coded legacy paths.
- Tests use explicit env or XDG temp paths instead of relying on global HOME state.

## Sub-Tasks

- [x] Rework `enableDisableConfigPath()` around config resolution.
- [x] Rework `defaultAdminPort()` to use `config.ResolveConfigPath`.
- [x] Rework SIGHUP config reload path to use the resolver.
- [x] Update install command tests to cover canonical XDG behavior.
- [x] Update helper and hosts lifecycle tests to use temp XDG config.

## Verification

- `go test ./cmd/slimference -count=1 -timeout 180s`
- `go test ./internal/install/... ./docs/ -count=1 -timeout 120s`

## Notes

The legacy file may still exist on disk from older runs. This task does not delete it. It only prevents new default Phase H commands from writing the wrong file and makes code paths agree on the loader's resolved source.
