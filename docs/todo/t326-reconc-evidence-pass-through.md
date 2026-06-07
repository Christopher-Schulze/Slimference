# T326 Reconc Evidence Pass-Through

## Why

Fresh scoped Codex sessions against `/Users/christopher/CODE/Golem` repeatedly
hit upstream `invalid_request_error` while inspecting Reconc state. T325 proved
that Reconc itself was not the direct root cause, but Reconc is a policy and
workflow evidence tool: its command output is already concise, user-facing, and
diagnostic. Mutating it through generic Layer-0 reducers creates compatibility
risk for little token upside.

## Acceptance

- Reconc command output passes through unchanged before Layer-0 policy
  selection, token counting, repeated-output collapse, captured-output
  compaction, and chunk dedup.
- The guard recognizes direct `reconc`, packaged `reconc-*` dist binaries,
  shell-wrapped commands, leading `cd <repo> && ...` commands, and
  `go run ./cmd/reconc ...` development invocations.
- The guard applies to all Codex Layer-0 routes, not only WSS.
- Non-Reconc commands that merely mention the word `reconc` remain eligible for
  normal reducers.
- Focused proxy regressions, full tests, CI, and rebuilt installed binary pass.

## Sub-Tasks

- [x] Add a Reconc command recognizer to the proxy Layer-0 reducer.
- [x] Short-circuit Reconc command outputs before any model-facing mutation.
- [x] Add route-wide regression coverage for direct, dist, shell, and `go run`
  Reconc invocations.
- [x] Update docs and task SSOT.
- [x] Run verification gates and install the new build.

## Notes

- Product tradeoff: Reconc outputs are treated as evidence/control-plane text,
  not a savings surface. This sacrifices small possible repeated-output or
  chunk savings to avoid corrupting policy, audit, hook, and workflow evidence.
- The existing T325 WSS search/path-list guard still covers commands such as
  `find .reconc ...`; this task covers actual Reconc command invocations.

## Deviations

- None.
