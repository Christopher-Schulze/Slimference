# TASK 163: Codex newest hook surface + Desktop App verification

Status: TODO (planning 2026-05-15)
Priority: P0
Scope: `internal/hooks/codex.go`, `internal/integrate/codex_toml.go`, `internal/integrate/integrate.go`, `internal/proxy/provider.go`, `internal/proxy/handler.go`, `cmd/slimference/integrate_cmd.go`, `docs/documentation.md`

## Why

Two converging needs:

1. The latest Codex (CLI + Desktop App) exposes hook events we do not yet use (PreToolUse Write/Edit, PreToolUse ApplyPatch, PreCompact, Notification, plus richer SessionStart payloads). Every unused event is a missed compaction surface.
2. The OpenAI Codex Desktop App is built on the same engine as the CLI and is reported to share `~/.codex/config.toml` and `~/.codex/auth.json` with the CLI. If true, our existing `openai_base_url` patch wires both clients with zero extra code. This must be empirically verified, not assumed; an automated detection + status surface keeps us honest if OpenAI changes the contract.

**Why:** A single unified wiring across Codex CLI + Codex Desktop + Claude Code on top of one Slimference daemon is the cleanest architecture possible. The MITM/CA stack remains as a fallback for clients that ever drop config-based override support — we do not remove it.
**How to apply:** Hook surface gets every event the current Codex documents; integration status surfaces which clients are wired and via what mechanism (config / env / proxy fallback).

## Target State

1. **Hook surface expansion** in `internal/hooks/codex.go`:
   - PreToolUse matcher list extended beyond `Bash`/`Read` to include `Write`, `Edit`, `MultiEdit`, `ApplyPatch`, `Notebook*` where the current Codex schema accepts them.
   - PreCompact hook (if exposed by the current Codex schema): hand the conversation to Slimference's deterministic L1/L2 before Codex would run its own compaction.
   - Notification hook for TUI status mirroring.
   - SessionStart hook injects a one-line awareness preamble (deterministic output formats, archive markers) so the model trusts the compacted shape (RTK-style awareness ported).
   - PermissionRequest hook gains auto-approval policy lookup against the existing `filter.DeniedShellCommand` / `AskRequired` engine, not just logging.
2. **Desktop App verification**:
   - `slimference doctor desktop` detects the macOS app (`/Applications/Codex.app` or wherever the current build ships), reports version, and probes whether it honors `~/.codex/config.toml` by inspecting the running process's open file descriptors via `lsof -p <pid>` when the app is running.
   - `slimference integrate status` adds a row per client showing `wired-via: config-toml | env-var | proxy-mitm-fallback | not-wired`.
   - UA detection in `internal/proxy/provider.go` differentiates `codex-cli` vs. `codex-desktop` based on `User-Agent` header; analytics breaks out per-source.
   - If verification confirms shared config: nothing more to do — same wiring covers both.
   - If verification shows Desktop ignores config: fall back to system-proxy + CA-trust (existing `internal/tlsca/` + `internal/proxy/connect.go` infrastructure is reused, not rebuilt). New helper `internal/integrate/macos_systemproxy.go` for `networksetup` and `security` orchestration.
3. **Default install scope is Codex-only during the current testing phase**:
   - `slimference integrate install` (no client arg) wires **only Codex CLI** (config.toml + hooks.json). Codex Desktop is automatically covered if it shares the config (Outcome A).
   - Claude Code wiring exists in code (`integrate/shellrc.go`, `internal/hooks/claude.go`) but is **gated behind explicit opt-in**: `slimference integrate install claude --enable-experimental` or env `SLIMFERENCE_ENABLE_CLAUDE_WIRING=1`. Without that flag, `integrate install` skips Claude with a status note. This prevents accidental activation while Codex is the validation focus.
   - `slimference integrate install all` requires the same opt-in flag for Claude or it errors with a clear message pointing at the gating mechanism.
   - `slimference integrate status` lists Claude Code as `not-wired (gated, requires --enable-experimental)` so the user always sees the situation.
