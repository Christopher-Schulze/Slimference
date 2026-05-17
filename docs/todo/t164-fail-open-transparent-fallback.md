# TASK 164: Fail-open transparent fallback (daemon down must not kill coding tools)

Status: TODO (planning 2026-05-15)
Priority: P0
Scope: `internal/proxy/`, `internal/daemon/`, `internal/hooks/`, `cmd/slimference/`, `cmd/slimference-sidecar/` (new), launchd plist generator, systemd unit generator, `docs/documentation.md`

## Why

The unified wiring across Claude Code, Codex CLI, and Codex Desktop App routes every API request through `127.0.0.1:8990`. If the Slimference daemon is down (crash, OOM-kill, version upgrade, manual stop, disk full), every coding tool the user has loses its API connection at once. That is a catastrophic failure mode: instead of "no compaction this session" we deliver "no coding at all".

The contract must be: **with Slimference running, you save tokens; with Slimference down, you continue normally, just without savings**. Never the third option.

**Why:** A token-compaction tool that takes down the user's whole workflow when it crashes is worse than no tool. Trust survives only if the failure mode is invisible.
**How to apply:** Every component on the user-traffic hot path (HTTP listener, hook scripts) must have an explicit fail-open code path that has been tested under the failure condition it claims to handle.

## Target State

1. **Resilient sidecar listener** on `127.0.0.1:8990`:
   - Small dedicated binary `slimference-sidecar` (~200 LOC, zero compaction logic) holds the TCP socket. Pure reverse-proxy.
   - On every request, calls Slimference engine over Unix socket (`~/.slimference/run/hook.sock`) with a hard timeout (50 ms for hot path).
   - If engine responds: forwards engine's mutated request upstream and streams response back.
   - If engine timeout / connection refused / 5xx: forwards original unmodified request to the real upstream (`api.anthropic.com` for Anthropic-shape, `chatgpt.com` for OpenAI/Codex-shape), determined by Host/path heuristics already in `internal/proxy/provider.go`.
   - In both modes the Authorization header passes through unchanged.
   - Sidecar never imports `internal/compression`, `internal/summarization`, `internal/tui`, `internal/sessions` — keeps its dependency graph and crash surface minimal.
2. **Socket activation** (OS-level resilience):
   - macOS: launchd plist gains `<key>Sockets</key>` with port 8990 → OS holds the listener, spawns `slimference-sidecar` on first connection. If sidecar crashes, OS respawns on next connection.
   - Linux: ship `slimference-sidecar.socket` + `slimference-sidecar.service` systemd units alongside the existing daemon unit; `Type=notify` for healthy startup signaling.
   - Windows: skip socket activation; rely on auto-start service. Document.
3. **Hook-script fail-open everywhere**:
   - Every hook script in `internal/hooks/codex.go` and `internal/hooks/claude.go` wraps the Slimference call in a `timeout 30ms` guard with a passthrough `cat` fallback. Stdin always reaches stdout when the call fails or times out.
   - Extend the existing `failOpenCodexPostTool` pattern (`cmd/slimference/main.go:1088`) to every hook subcommand: `handleRewriteCmd`, `handleReadHookCmd`, `handleCodexHookCmd` lifecycle events. Helper `failOpenPassthrough(payload []byte, reason string)` in `cmd/slimference/failopen.go`.
   - Telemetry: each fail-open path records a `fail_open=1 reason=<x>` row so analytics can show degraded sessions.
4. **Engine-health probe**:
   - Sidecar polls Unix socket every 5 s with `{"op":"ping"}`; caches `engine_healthy bool` in an `atomic.Bool`. Skip engine when unhealthy; reattempt next tick.
   - Engine reports own health via `slimference status --json` returning `{healthy:true|false, last_error, version, uptime_seconds}`.
5. **Status surfacing**:
   - `slimference status` exit codes: `0` engine healthy, `1` sidecar up but engine down (degraded), `2` everything down.
   - TUI tray indicator: green / yellow (degraded passthrough) / red (sidecar dead). Existing `tui.HookStatus` extends with `EngineHealthy bool, SidecarHealthy bool`.
6. **Upgrade safety**:
   - During `slimference upgrade` (planned): stop engine, leave sidecar running → users keep coding through the upgrade window; engine reattaches when ready.
