# TASK 196: Full reversibility audit for the Phase G install

Status: PLANNING 2026-05-16
Priority: P0 (the "kann ihn entfernen" piece - critical for trust)
Scope: `cmd/slimference/proxy_cmd.go`, new
       `internal/control/reversibility/`, integration tests under
       `tests/integration/reversibility_e2e.go`

## Why

A system-trust-modifying installer needs a credible uninstall. If the
user runs install, then later uninstalls, the system MUST be byte-equal
to its pre-install state with respect to every artefact we touched.

CLAUDE.md "Reversibility-by-default" rule.

## Touchpoints we modify on install

| Artefact                                    | Owner          | Reversal                    |
|---------------------------------------------|----------------|----------------------------|
| `~/.slimference/ca/{root.crt, root.key}`    | Slimference    | `slimference ca purge`     |
| `/Library/Keychains/System.keychain` trust  | OS, sudo       | `security remove-trusted-cert` |
| `/etc/hosts` entries (marker-fenced)        | OS, sudo       | restore from backup        |
| `~/Library/LaunchAgents/com.slimference.proxy.plist` | OS-user | `launchctl unload` + rm    |
| `/etc/pf.anchors/com.slimference` (if used) | OS, sudo       | flush + rm                  |
| `~/.config/slimference/*`                   | Slimference    | leave or purge per flag    |
| `~/.codex/config.toml` (if patched)         | Slimference    | restore from backup        |
| `~/.codex/hooks.json` (if patched)          | Slimference    | restore from backup        |
| `~/.claude/settings.json` (if patched)      | Slimference    | restore from backup        |

For every patched user file we keep a backup copy at the moment of
first patch under `~/.slimference/backups/<file-relpath>.<timestamp>`.
The uninstall restores the most recent backup.

## Snapshot-diff verification

The uninstall acceptance test:

1. Take a snapshot of every touchpoint **before** install.
2. Install.
3. Use.
4. Uninstall.
5. Take a snapshot **after** uninstall.
6. Diff. The diff MUST be empty for:
   - `/etc/hosts` content.
   - Trusted-roots list in System.keychain (count + fingerprint set).
   - `~/Library/LaunchAgents/` directory contents.
   - `~/.codex/config.toml` content (modulo whitespace).
   - `~/.codex/hooks.json` content.

The diff MAY contain:
- `~/.slimference/` directory (the CA files are NOT auto-purged unless
  the user explicitly runs `slimference ca purge`; documented in T191).
- `~/.config/slimference/*` (user-edited configs survive uninstall to
  preserve operator preferences; explicit `--purge-config` flag wipes).
- Log files under `~/.slimference/log/` (forensic).

## Install rollback policy

If install fails partway through (e.g. user cancels sudo prompt for
Keychain trust), the installer must roll back every step it completed
so far. Atomic semantics or close to it.

Implementation: each install step is a `Step` with `apply()` and
`reverse()` methods. The installer applies steps in order. On any
failure, it reverses applied steps in reverse order.

## Implementation

```go
package reversibility

type Step interface {
    Name() string
    Apply(ctx context.Context) error
    Reverse(ctx context.Context) error
    Inspect(ctx context.Context) State   // already-applied / not / partial
}

type Installer struct {
    Steps []Step
}

func (i *Installer) Apply(ctx context.Context) error {
    applied := []Step{}
    for _, step := range i.Steps {
        if err := step.Apply(ctx); err != nil {
            // Roll back applied steps
            for k := len(applied) - 1; k >= 0; k-- {
                _ = applied[k].Reverse(ctx)
            }
            return err
        }
        applied = append(applied, step)
    }
    return nil
}

func (i *Installer) Reverse(ctx context.Context) error {
    for k := len(i.Steps) - 1; k >= 0; k-- {
        if err := i.Steps[k].Reverse(ctx); err != nil {
            return err // continue with remaining steps; report errors as a chain
        }
    }
    return nil
}
```

Each step lives in its own file:

- `step_ca_generate.go`
- `step_ca_keychain_trust.go`
- `step_hosts_patch.go`
- `step_launchd_install.go`
- `step_pfctl_install.go` (only if pfctl route chosen instead of hosts)
- `step_codex_config_backup.go` (no-op if config doesn't need patching)
- `step_hooks_config_backup.go`

## Sub-Tasks

- [ ] Define `Step` interface + `Installer`.
- [ ] Implement each step with apply/reverse/inspect.
- [ ] Backup directory layout `~/.slimference/backups/<rel>.<ts>`.
- [ ] Snapshot helper: `reversibility.Snapshot()` returns a hash-tree
      of every touchpoint.
- [ ] `slimference ca purge` command (deletes CA files after confirming
      keychain trust is already removed).
- [ ] Integration test:
      - `tests/integration/reversibility_e2e.go`
      - Snapshot → install → snapshot → uninstall → snapshot → diff.
      - Runs against a temporary user-home / temporary keychain.
- [ ] Operator runbook section in `docs/operations.md`: "Recovering
      from a partial install".

## Acceptance

- E2E snapshot-diff test passes on macOS arm64.
- Mid-install failure (synthetic injection of an error in any step)
  rolls back cleanly; final snapshot equals initial snapshot.
- A 30-day-old install is uninstalled cleanly; every artefact reversed.
- User can manually run `slimference ca purge` to delete the on-disk
  CA files after uninstall.

## Notes

- Snapshot is privacy-preserving: only file paths + content hashes are
  recorded, never raw content (except for `/etc/hosts` because the file
  is short and required for diff readability).
- The integration test runs in CI via macOS GitHub Actions runner with
  a synthetic Keychain (we can use a per-test temporary keychain to
  avoid touching the user's real keychain).

## Deviations

(none yet)
