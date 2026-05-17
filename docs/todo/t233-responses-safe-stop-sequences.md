# TASK 233: Responses-safe stop-sequence injection

Status: DONE
Priority: P0 before resuming T209/T224 live traffic
Scope: `internal/outstop`, HTTP proxy wiring, WSS Phase-F request adapter

## Why

The first live scoped Codex CLI HTTP smoke reached the Slimference provider,
but upstream rejected the mutated request with `Unsupported parameter: stop`.

Root cause: `outstop.MergeIntoBody` treated `types.CodexChatGPT` like OpenAI
Chat Completions and injected top-level `stop`. Codex CLI 0.130 uses the
Responses API body shape (`input`, not `messages`) on both `/v1/responses` and
`/backend-api/codex/responses`. That shape rejects Chat-Completions-only stop
parameters.

This is a product-path regression introduced by routing Codex through the
scoped provider route. The route worked; one output-reduce mutator was too
broad.

## Target State

- Chat Completions requests with top-level `messages` keep stop-sequence
  injection.
- Responses API requests with top-level `input` are passed through by outstop.
- Bodies without a top-level `messages` array are passed through by outstop.
- HTTP and WSS both inherit the same guard through `outstop.MergeIntoBody`.
- Counters do not record stop injections for Responses-shaped Codex traffic.
- No config flag is added; the optimization becomes schema-aware rather than
  operator-managed.

## Acceptance

- Unit tests prove `OpenAI` and `CodexChatGPT` Responses-shaped bodies remain
  byte-equal and report `AddedCount=0`.
- Unit tests prove OpenAI/Codex Chat-Completions-shaped bodies still inject
  `stop`.
- HTTP wire tests prove `/v1/responses` and
  `/backend-api/codex/responses` do not forward `stop`.
- HTTP wire tests prove `/v1/chat/completions` still forwards `stop`.
- WSS Phase-F tests prove Responses-shaped request frames do not gain `stop`
  and do not increment the stop counter.
- `go test ./internal/outstop ./internal/proxy -count=1` passes.
- `go run ./scripts/ci` passes with aggregate coverage >= 99.5%.

## Sub-Tasks

- [x] Reproduce and classify the live failure as Responses API shape drift.
- [x] Add schema guard to `outstop.MergeIntoBody`.
- [x] Update outstop unit coverage for Responses skip and Chat Completions keep.
- [x] Add HTTP wire regressions for OpenAI Responses and Codex Responses.
- [x] Update WSS Phase-F regressions to require no `stop` on Responses frames.
- [x] Run focused and full verification.
- [x] Rebuild/install, restart daemon, and rerun live scoped HTTP smoke.

## Notes

- This task does not change transport selection. `auto` still falls back to
  HTTP until WSS certification lands.
- This task does not touch `~/.codex/config.toml`, `/etc/hosts`, pfctl,
  Keychain, env vars, or Claude Code.
- `beterse` already has a Codex Responses `input[system]` path and is default
  off. Other mutators are covered by a follow-up sweep only if focused tests or
  live smoke expose real incompatibility.
- Focused verification passed:
  `go test ./internal/outstop ./internal/proxy -run 'TestMergeIntoBody|TestOutstopWire|TestWSPhaseF|TestMITMConversation' -count=1`.
- Full verification passed: `go test ./... -count=1 -timeout 300s`,
  `go vet ./...`, and `go run ./scripts/ci`.
- Live scoped HTTP smoke passed:
  `slimference codex run --transport=http -- exec "Reply with exactly: PING"`
  returned `PING`, exit 0, with `stop_seq_injections` unchanged at 0.

## Deviations

- None.
