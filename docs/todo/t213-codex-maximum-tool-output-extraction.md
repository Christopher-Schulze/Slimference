# TASK 213: Codex maximum tool-output extraction

Status: DONE (2026-05-17)
Priority: P0 before or alongside T209 where no live arm is required
Scope: `internal/proxy`, `internal/hooks/codex*`, `internal/filter`, WSS fixtures, `/admin/state`, `gain/savings` telemetry

## Why

Codex currently cannot rely on transparent `updatedInput` command rewrite. The verified hook contract parses `updatedInput` but does not honor it, so the RTK/Claude trick cannot be the main Codex path. To maximize Codex savings anyway, Slimference must squeeze tool-output compaction from every proxy-visible shape: HTTP Responses bodies, ChatGPT backend bodies, WSS conversation frames, function-call outputs, shell envelopes, read-file outputs, and future Codex tool variants.

The goal is not another integration surface. The goal is to make the existing two surfaces brutal:

- Hooks provide lifecycle/precision signals where Codex actually emits them.
- Transparent MITM/proxy path mutates request/response bodies and WSS frames when the schema is known.

## Target State

Codex maximum extraction is measurable, not just implemented:

- Every known Codex tool-output envelope shape is parsed into a command/read context.
- Tool output is compacted with the same Layer-0 filters used by `slimference filter`.
- The original envelope metadata is preserved.
- Unknown shapes are byte-equal fail-open.
- WSS request frames and HTTP Responses bodies share equivalent logic where possible.
- `/admin/state`, `gain`, or `savings` reports adoption:
  - total Codex tool outputs observed
  - compacted outputs
  - skipped outputs by reason
  - malformed / unknown shape count
  - estimated tokens saved by Layer 0 on Codex
- The live T209 test can answer whether Codex CLI actually produced compacted frames/requests.

## Maximum-Possible Check

Audit every Codex shape already known in tests and docs:

- `function_call` + `function_call_output`
- `local_shell_call` + `local_shell_call_output`
- `shell_call_output`
- `tool_result`, `tool_output`, `mcp_call_output`
- `computer_call_output` must be explicitly bypassed unless proven text-safe.
- Raw shell envelopes with `Chunk ID`, wall time, exit code, and `Output:`.
- Command fields: `command`, `cmd`, `command_line`, `cmdline`, `shell_command`, `bash_command`, `run_command`, `argv`, `args`.
- Read fields: `path`, `file_path`, `filepath`, `absolute_path`.
- WSS envelope placements: `body`, `request`, top-level request body, raw envelope.
- Codex Desktop sideband paths: verify bypass remains enforced for voice/images/plugins/memories/auth.

## Acceptance

- No known Codex tool-output fixture bypasses Layer-0 without a documented skip reason.
- Bypass reasons are surfaced in tests and telemetry.
- Any tool class that can be unsafe to mutate, such as computer-use, binary/image/audio, is classified and bypassed.
- Layer-0 compaction preserves all non-output metadata fields.
- WSS and HTTP paths do not diverge silently; shared helpers or shared tests prove equivalent behavior.
- `go run ./scripts/ci` remains green before any live T209 arm.

## Sub-Tasks

- [x] Inventory current Codex HTTP and WSS fixture shapes covered by `provider.go`, `layer0_proxy.go`, and `wsmitm_phasef.go`.
- [x] Add Codex proxy Layer-0 adoption counters: `proxy_layer0_requests_modified` and `proxy_layer0_tokens_saved`.
- [x] Extend parser coverage for `parameters`, top-level `content`, nested `result`, and `tool_response` output fields.
- [x] Keep unsafe/non-text sideband output fail-open through existing route/path bypasses and unknown-shape no-op behavior.
- [x] Wire Layer-0 compaction into WSS request frames via `applyProxyLayer0WithSession`.
- [x] Surface proxy Layer-0 saved tokens through `SavingsProbe`.
- [x] Add HTTP/WSS/parser tests for Codex tool-output compaction and preservation.
- [x] Prepare T209 assertions: before live prompt counters zero; after prompt expect `wss.frames_reencoded>0` and/or `proxy_layer0_requests_modified>0`, with `degraded_sessions=0` and `parse_failures=0`.

## Verification

- `go test ./internal/proxy -run 'TestServeHTTP_Codex|TestProxyLayer0|TestWSPhaseF|TestMITM|TestCodexDesktop' -count=1 -timeout 120s`
- `go test ./internal/filter ./internal/hooks -run 'TestRewrite|TestCodex' -count=1 -timeout 120s`
- `go run ./scripts/ci` — PASS, all 8 steps, including 100.0 % productive Go coverage, codex smoke gate, live corpus synthetic gate, and leaf audit gate.

## Notes

This task does not live-arm Codex. It should make T209 more informative by ensuring the live run reports exactly which Codex outputs were compacted and which were safely bypassed.

Implemented files: `internal/proxy/outstop_counters.go`, `internal/proxy/savings_probe.go`, `internal/proxy/handler.go`, `internal/proxy/wsmitm_phasef.go`, `internal/proxy/provider.go`, tests.

Hardening discovered during final CI closure: the Layer-2 incremental branch had an absolute-vs-relative anchor-index bug. Delta slices were checked with absolute anchor indices, so the "all new messages are anchored" fail-open path could be missed. Fixed with range-aware anchor filtering in `internal/summarization/anchor.go` and `internal/summarization/layer2.go`, covered by regression tests.
