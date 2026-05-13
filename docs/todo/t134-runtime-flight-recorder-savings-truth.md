# TASK 134: Runtime flight recorder and savings truth

Status: DONE (local implementation 2026-05-13; live corpus proof remains T140/T118b)
Priority: P0
Scope: `internal/debug/`, `internal/proxy/`, `internal/hooks/`, `internal/analytics/`, `internal/tui/`, `cmd/slimference/debug*`, `cmd/slimference/gain*`, `scripts/benchmarks/`, `docs/live-corpus-policy.md`.

## Why

Transparent mode is only useful if the operator can prove what happened. We need one durable, replayable, privacy-aware flight recorder that answers:

- Did the request go through Slimference or bypass?
- Which host/path/provider was it?
- Was it CONNECT/MITM, direct config-patch, WebSocket tunnel, raw passthrough, hook, or local filter?
- Which layers ran?
- How many input tokens would have gone upstream, how many did go upstream, and how many were provider-cache hits?
- What output-token discipline was injected and did it reduce output later?
- Did any layer expand content, fail, or revert?

Existing debug/analytics systems exist, but they are split across filter DB, analytics JSONL, decision logs, admin status, TUI cards, and benchmark reports. This task makes the evidence unified and good enough for hard tuning.

## Target State

Every relevant request/tool event emits one normalized `FlightEvent` chain:

1. Ingress event: mode, host, route, provider, client hints, request id, session id, turn id.
2. Routing event: direct/proxy/CONNECT/MITM/raw/WebSocket/hook.
3. Extraction event: shape recognized, messages extracted, fallback reason.
4. Layer event per layer and sub-layer: before tokens, after tokens, saved tokens, elapsed, reason, fallback/revert.
5. Provider/cache event: upstream status, latency, usage, cached tokens, prompt cache read/create tokens, previous_response_id state usage.
6. Output event: output tokens, output-reduce injection metadata, model/tool failure hints.
7. Egress event: final status, total overhead, total net saving, confidence class.

## Work Packages

### WP1 - Schema

- Define `FlightEvent` and `FlightRequestSummary` under `internal/debug`.
- Keep current `RequestSummary` readable; migrate by adding fields, not breaking old JSONL.
- Required fields:
  - `schema_version`
  - `request_id`
  - `session_id`
  - `turn_id`
  - `source` (`proxy`, `transparent_connect`, `websocket`, `hook_pre`, `hook_post`, `filter`, `readhook`)
  - `provider`
  - `host`
  - `path`
  - `client_family` (`codex_cli`, `codex_app`, `claude_code`, `unknown`)
  - `route_mode`
  - `bypass_reason`
  - `layers`
  - `token_accounting`
  - `cache_accounting`
  - `output_reduce`
  - `errors`
  - `privacy_redaction_state`

### WP2 - Token accounting

- Separate estimates from provider-reported numbers:
  - `estimated_original_input_tokens`
  - `estimated_final_input_tokens`
  - `provider_input_tokens`
  - `provider_cached_tokens`
  - `provider_output_tokens`
  - `estimated_output_tokens`
  - `billable_savings_estimate`
  - `wire_savings_estimate`
- Never count `previous_response_id` as billable token saving unless provider usage proves lower billable input.
- Count OpenAI cached tokens from `usage.prompt_tokens_details.cached_tokens`.
- Count Anthropic cache usage from existing cache fields.

### WP3 - Transparent routing logging

- Log CONNECT decisions:
  - allowlisted MITM
  - raw passthrough
  - CA init failure
  - WebSocket tunnel
  - daemon-down detected by status
- Add counters for voice/audio bypass assumptions:
  - UDP/WebRTC is not visible to HTTP proxy.
  - TCP realtime/audio paths that hit HTTPS proxy are recorded as tunnel/passthrough and not compressed unless explicitly supported.

### WP4 - Hook logging

- `pretool`, `posttool`, `readhook`, `PermissionRequest`, `SessionStart`, `UserPromptSubmit`, and `Stop` should log:
  - hook event
  - matcher/tool
  - decision
  - compacted bytes/tokens
  - unsupported-output fallback
  - stderr/error
