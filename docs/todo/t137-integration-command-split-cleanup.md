# TASK 137: Integration command split cleanup and non-mutating transparent-mode product path

Status: DONE (opened 2026-05-13, completed 2026-05-13)
Priority: P0
Scope: `cmd/slimference/integrate_cmd.go`, `cmd/slimference/proxy_cmd.go`, `cmd/slimference/main.go`, `internal/integrate/`, `internal/hooks/`, `internal/transparent/`, `docs/integration.md`, `docs/transparent-mode.md`, `docs/todo/t65-auto-integration-installer.md`, `docs/todo/t72-codex-integration-single-owner.md`.

## Why

The repo currently has overlapping integration concepts:

- `integrate install --client codex`: mutates Codex config to point at Slimference.
- `hook install codex`: installs Codex hooks only.
- `proxy install/enable`: transparent system-proxy/CA path.

The product target is now clearer: transparent proxy/CA/daemon is the default path and must not mutate Codex config. Config-patch and hook modes remain explicit advanced/legacy modes.

The documentation also claims T72 unified ownership, but the current code path must be re-audited and fixed so command names, effects, verify checks, and TUI actions all say and do the same thing.

## Target State

Three separate modes with no ambiguity:

1. `proxy` mode:
   - installs local CA + daemon + macOS proxy control.
   - does not edit `~/.codex`.
   - default recommended path for Codex App/CLI transparent interception.
2. `hook` mode:
   - installs optional Codex/Claude hooks.
   - does not edit base URLs.
   - clearly marked optional and limited by hook contract.
3. `integrate` mode:
   - legacy/config-patch direct routing.
   - explicitly edits client config.
   - used only when operator asks for config mutation.

## Work Packages

### WP1 - Command semantics audit

- [x] Read all call paths:
  - `handleHookCmd`
  - `handleIntegrateCmd`
  - `proxyRun`
  - `hooks.InstallCodex`
  - `integrate.Install`
  - `integrate.DetectCodex`
  - `hooks.InspectCodexHooks`
- [x] Produce a code-level matrix:
  - command
  - files touched
  - daemon touched
  - networksetup touched
  - keychain touched
  - Codex config touched
  - Codex hooks touched
  - reversible command

### WP2 - Rename or clarify outputs

- [x] `slimference hook install codex` output says only hooks and explicitly says Codex config was not modified.
- [x] `slimference integrate install --client codex` remains config-patch mode and docs/help label it as legacy/config-patch.
- [x] `slimference proxy install` remains daemon/CA setup and `proxy enable` remains the arming step.
- [x] `hook verify codex` no longer requires `openai_base_url` / `chatgpt_base_url`.
- [x] `integrate status --client codex` remains the config-patch truth surface.

### WP3 - Detect split

- [x] Split status into independent dimensions:
  - `codex_binary`
  - `codex_config_patch`
  - `codex_hooks`
  - `transparent_daemon`
  - `transparent_proxy`
  - `transparent_ca`
- [x] Avoid one "fully wired" label that conflates hook/config/proxy modes in hook status; integrate status keeps its legacy/config-patch meaning.

### WP4 - TUI integration

- [x] TUI hook status no longer treats missing Codex config-patch as missing hook install.
- [x] TUI/CLI wording now separates:
  - Transparent Ready
  - Hooks Optional
  - Config Patch Off
  - Daemon Off/On
- [ ] Dedicated TUI control-plane action groups move to T133.

### WP5 - Backward compatibility

- [x] Existing users with config-patch blocks keep working.
- [x] `integrate emergency-off` remains able to remove config patches.
- [x] `proxy disable` does not remove hooks/config patches.
- [x] `hook remove codex` does not remove config patches.

### WP6 - Tests

- [x] Golden tests for help/output strings and config-neutral hook install.
- [x] Detect tests cover split ownership:
  - no Codex
  - Codex binary only
  - hooks only
  - config patch only
  - transparent daemon/CA only
  - all modes.
- [x] Idempotence tests for install/remove remain green.

## Acceptance

- [x] No command claims to modify config unless it does.
- [x] `hook verify codex` verifies hooks only.
- [x] `integrate status --client codex` reports config-patch separately from transparent mode.
- [x] TUI hook status no longer reports Codex as broken only because config-patch is absent; full operator console is T133.
- [x] Default transparent/hook install path can leave `~/.codex/config.toml` untouched.
- [x] Existing config-patch users stay supported.
- [x] `go run ./scripts/ci` passes.

## Notes

- This task is first in Phase AA because every later UX and certification task depends on honest command semantics.
- Implemented by splitting hook, integrate, and proxy ownership boundaries in code, tests, help text, integration docs, and transparent-mode docs.
- `go run ./scripts/ci` passed 8/8 after the change, including 100.0% statement coverage.