7. **Test surface** (this is non-negotiable):
   - Integration test `tests/integration/fail_open_test.go`: spin sidecar without engine, send Claude-shape and Codex-shape requests, assert they reach upstream unchanged.
   - Chaos test `tests/integration/engine_kill_test.go`: kill engine mid-stream, assert the active streaming response completes and the next request degrades cleanly.
   - Hook test matrix: every hook script under simulated `timeout` returns its stdin to stdout with exit 0.

## Acceptance

- Killing the daemon (`pkill slimference`) mid-session does not interrupt an active Claude Code or Codex CLI session; the next request degrades to passthrough.
- After the daemon is killed, new sessions start successfully with `wired-via: degraded-passthrough` reported in `slimference status`.
- Restarting the daemon recovers compaction without restarting any client.
- All hook scripts under `timeout 30ms` with daemon down return stdin to stdout, exit 0; tools never see broken hooks.
- launchd plist (macOS) and systemd unit (Linux) provide socket activation; OS, not Slimference, owns the listener.
- 100% statement coverage on `cmd/slimference-sidecar/` and the new fail-open helpers.
- Documented in `docs/documentation.md` with explicit failure-mode matrix.

## Sub-Tasks

- [ ] Wire schema between sidecar and engine in `internal/daemon/hookproto/` (shared with t156).
- [ ] New binary `cmd/slimference-sidecar/main.go`, ~200 LOC, dependency-light.
- [ ] Sidecar reverse-proxy with `httputil.ReverseProxy`, health-probe ticker, atomic-bool gating.
- [ ] Upstream-selection helper extracted from `internal/proxy/provider.go` into `internal/proxy/upstream/` so sidecar can use without pulling the full proxy package.
- [ ] launchd plist generator gains `Sockets` key; tests in `internal/daemon/launchd_test.go` extended.
- [ ] systemd `slimference-sidecar.{socket,service}` units + tests.
- [ ] Refactor every hook script template to wrap the call in `timeout 30ms ... || passthrough`.
- [ ] `cmd/slimference/failopen.go` helper + tests; wire into every hook handler (`handleRewriteCmd`, `handleReadHookCmd`, `handleCodexHookCmd`, `handlePostToolCmd`).
- [ ] `slimference status --json` engine-health response.
- [ ] TUI status indicator extended with sidecar/engine separation.
- [ ] Integration tests: `fail_open_test.go` and `engine_kill_test.go` under `//go:build integration`.
- [ ] Document failure-mode matrix in `docs/documentation.md`:

      | State                                | Listener :8990 | Compaction | Hooks         |
      |--------------------------------------|---------------:|-----------:|--------------:|
      | Healthy                              | sidecar+engine | yes        | applied       |
      | Engine down, sidecar up              | sidecar        | passthrough| passthrough   |
      | Sidecar down                         | OS respawns it | passthrough| passthrough   |
      | Sidecar + engine down, no OS support | not bound      | n/a        | hook fail-open|

- [ ] Watchdog smoke: `slimference doctor failover` runs the chaos matrix locally and reports green/yellow/red.

## Notes

- The sidecar is the trust layer for the wiring promise. Treat its codebase like a security boundary: minimal dependencies, no panics, defensive logging, frozen API.
- Existing CA stack (`internal/tlsca/`, `internal/proxy/connect.go`) is unrelated to this task; it serves the CONNECT-MITM fallback for tools that lack config-based override. Documented separately in t163.
- For the streaming path (`/v1/messages` SSE, `/v1/responses` SSE), passthrough must preserve `Transfer-Encoding: chunked` and not buffer. Reuse `internal/proxy/streaming.go` semantics where possible without pulling the heavy filter pipeline.
- Engine-down mode disables Layer 1/2/3/4 entirely — explicit and documented, not a silent partial.
- Current validation focus is Codex (CLI + Desktop). Claude Code wiring is gated per t163, so fail-open testing in this task targets Codex flows first; Claude flows tested only behind the same opt-in flag, but the sidecar logic is client-agnostic by design.
- Upstream selection helper must handle both `chatgpt.com/backend-api/codex/*` (Codex shape) and `api.openai.com/v1/responses` (raw OpenAI shape). Detection lifted out of `internal/proxy/provider.go` so the sidecar does not import the full proxy package.

## Deviations

(none yet)
