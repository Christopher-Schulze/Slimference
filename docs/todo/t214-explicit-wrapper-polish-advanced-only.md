# TASK 214: Explicit wrapper polish, advanced only

Status: DONE (2026-05-17)
Priority: P2 after T213/T212 interfaces stabilize
Scope: `cmd/slimference filter|rewrite`, help/completion/docs, hook-internal wrapper behavior

## Why

`slimference filter -- <cmd>` is useful for humans, hook scripts, and fallback workflows. It is also the internal mechanism that Claude/RTK-style PreToolUse rewrite points at. But it must not become a third default integration surface. The product architecture stays two-surface: hooks for signal input, transparent MITM for traffic input.

The wrapper should be excellent, obvious, and safe as an advanced/manual tool without being promoted as the primary Codex routing path.

## Target State

- `slimference filter -- <cmd>` is documented as the command-output wrapper.
- `slimference rewrite -- <cmd>` is documented as the hook rewrite planner.
- Help text explains that hooks may call the wrapper internally.
- Default install does not add shell aliases, shell functions, env proxy variables, or base URLs.
- Optional alias/snippet is clearly advanced and copy/paste only.
- Filtered output is tagged internally so later proxy compression can avoid double-compaction.
- Exit code preservation and raw recovery are documented and tested.
- Wrapper UX covers streaming logs via `--stream`.

## Maximum-Possible Check

The wrapper cannot replace transparent MITM for Codex Desktop or hardcoded WSS traffic. This must be stated clearly in docs/help so future agents do not re-promote it as the universal path.

Evaluate:

- Can wrapper be made faster without new binary split?
- Does `slimference filter` add enough metadata for downstream proxy skip/dedup?
- Are common shell examples clear: `git status`, `go test`, `rg`, `cat`, `docker logs --follow`?
- Does completion include all relevant flags?
- Does help avoid suggesting legacy `proxy env/run` as the primary route?

## Acceptance

- Wrapper docs and help are clear, short, and advanced-only.
- No default command mutates shell rc files or creates aliases.
- Tests prove wrapper preserves child exit code and does not double-filter already filtered output.
- `rewrite` examples include compound-command behavior.
- Streaming mode remains opt-in and tested.

## Sub-Tasks

- [x] Audit current `filter`, `rewrite`, help, completion, and docs wording.
- [x] Add concise advanced wrapper examples to help.
- [x] Verify prefiltered-tag behavior across wrapper -> proxy path in `internal/compression/prefilter_tag.go` and tests.
- [x] Verify existing already-prefixed, streaming, and exit-code tests; add help regression for advanced-only wording and removed unimplemented flags.
- [x] Ensure no Phase H install/TUI flow promotes wrapper as a default surface.

## Verification

- `go test ./cmd/slimference ./internal/filter ./internal/compression -run 'Test.*Filter|Test.*Rewrite|Test.*Prefilter|Test.*Completion' -count=1 -timeout 120s`
- `go test ./docs -count=1`

## Notes

Wrapper polish is valuable, but not a replacement for Codex transparent MITM. It is the right abstraction for Claude hooks and manual diagnostics.

Implemented files: `cmd/slimference/help.go`, `cmd/slimference/completion.go`, `cmd/slimference/help_test.go`, docs.
