# T37 - Claude Read Hook Cache + Delta Import from `token-optimizer`

Status: done
Priority: high
Scope: `internal/readcache`, `internal/hooks`, `internal/proxy`, `internal/tui`, `cmd/slimference`
Source repo: `repos/token-optimizer`

---

## Problem

Slimference compresses `Read` output after it already entered the conversation.
That still leaves a repeated-file-read waste pattern untouched:

- identical `Read` on the same file and same range
- full-file re-read after a small edit
- repeated "confirm again" reads of unchanged files

`repos/token-optimizer` contains the only foreign implementation examined so far
that adds a real new lever here: a pre-tool `Read` cache with unchanged-read
blocking and delta-style feedback.

This is not currently covered by Slimference's existing:

- Layer 1 deterministic compression
- Layer 2 summarization
- Layer 3 response cache

So this feature is additive, not duplicate.

---

## Reality-Checked Extraction from the Foreign Repo

Relevant upstream references:

- `repos/token-optimizer/skills/token-optimizer/scripts/read_cache.py`
- `repos/token-optimizer/openclaw/src/read-cache.ts`

Ideas worth importing:

1. Session-scoped read memory, keyed by file path + range.
2. Hard stop for unchanged repeated reads.
3. Full-file delta on re-read after a small edit.
4. Minimal, deterministic local state. No LLM needed.

Ideas explicitly not imported:

1. The monolithic Python control plane in `measure.py`.
2. Shadow telemetry / coach / dashboard coupling.
3. Broad self-heal logic.
4. Plugin-specific OpenClaw / Claude product glue.

---

## Desired End State

### Phase 1 - Hard stop for unchanged reads

- [x] Claude `PreToolUse` on `Read` is routed through Slimference.
- [x] If the same session requests the same file with the same offset/limit and the
  file is unchanged on disk, Slimference blocks the tool call with a concise
  reason explaining the file is already in context.

### Phase 2 - Small-file full-read delta

- [x] For full-file reads only (`offset=0`, `limit=0`), Slimference stores a bounded
  cached snapshot.
- [x] If the file changed and the change can be summarized in a shorter, readable
  delta, Slimference blocks the re-read and returns that delta in the hook
  reason.

### Phase 3 - Hygiene and operability

- [x] Session cache files are stored under `~/.slimference/read-cache/`.
- [x] Cache format is JSON, deterministic, and small.
- [x] Slimference hook install/remove handles the Claude `Read` hook cleanly.

### Deferred for later phases

- Rich structure digests for blocked unchanged reads.
- Optional cache clearing on explicit compact / session-bound cwd reset signals.
- Additional long-run analytics/report exports if read-cache metrics should be broken out beyond admin/TUI.

---

## Implementation Plan

### WP1 - New package: `internal/readcache`

- Add a dedicated package instead of hiding this in `internal/filter`.
- Responsibilities:
  - parse read-hook payload
  - load/save session state
  - evaluate unchanged-read / delta-read decisions
  - sanitize session IDs and cache paths

Core types:

- `Request`
- `Decision`
- `SessionState`
- `FileEntry`

### WP2 - Internal CLI entry point

- Add hidden subcommand: `slimference readhook`
- stdin: hook JSON
- stdout: Claude `hookSpecificOutput` JSON only on block
- exit code:
  - `0` = allow or block payload emitted
  - `1` = invalid usage / malformed payload

Why:
- keeps shell hook scripts minimal
- reuses Go logic directly

### WP3 - Claude hook install wiring

- Install a second Claude hook script:
  - `~/.claude/hooks/slimference-read-cache.sh`
- Add a `PreToolUse` matcher for `Read`
- Keep existing Bash rewrite hook unchanged
- Remove both cleanly on uninstall

### WP4 - Delta strategy for Phase 1 delivery

First implementation must stay conservative:

- full-file reads only
- max cached content budget: bounded small files
- line-based added/removed summary
- block only if delta text is clearly shorter than the new file content
- otherwise fall back to allow

### WP5 - Tests

- unit: payload parsing, state persistence, unchanged read, changed read, delta
- hooks: install/remove includes `Read` matcher
- cmd: `slimference readhook` outputs valid Claude hook JSON on block

---

## Subtasks

- [x] Add `internal/readcache` package with state model and payload parsing.
- [x] Implement unchanged-read block logic.
- [x] Implement bounded full-file delta logic.
- [x] Add hidden `slimference readhook` subcommand.
- [x] Extend Claude hook install/remove for `Read`.
- [x] Add package and integration tests.
- [x] Run targeted Go tests for readcache, hooks, and cmd.
- [x] Update `docs/todo.md` status when the first production slice lands.

---

## Landed in this pass

- New package: `internal/readcache`
- New hidden CLI path: `slimference readhook`
- Claude install now writes:
  - `~/.claude/hooks/slimference-rewrite.sh`
  - `~/.claude/hooks/slimference-read-cache.sh`
- `InstallClaude` / `RemoveClaude` now wire and remove the `Read` matcher
- Repo verification:
  - `go test ./internal/readcache ./internal/hooks ./cmd/slimference`
  - `go test ./...`

## Remaining Follow-Ups

- none for the implemented T37 scope

---

## Acceptance Criteria

- [x] Repeated unchanged `Read` in the same session is blocked before the tool runs.
- [x] Small full-file re-reads can return a concise delta instead of a full read.
- [x] No unrelated Claude hooks are destroyed during install/remove.
- [x] Codex now has matching `Read` hook semantics and coherent install verification.
- [x] Read-cache state is flushable and visible through admin/TUI status surfaces.
- [x] Coverage for touched packages remains complete and `go test ./...` stays green.

## Completed in the final pass

- Added persisted read-cache metrics (`evaluations`, `allows`, `blocks`, `unchanged`, `delta`) in `internal/readcache/stats.go`.
- Added read-cache snapshot + flush integration to proxy admin status and `FlushCaches()`.
- Added TUI visibility for read-cache activity in dashboard and stats views.
- Added Codex `Read` hook support via `codex-read-tool.sh`, `hooks.json` `PreToolUse` matcher `Read`, and matching verify/install/remove logic.
- Extended `slimference readhook` to emit Claude or Codex hook payloads.
- Added regression coverage for stats, admin surfaces, remote adapter, Codex hook install/status, and CLI output modes.
