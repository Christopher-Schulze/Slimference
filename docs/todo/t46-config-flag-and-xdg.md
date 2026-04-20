# T46 - `--config <path>` Flag + XDG-Compliance Fallback

Status: todo
Priority: P1
Scope: `cmd/slimference/main.go`, `internal/config/loader.go`, `docs/documentation.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Config is resolved via:

1. `SLIMFERENCE_CONFIG` env var (if set)
2. `~/.slimference/config.toml`

There is no CLI flag to override at invocation, and the default path is not
XDG-Base-Directory-Specification compliant. Users who want to run multiple
profiles side-by-side (e.g. `slim-dev.toml`, `slim-prod.toml`) must set the
env var per invocation. Linux/NixOS power-users expect
`$XDG_CONFIG_HOME/slimference/config.toml`.

## Current State

- Loader: `config.Load(path string) (*Config, error)` called with the env
  or default path.
- No flag plumbing.

## Target State

- `slimference --config /path/to/config.toml ...` overrides everything.
- Precedence (highest first):
  1. `--config <path>`
  2. `SLIMFERENCE_CONFIG` env var
  3. `$XDG_CONFIG_HOME/slimference/config.toml`
  4. `~/.config/slimference/config.toml` (XDG default when env unset)
  5. `~/.slimference/config.toml` (legacy - still supported, deprecated)
- `doctor` prints which path was selected and which were checked.
- Migration helper: `slimference config migrate` moves legacy path to XDG.

## Design

### Loader signature

```go
type LoadOptions struct {
    ExplicitPath string  // from --config
    EnvVar       string  // "SLIMFERENCE_CONFIG"
    AllowLegacy  bool    // default true
}
func Load(opts LoadOptions) (*Config, LoadInfo, error)

type LoadInfo struct {
    ResolvedPath string
    Source       string  // "flag" | "env" | "xdg" | "legacy" | "defaults"
    Checked      []string
}
```

### Legacy deprecation

- Continue to read `~/.slimference/config.toml` but emit one-shot
  `slog.Warn(event=config_legacy_path_in_use)` with hint to migrate.
- Keep data-paths (`~/.slimference/analytics`, `~/.slimference/readcache`
  etc.) unchanged - out of scope here; only config file moves.

### Doctor output

```
[OK  ] Config file: /home/chris/.config/slimference/config.toml (source=xdg)
       checked: flag=-, env=-, xdg=/home/chris/.config/slimference/config.toml,
                legacy=/home/chris/.slimference/config.toml
```

### Migrate command

```
$ slimference config migrate
  legacy: /home/chris/.slimference/config.toml
  target: /home/chris/.config/slimference/config.toml
  action: copy -> verify -> remove legacy (with --delete-legacy)
```

## Implementation Plan

### WP1 - Loader refactor
- Change `Load` to take `LoadOptions` + return `LoadInfo`.
- Update all callers.

### WP2 - CLI flag
- `--config <path>` plumbed from `main.go` into loader.

### WP3 - XDG resolution
- Pure helper `xdgConfigPath() string` honouring `$XDG_CONFIG_HOME`.

### WP4 - Doctor integration
- Expose `LoadInfo` in `doctor` command output.

### WP5 - Migrate subcommand
- `slimference config migrate [--delete-legacy]`.

### WP6 - Tests
- Table test covering all 5 precedence levels.
- Symlink / unreadable / malformed file error paths.

---

## Subtasks

- [ ] Refactor `config.Load` to `LoadOptions` + `LoadInfo`.
- [ ] Add `--config` CLI flag in main.
- [ ] Implement XDG resolver + legacy fallback with warn.
- [ ] Extend `doctor` output with source and checked list.
- [ ] Implement `slimference config migrate`.
- [ ] Golden-file test for doctor output.
- [ ] Unit tests covering precedence matrix.
- [ ] Update `docs/documentation.md` §Config and README.

## Risks

- Users with custom `~/.slimference` symlinks break on migrate. Guard:
  detect symlink target, refuse migrate if target != expected.
- Race between concurrent migrations: use atomic rename.

## Acceptance Criteria

- [ ] All 5 precedence levels tested.
- [ ] `slimference --config /tmp/foo.toml ...` loads that file.
- [ ] `doctor` reports resolved path + source.
- [ ] Legacy path still works with deprecation warn.
- [ ] Migrate command copies + verifies + (optionally) deletes legacy.

## Out of Scope

- Data-directory migration (analytics/readcache/toolarchive paths).
- Multi-config merge (single file only).

---

## Validation

```
./slimference --config ./testdata/custom.toml doctor
XDG_CONFIG_HOME=/tmp/xdg ./slimference doctor
./slimference config migrate --delete-legacy
go test ./internal/config/...
```
