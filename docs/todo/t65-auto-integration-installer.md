# T65 - Auto-Integration Installer (Claude Code + Codex)

Status: todo
Priority: P0
Scope: `internal/integrate/` (new), `cmd/slimference/integrate_cmd.go` (new), `cmd/slimference/main.go`, `internal/hooks/`, `internal/daemon/`, `internal/tui/`
Driver: 2026-04-20 user requirement "volles Paket, muss wirklich absolut funktionieren"

---

## Problem

Slimference today works in pieces:

- Claude Code requires the user to `export ANTHROPIC_BASE_URL=http://127.0.0.1:8990` manually.
- Codex accepts `openai_base_url` in `~/.codex/config.toml` (research 2026-04-20)
  plus `chatgpt_base_url` for the backend-api routes, but neither is wired.
- Hooks are separately installed via `slimference hook install claude|codex`.
- launchd is separately installed via `slimference service install`.

A new user has to run four disconnected commands and edit a shell rc file by
hand. If any one of those steps fails or drifts, the integration falls apart
silently. We need one command that wires everything up idempotently and one
command that tears everything down cleanly.

## Target State

Two CLI verbs:

```
slimference integrate status     # report per-client wiring state
slimference integrate install    # idempotent wire-up (all detected clients)
slimference integrate remove     # clean tear-down
slimference integrate install --client claude    # target one client only
slimference integrate install --dry-run          # print diff without writing
```

Plus TUI badges (covered by T67).

## Scope per client

### Claude Code
- **Detection**: `which claude` in PATH AND `~/.config/claude/*.json` / `~/.claude/` exists.
- **Wire points**:
  1. Hook install (existing: `slimference hook install claude`).
  2. `ANTHROPIC_BASE_URL=http://127.0.0.1:8990` exported via a
     Slimference-marked block in the user's shell rc (zsh/bash/fish auto-detected).
- **Remove** undoes both.

### Codex
- **Detection**: `which codex` AND `~/.codex/config.toml` exists.
- **Wire points**:
  1. Hook install (existing: `slimference hook install codex`).
  2. `openai_base_url = "http://127.0.0.1:8990"` written into
     `~/.codex/config.toml` inside a Slimference-marked block (preserves
     existing keys + comments).
  3. `chatgpt_base_url = "http://127.0.0.1:8990"` same block (routes the
     backend-api traffic through the proxy).
- **Remove** undoes all three.

### Daemon
- **Wire points**:
  1. launchd install with `KeepAlive=true` and `RunAtLoad=true` (T68 delivers
     the plist template).
  2. Health probe after install: `curl 127.0.0.1:8990/admin/health` must return
     200 within 5 s or install reports `degraded`.
- **Remove** runs `launchctl unload` then deletes the plist.

### Shell RC manipulation

- Uses fenced marker lines so edits are round-trip safe:
  ```
  # >>> slimference integration >>>
  export ANTHROPIC_BASE_URL=http://127.0.0.1:8990
  # <<< slimference integration <<<
  ```
- Targets are auto-detected in priority order: `$ZDOTDIR/.zshrc`, `~/.zshrc`,
  `~/.bashrc`, `~/.bash_profile`, `~/.config/fish/config.fish`.
- If multiple exist, writes into the shell matching `$SHELL`; documents that
  in the status output.

### config.toml manipulation

2026-05-13 update: modern ChatGPT-auth Codex appends `/responses` to
`openai_base_url`, so the implemented values are now backend-prefixed:

```
openai_base_url = "http://127.0.0.1:8990/backend-api/codex"
chatgpt_base_url = "http://127.0.0.1:8990/backend-api/"
```

The historical root values below are retained as original T65 planning context,
not as current implementation truth.

- Uses the same marker fence in TOML comments:
  ```
  # >>> slimference integration >>>
  openai_base_url = "http://127.0.0.1:8990"
  chatgpt_base_url = "http://127.0.0.1:8990"
  # <<< slimference integration <<<
  ```
- Appended at end of file if marker block absent; replaced in-place if present.
- Pre-edit snapshot to `~/.codex/config.toml.slim-backup-<ts>` before first
  edit so an anxious user can revert.

## Daemon-down fallback semantics

Critical: the user's requirement is "wenn der daemon abkackt muss der traffic
normal durchgehen".

Path A - **daemon process crashed**:
- launchd KeepAlive restarts in 1-2 s (T68).
- During the gap the client sees `ECONNREFUSED`.
- Anthropic SDK retries 3x with exponential backoff by default (reqwest /
  Python SDK both do this).
- Net effect: user barely notices; worst case a single request fails and the
  client surfaces a retry banner.

Path B - **user wants hard bypass**:
- `slimference integrate remove` strips the shell rc block and the
  config.toml block.
- User must reload their shell (`exec $SHELL -l`) or restart Claude Code /
  Codex for the new env to take effect.
