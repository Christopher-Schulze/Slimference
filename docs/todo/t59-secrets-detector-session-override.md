# T59 - Secrets-Detector Per-Session-Override + Allowlist-Session-TOML

Status: todo
Priority: P2
Scope: `internal/security/`, `cmd/slimference/`, `internal/tui/`, `docs/documentation.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`internal/security.Redact` applies 12 built-in secret-pattern detectors
plus a user-configurable allowlist. The allowlist is **global** (config
file). For users who legitimately discuss API keys in code snippets
(security research, setup guides, CI debugging), there is no way to
disable redaction for a single session without editing config and
restarting the proxy.

Friction: users silently see `[REDACTED]` where their real content
should be; often they do not realise Slimference is the cause and
blame their client.

## Current State

- `[security] redact_mode = "redact" | "warn" | "block"`.
- `[security.allowlist] patterns = [...]` - global.
- No per-session override; no hot reload.

## Target State

Three new mechanisms:

1. **Session allowlist file**:
   `~/.slimference/allowlist.session.toml`, watched for changes
   (fsnotify, already a dep). Applies on top of config allowlist for
   the current process lifetime.

2. **CLI temp-disable**:
   `slimference security disable-for <duration>` (max 1 h). Pauses
   redaction entirely with a bright slog.Warn every minute. Auto-resumes.

3. **TUI hotkey**: `r` in Stats view toggles redaction status display
   (on / off / warn-only). Requires explicit confirmation dialog.

## Design

### Session allowlist file

`~/.slimference/allowlist.session.toml`:

```toml
# Regexes appended to the active allowlist for this process lifetime.
# Edited live; reloaded on save.
patterns = [
  '^AKIA[0-9A-Z]{16}$',      # project-specific AWS keys
  '^sk-fake-',               # known fake keys
]
# Exact strings, compared as whole words.
literals = [
  "sk-ant-test-abc123",
]
```

Watcher: `internal/security/session_allowlist.go` uses fsnotify;
applies on write within 200 ms debounce. Failure to parse logs warn
and keeps prior state.

### CLI command

```
slimference security disable-for 10m
slimference security enable
slimference security status
```

- `disable-for <dur>`: set global "redaction paused until
  <absolute-time>". stored in an atomic time.Time.
- `enable`: reset.
- `status`: print current mode + paused-until.

While paused, every request logs
`event=security_bypass_active remaining=<dur>`. TUI Stats shows red
"SECURITY BYPASS ACTIVE until 14:32:11" banner.

### TUI hotkey

`r` in Stats view opens a modal:

```
Toggle redaction mode
  [ ] redact (current)
  ( ) warn
  ( ) off (15 min)
  ( ) off (1 hour)

[confirm] [cancel]
```

Confirmation writes through to the same atomic state.

### Max duration guard

Hard cap at 1 h. Attempts to set longer are clamped with warn.

### Persistence

None. Session override dies with the process. Legit: if user wants
permanent, they edit `config.toml`.

## Implementation Plan

### WP1 - Session allowlist watcher
- Watch file, reload on change, merge with config allowlist.

### WP2 - Disable-for state
- Atomic `time.Time` for paused-until.
- Pipeline checks before pattern run.

### WP3 - CLI subcommand
- `slimference security ...` with three verbs.

### WP4 - TUI hotkey + modal.

### WP5 - Telemetry
- Counter `security_bypass_seconds_total` (for audit).
- Log events on bypass start / end.

### WP6 - Tests
- Watcher reloads on file change.
- Disable-for expires correctly.
- Max-duration clamp.
- TUI modal flow.

---

## Subtasks

- [ ] Session allowlist file + fsnotify watcher.
- [ ] Atomic disable-for state.
- [ ] CLI `security disable-for | enable | status`.
- [ ] TUI hotkey + modal.
- [ ] Telemetry counters + log events.
- [ ] Documentation in `docs/documentation.md` §13.
- [ ] Tests.

## Risks

- User disables and forgets. Mitigation: 1 h hard cap + prominent
  banner + periodic warn log.
- Allowlist file path conflicts with config. Clarify in docs that this
  is a separate, session-only file.
- Audit gap. Mitigation: every bypass window logs start + end + total
  requests in the window.

## Acceptance Criteria

- [ ] Session allowlist file reloads within 500 ms of save.
- [ ] `disable-for 15m` pauses redaction, expires on time.
- [ ] Max duration clamped at 1 h.
- [ ] TUI hotkey opens modal, state roundtrips.
- [ ] Audit log records every bypass window.
- [ ] `go test -race ./...` green.

## Out of Scope

- Per-provider / per-route override.
- Cross-session persistence.

---

## Validation

```
./slimference security disable-for 1m
./slimference security status
echo 'patterns = ["^foo.*"]' > ~/.slimference/allowlist.session.toml
go test -race ./internal/security/...
```
