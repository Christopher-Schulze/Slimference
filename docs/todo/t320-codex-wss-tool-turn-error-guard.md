# T320 Codex WSS Tool-Turn Error Guard

## Why

A scoped Codex CLI run through Slimference surfaced an OpenAI WSS error frame:
`status=400`, `type=invalid_request_error`, `message=Invalid request`. The
session was confirmed to be routed through the `slimference-codex` provider and
the Codex WSS `/backend-api/codex/responses` route. The persisted Codex session
file did not contain the error, and Slimference only had request summaries before
the failing server frame, so the failure was hard to diagnose from local logs.

The risky shape was a WSS continuation/tool-output turn where Codex can carry
tool results as `response_item.payload.function_call_output`, not just as a
top-level `function_call_output` item. Layer 0 can safely compact these tool
outputs, but output-reduce instructions must not be injected into these turns:
that changes the request envelope around a tool-result continuation and can
produce an upstream invalid-request response.

## Acceptance

- WSS output-reduce must skip every normalized tool-result request shape,
  including `response_item.payload.function_call_output`.
- Layer 0 tool-output compaction remains available on those turns.
- WSS upstream `error`, `response.failed`, and `response.incomplete` frames stay
  byte-equal and produce content-free debug summaries with status/type/message.
- Proxy and CLI tests must not write synthetic failures or debug entries into
  the user's real `~/.slimference` state.
- Documentation must state the WSS output-reduce boundary precisely.
- Targeted and package tests must pass.

## Sub-Tasks

- [x] Verify the live Golem Codex process was actually routed through
  Slimference and not a direct Codex session.
- [x] Identify the unsafe output-reduce injection window on WSS tool-output /
  continuation turns.
- [x] Replace top-level-only `function_call_output` detection with normalized
  tool-result detection.
- [x] Add a regression test for nested
  `response_item.payload.function_call_output` turns.
- [x] Add content-free WSS upstream-error summaries for future diagnostics.
- [x] Add a regression test for `status=400 invalid_request_error` recording.
- [x] Isolate `cmd/slimference` and `internal/proxy` tests from the user's real
  home, XDG paths, logs, analytics, and debug decisions.
- [x] Update documentation and TODO surfaces.
- [x] Run focused WSS/CLI tests and full `cmd/slimference` plus
  `internal/proxy` package tests.

## Notes

- Root cause: output-reduce only skipped top-level `function_call_output`.
  Current Codex WSS also sends tool results wrapped under
  `response_item.payload.function_call_output`. That allowed an output-reduce
  directive to be considered on a tool-result continuation turn.
- The fix does not weaken deterministic input savings: Layer 0 still handles
  read/search/git/test/tool-output reducers. Only output-reduce directive
  injection is blocked on tool-result turns.
- The visible old raw shell launcher text came from an already-running/older TUI
  instance or already-open terminal command. The installed source already clears
  the terminal and prints the short `[SF]` preamble before exec.
- Historical test pollution found in `~/.slimference/logs/slimference.jsonl`
  came from tests writing to the real home. After the isolation fix, a fresh
  package test run left the real log and decisions files unchanged.
- Verification completed: focused WSS regression tests, focused config
  subprocess regression test, `go test ./cmd/slimference -count=1`,
  `go test ./internal/proxy -count=1`, `go test ./...`,
  `git diff --check`, `go run ./scripts/ci`, `go run ./scripts/build -restart
  -version 0.9.1`, and forced Codex WSS recertification.

## Deviations

- None.