- Traffic now flows direct to Anthropic / OpenAI.

Path C - **soft bypass without uninstall** (covered by T67):
- TUI master switch disables every provider + every layer.
- Proxy keeps accepting connections but forwards bytes unmodified (true
  transparent relay).
- Adds ~1 ms of latency; no token savings; zero compression risk.
- Hot-reload, no shell restart needed.

## Implementation Plan

### WP1 - `internal/integrate/detect.go`
- `DetectClaudeCode() ClientStatus` (binary present, config dir present, hooks
  installed, shell rc wired).
- `DetectCodex() ClientStatus` (binary, config.toml, hooks, config.toml wired).
- `DetectDaemon() DaemonStatus` (plist present, launchctl shows it, `/admin/health` 200).
- `ClientStatus` enum: NotInstalled, Installed, PartiallyWired, FullyWired.

### WP2 - `internal/integrate/shellrc.go`
- `DetectRCFile() string` with the priority chain.
- `ReadBlock(path, marker) (string, exists bool, err)`.
- `WriteBlock(path, marker, content) error` (atomic: write-temp + rename).
- `RemoveBlock(path, marker) error`.
- Round-trip fixtures: zsh, bash, fish, empty file, missing file, file with
  marker at top / middle / end.

### WP3 - `internal/integrate/codex_toml.go`
- Parses `~/.codex/config.toml` with the BurntSushi library already in go.mod.
- Adds `openai_base_url` + `chatgpt_base_url` into a marked block.
- Preserves every other key, comment, and order (use a re-emit that keeps the
  un-touched sections verbatim via string-based edit, not TOML round-trip -
  round-tripping loses comments).
- Creates a timestamped backup before first write.

### WP4 - `internal/integrate/install.go`
- Top-level `Install(opts Options) Report` that orchestrates:
  1. Detect each target.
  2. For each target not FullyWired: apply the delta.
  3. Emit a Report with per-step outcomes.
- Idempotent: re-running yields all-green with no writes.

### WP5 - `cmd/slimference/integrate_cmd.go`
- `handleIntegrateCmd(args)` with subcommands `status|install|remove`.
- `--client claude|codex|daemon|all` filter.
- `--dry-run` prints unified diff of intended writes.
- `--force` re-applies blocks even if marker exists (self-healing).

### WP6 - Tests
- Table-driven for shellrc + codex_toml manipulation.
- Integration test: temp HOME, install, status reports FullyWired, remove,
  status reports NotInstalled.
- Dry-run test: no writes occur, but diff output matches expected.
- Legacy-config-preserve test: existing Codex config keys survive.

### WP7 - Docs
- `docs/integration.md` walk-through (install / status / remove / troubleshoot).
- Update `docs/documentation.md` Appendix P with a new "Auto-Integration"
  subsection.

## Risks

- **Shell rc corruption** on an exotic shell setup. Mitigation: only touch
  files whose parent dir exists, always write via temp+rename, always leave
  a `.slim-backup-<ts>` copy on first write.
- **TOML round-trip loses comments** if we use the library re-emit path.
  Mitigation: string-based edit with marker fences, never re-emit the whole
  file.
- **Concurrent edits** if the user edits rc while install runs. Accept - we
  are not a multi-writer system; we check mtime before replace and fail loud
  if changed.
- **Fish shell syntax** differs. Mitigation: emit `set -gx ANTHROPIC_BASE_URL http://127.0.0.1:8990`
  instead of `export ...` when fish is detected.

## Acceptance Criteria

- [ ] `slimference integrate install` on a fresh machine wires Claude Code,
      Codex, and the daemon in one command, idempotently.
- [ ] `slimference integrate status` reports per-client wiring state with
      color-coded output.
- [ ] `slimference integrate remove` undoes every side effect cleanly.
- [ ] `--dry-run` prints the diff without writing.
- [ ] Every file edit leaves a timestamped backup on first touch.
- [ ] Shell rc edits survive round-trip (re-running install finds marker,
      makes no change).
- [ ] Codex config.toml edits preserve every other key and comment.
- [ ] `go test -race ./internal/integrate/...` green.

## Out of Scope

- GUI wizard.
- Cross-shell auto-migration (e.g. copying zsh block into bash).
- Editing `$PATH` or Homebrew prefixes.
- MITM / CA install - not needed because both clients accept plain-HTTP
  base-URL overrides.

---

## Validation

```
# fresh state
slimference integrate status
# -> Claude Code: NotWired, Codex: NotWired, Daemon: NotInstalled

slimference integrate install --dry-run
# -> prints diff blocks

slimference integrate install
# -> writes rc block, codex block, launchd plist, reports FullyWired

slimference integrate install     # idempotent
# -> reports AlreadyWired, no changes

slimference integrate remove
slimference integrate status      # reports NotWired again
```
