# T71 - Codex CLI Live E2E Certification

Status: todo
Priority: P0
Scope: `cmd/slimference/integrate_cmd.go`, `internal/integrate/`, `internal/hooks/`, `internal/proxy/`, `docs/integration.md`, local smoke procedure
Driver: User wants Slimference to work perfectly for Codex CLI, not merely pass unit tests.

---

## Problem

Repository tests prove pieces of Codex support, but the current live machine is
not fully wired:

- Codex binary: `/Users/christopher/.npm-global/bin/codex`.
- Version: `codex-cli 0.125.0`.
- `~/.codex/hooks.json` has PreToolUse and PostToolUse hooks.
- `~/.codex/hooks.json` does not currently show the Codex Read hook.
- `~/.codex/config.toml` has `codex_hooks = true`, but no Slimference
  `openai_base_url` / `chatgpt_base_url` block.
- `slimference integrate status --client codex --json` reports Codex
  `partially_wired` and daemon `unreachable`.

That means the local Codex CLI path is not yet certified end-to-end. It may have
Layer 0 hook benefits, but traffic is not proven to flow through the proxy.

## Target State

A repeatable certification flow proves:

1. `integrate status --client codex` reports the exact pre-state.
2. `integrate install --client codex` writes the intended config and hook state.
3. Slimference daemon/headless mode accepts traffic.
4. A real Codex CLI request reaches Slimference.
5. Slimference routes it to the correct upstream and Codex receives a normal response.
6. `bypass on` and `integrate emergency-off` both recover cleanly.

## Implementation Plan

### WP1 - Build a non-destructive certification harness
- Add a `scripts/utils` Go command or a `slimference test codex-e2e` subcommand.
- It must support `--dry-run` and a temp-home fixture mode.
- It must never mutate the user's real `~/.codex` unless explicitly requested.

### WP2 - Certify live state reporting
- Assert status distinguishes:
  - binary missing
  - hooks missing
  - pre/post hooks present but read hook missing
  - config not wired
  - daemon unreachable
  - fully wired
- Render the same facts in JSON and human output.

### WP3 - Certify the daemon/proxy path
- Start Slimference on a random local port in headless mode.
- Point a temp Codex config at that port.
- Use a stub upstream that mimics Codex response status/streaming shape.
- Verify request path, query, method, Authorization, User-Agent, and body are
  observed by Slimference before upstream.

### WP4 - Manual live smoke
- On the real machine, after explicit approval, wire actual Codex to
  `127.0.0.1:8990`.
- Run one tiny Codex interaction.
- Verify Slimference logs show Codex provider routing and the user gets a
  normal Codex answer.
- Immediately verify `bypass on`, `bypass off`, and `integrate emergency-off`.

### WP5 - Documentation
- Add a Codex certification checklist to `docs/integration.md`.
- Include exact rollback commands.

## Acceptance Criteria

- [ ] `slimference integrate status --client codex --json` detects every
      partial-wiring case accurately.
- [ ] Temp-home E2E test proves config + hooks + proxy + upstream flow without
      touching the real user home.
- [ ] Manual live smoke confirms Codex CLI traffic reaches Slimference and still
      receives a normal response.
- [ ] `slimference bypass on|off|status` works during the live smoke.
- [ ] `slimference integrate emergency-off --client codex` removes all Codex
      Slimference wiring and leaves Codex usable direct.
- [ ] `go test -race ./internal/integrate/... ./internal/proxy/... ./cmd/slimference/...` green.

## Out of Scope

- Changing Codex authentication.
- Capturing or storing real user prompts.
- Running live Codex smoke automatically in CI.

## Validation

```
go run ./cmd/slimference integrate status --client codex --json
go test -race ./internal/integrate/... ./internal/proxy/... ./cmd/slimference/...
slimference test codex-e2e --temp-home --stub-upstream
# manual only after approval:
slimference integrate install --client codex
codex
slimference bypass status
slimference integrate emergency-off --client codex
```
