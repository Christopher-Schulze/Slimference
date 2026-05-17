# TASK 216: Claude toggle UX truth

Status: DONE, superseded by stricter T217 park lock (2026-05-17)
Priority: P1 polish before exposing Claude as first-class
Scope: `internal/tui/view_apps.go`, `internal/tui/model.go`, `/admin/state` app entries if needed, install docs

## Why

The apps policy has a `claude_code` entry, but Phase H default hosts are
Codex-only and T217 parks every public Claude activation path. Therefore
toggling Claude Code in the TUI must be blocked rather than suggesting
Anthropic traffic can route through Slimference.

The current product target is Codex CLI first and Codex Desktop next. Claude
support remains in the codebase, but is not a Slimference product surface while
RTK handles Claude Code.

## Target State

- Claude Code appears as parked in the TUI.
- If `claude_code` policy can be toggled but routing prerequisites are missing, the UI says so.
- The user cannot confuse "policy bit enabled" with "Anthropic traffic is routed".
- A future Claude product path would require a new explicit decision and a new
  task; no `root-arm --with-claude` exists now.
- Codex CLI and Codex Desktop toggles remain fully functional.

## Maximum-Possible Check

Determine which model is best:

Option A: Disable Claude row in Apps view until a Claude hosts-arm path exists.

Option B: Allow toggling the policy bit but render "inactive: host not armed" and keep telemetry zero.

Option C: add a future first-class Claude product path. Deferred by T217.

For the current Codex-first phase, A or B is acceptable. C is out of scope until the user explicitly includes Claude live traffic.

## Acceptance

- TUI no longer suggests Claude can be routed by policy alone.
- Tests prove pressing Claude toggle does not silently imply traffic routing when hosts are Codex-only.
- `slimference status` app section, if it shows Claude, includes opt-in/inactive wording.
- Docs state: Claude Code is retained, default-off, and not live-routed in Codex-first certification.
- No `api.anthropic.com` is added to default hosts.

## Sub-Tasks

- [x] Choose option A/B hybrid: show Claude row, but park it as `HOSTS OFF` and prevent policy toggling while hosts remain Codex-only.
- [x] Update Apps view row rendering for `claude_code`.
- [x] Update keyboard/toggle behavior and flash message.
- [x] Add tests for Claude row truthfulness and parked toggle behavior.
- [x] Update docs/install.md and docs/documentation.md wording.

## Verification

- `go test ./internal/tui -run 'Test.*Apps|Test.*Claude' -count=1 -timeout 120s`
- `go test ./cmd/slimference -run 'Test.*Status|Test.*Apps' -count=1 -timeout 120s`
- `go test ./docs -count=1`

## Notes

This task does not delete Claude code. It prevents a false promise in the UI while preserving the future max-mode path from T212.

Implemented files: `internal/tui/view_apps.go`, `internal/tui/model.go`, `internal/tui/view_apps_test.go`, docs.