- Hook logs must not include secrets after redaction.

### WP5 - TUI and CLI views

- Add `slimference debug flight last|tail|replay|export`.
- Add TUI diagnostics view:
  - last N requests
  - savings by layer
  - bypasses by reason
  - provider cache hit rate
  - top expansion/revert cases
  - slowest steps
  - hook failures
- Add JSON and CSV exports for offline analysis.

### WP6 - Privacy and storage

- Redaction before disk write for secrets, auth headers, cookies, local home path, temp path.
- Configurable retention:
  - max days
  - max bytes
  - per-event body capture off by default
  - scrubbed corpus export opt-in
- Flight recorder must degrade gracefully when disk is full/unwritable.

### WP7 - Tests

- Golden JSONL schema tests.
- Backward-compatible replay of old `RequestSummary`.
- Unit tests for OpenAI cached-token parsing, Anthropic cache parsing, bypass accounting, layer revert accounting.
- TUI snapshot tests for diagnostics states.

## Acceptance

- [x] One replayable log answers route, layer, token, cache, hook, and error questions.
- [x] OpenAI `cached_tokens` and Anthropic cache usage are visible separately.
- [x] Estimated vs provider-reported token numbers are never mixed without labels.
- [x] TUI and CLI expose the same truth.
- [x] Privacy redaction is default-on.
- [x] `go run ./scripts/ci` passes.

## Notes

- Implemented `internal/debug/flight.go` with backward-compatible `RequestSummary.flight` generation. Old JSONL replays hydrate into the new flight schema.
- `Recorder.Record` now redacts before memory/disk persistence: bearer auth, API-key/token/password/cookie assignments, OpenAI-style `sk-*` keys, user home paths, and temp paths are scrubbed before `flight` is regenerated.
- Proxy request summaries now carry source/host/path/route fields, provider-reported input/cache/output numbers, output-reduce metadata, `previous_response_id` state, and normalized flight events.
- CONNECT-level routing decisions now emit flight records directly from `ConnectInterceptor`: raw passthrough, allowlisted MITM connect, leaf-cert failure, TLS handshake failure, and WebSocket tunnel upgrades.
- Hook entry points now emit local flight records when `[debug].decisions_log` is configured: `rewrite` as `hook_pre`, `posttool` as `hook_post`, and `readhook` as `readhook`.
- OpenAI/Codex cache usage is parsed from both `usage.prompt_tokens_details.cached_tokens` and `usage.input_tokens_details.cached_tokens`; Anthropic cache usage remains parsed from `cache_read_input_tokens` / `cache_creation_input_tokens`.
- CLI surface added: `slimference debug flight last|tail|replay|export`. `last|tail|replay` support `--json`; `export` writes JSONL by default and CSV with `--csv` or `.csv` output path.
- TUI debug view now has a `FLIGHT RECORDER` diagnostics block using the same normalized flight records as the CLI: recent route/source/layers, billable savings, provider cache, output tokens, bypass count, and slowest request.
- Body capture is still off by design: flight logs store accounting, routing, and error metadata, not raw request/response bodies. Scrubbed real-session corpus capture remains the operator-driven T140/T118b path.
- `go run ./scripts/ci` passed 8/8 with 100% coverage after implementation.

## Completion Evidence

- Code: `internal/debug/flight.go`, `internal/debug/redaction.go`, `internal/proxy/streaming.go`, `internal/proxy/connect.go`, `internal/proxy/handler.go`, `cmd/slimference/main.go`, `internal/tui/model.go`, `internal/tui/views.go`.
- Tests: `internal/debug/flight_test.go`, `internal/debug/redaction_test.go`, `internal/proxy/streaming_cache_usage_test.go`, `internal/proxy/connect_test.go`, `internal/proxy/proxy_unit_test.go`, `cmd/slimference/debug_cmd_test.go`, `cmd/slimference/main_test.go`, `internal/tui/model_test.go`.
- Local verification before marking done: focused `go test ./cmd/slimference ./internal/debug ./internal/proxy ./internal/tui`; full CI must be re-run after the next task batch before commit.