4. CA-stack stays untouched (`internal/tlsca/`, `internal/proxy/connect.go`). Documented as the fallback path; not the default for any supported client. Stays available for Outcome C without code changes.

## Acceptance

- Codex hook event surface registered in `hooks.json` covers every event the current Codex schema accepts; CHANGELOG documents the matrix.
- `slimference doctor desktop` runs without root, returns clear Outcome A/B/C verdict.
- `slimference integrate status` shows per-client wiring mechanism; Claude Code clearly marked `gated`.
- `slimference integrate install` (no args) wires Codex CLI only; Claude Code is skipped with explicit status note unless `--enable-experimental` is passed.
- Codex Desktop App, after `slimference integrate install` + verification, routes its traffic through the Slimference proxy in a live e2e test (manual smoke test, captured as integration test if reproducible).
- Analytics distinguish `codex-cli` vs. `codex-desktop` UA in `gain` reports.
- 100% coverage maintained on `internal/hooks/`, `internal/integrate/`.

## Sub-Tasks

- [ ] Codex hook schema audit: read the current Codex CLI source (the public OSS repo) or its installed `hooks.json` schema; produce a matrix of supported events vs. our coverage in this task's Notes.
- [ ] Gate Claude Code wiring: add `--enable-experimental` flag + `SLIMFERENCE_ENABLE_CLAUDE_WIRING` env in `cmd/slimference/integrate_cmd.go`; `InstallClaude` only runs when set.
- [ ] Extend `installCodexHooksJSONWithScripts` in `internal/hooks/codex.go` for the new event types; add new lifecycle scripts as needed.
- [ ] New hook script generators for Write/Edit/ApplyPatch/PreCompact/Notification.
- [ ] Awareness preamble: ship `~/.slimference/codex-awareness.md`, reference it from SessionStart hook output (Codex injects it as system context).
- [ ] UA-based source detection in `internal/proxy/provider.go`; add `Source` field to request log.
- [ ] `slimference doctor desktop` subcommand (`cmd/slimference/doctor_*.go` extension).
- [ ] `slimference integrate status` per-client mechanism reporting.
- [ ] `internal/integrate/macos_systemproxy.go` (Outcome C fallback) — kept dormant unless the doctor probe demands it.
- [ ] Live e2e smoke playbook in `docs/documentation.md`: how to verify CLI + Desktop both routed.
- [ ] Update `docs/documentation.md` "Wiring" section with the unified architecture diagram (1 daemon, 3 clients, 1 fallback CA stack).

## Notes

**Wiring matrix (target state):**

| Client          | Primary wiring                                | Mechanism                | Default install | Fallback        |
|-----------------|-----------------------------------------------|--------------------------|-----------------|-----------------|
| Codex CLI       | `~/.codex/config.toml` + `hooks.json`         | config + hooks patch     | **enabled**     | CA + sysproxy   |
| Codex Desktop   | shares `~/.codex/config.toml` (to verify)     | inherited from CLI patch | **enabled** (Outcome A) | CA + sysproxy |
| Claude Code     | `ANTHROPIC_BASE_URL` env + settings.json hooks| shellrc + settings patch | **gated**, opt-in via `--enable-experimental` | CA + sysproxy |

Current testing focus is Codex (CLI + Desktop). Claude Code wiring is feature-complete in code but kept dormant until Codex validation lands.

**CA fallback stack stays intact:** `internal/tlsca/{ca,signer,verify}.go`, `internal/proxy/connect.go`, allowlist in `internal/config/defaults.go`. Not removed, not refactored. Documented as Outcome-C fallback.

**Why we keep CA infra even when unused on the default path:**
- spec+.md §16.4 Provider-Invisibility path (utls activation later).
- Transparent-Mode use cases (system-wide intercept for opaque tools).
- Future GUI tools that ship without config-based base_url override.

## Deviations

(none yet)
