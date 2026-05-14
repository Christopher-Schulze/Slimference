# TASK 140: Codex CLI/App live E2E certification and real corpus

Status: PENDING (opened 2026-05-13)
Priority: P0
Scope: `scripts/verify/`, `scripts/benchmarks/`, `tests/fixtures/live_corpus/`, `cmd/slimference/proxy_cmd.go`, `cmd/slimference/watch_cmd.go`, `internal/proxy/`, `internal/transparent/`, `docs/live-corpus-policy.md`, `docs/transparent-mode.md`, `docs/savings-assessment.md`.

## Why

The repository is locally green, but the product is not certified until real Codex CLI/App traffic flows through transparent mode and the operator can disable it cleanly. Synthetic corpora prove code paths; they do not prove the current Codex App, CLI, Browser-Use, WebSocket mode, voice bypass, or real savings.

This task is deliberately last in Phase AA because it certifies the whole product path after T133-T139/T141.

## Target State

One manual-but-scripted certification run proves:

1. Fresh daemon/CA install.
2. Autostart enable/disable.
3. Proxy arm/disarm.
4. Codex CLI text turn flows through transparent mode.
5. Codex App text turn flows through transparent mode.
6. Codex App WebSocket/Responses path works.
7. Browser-Use to non-LLM HTTPS website is raw passthrough and not inspected.
8. Voice/microphone path remains unaffected.
9. Disable returns direct OpenAI traffic.
10. Uninstall removes CA trust, launchd, proxy settings.
11. Flight recorder and corpus export produce scrubbed evidence.
12. Token savings and cached-token telemetry are realistic and separated by layer/provider.

## Work Packages

### WP1 - Certification harness

- Add `scripts/verify` mode:
  - `transparent-e2e-plan`
  - `transparent-e2e-record`
  - `transparent-e2e-report`
- The harness should print exact manual steps and collect local evidence:
  - daemon status
  - proxy status
  - keychain CA state
  - networksetup state
  - admin status
  - flight log IDs
  - decision summaries
  - corpus export pointers.

### WP2 - Codex CLI proof

- Run one short Codex CLI task while proxy armed.
- Capture:
  - host/path/provider
  - CONNECT/MITM route
  - request extraction success
  - layers applied
  - upstream status
  - cached_tokens if any
  - output tokens
- Then run same or comparable prompt with proxy disarmed for baseline where feasible.

### WP3 - Codex App proof

- Run one Codex App text turn.
- Capture same route/layer/token proof.
- Confirm bundled native `codex` binary/app paths only for reporting, not mutation.

### WP4 - WebSocket proof

- Identify whether current Codex App uses WebSocket mode for responses.
- If yes:
  - prove WebSocket upgrade reaches `WebSocketTunnel`.
  - prove connection stays alive.
  - prove bytes are tunneled and no frame compression is attempted.
  - record whether future message-boundary compression is possible.
- If no:
  - record "not observed in this version" with app/CLI version.

### WP5 - Browser-Use passthrough proof

- Open a non-LLM HTTPS site through Browser-Use while proxy armed.
- Prove host is not allowlisted and uses raw TCP relay.
- Confirm no TLS MITM/cert substitution for that host.
- Confirm no compression/inspection.

### WP6 - Voice/microphone proof

- Use microphone transcription while proxy armed.
- Expected:
  - UDP/WebRTC bypasses system HTTPS proxy and is invisible to Slimference.
  - If TCP/TLS fallback occurs, it is tunneled/passthrough unless explicitly supported.
- Record observed proxy logs: absence of audio payload inspection is success.

### WP7 - Disable/uninstall proof

- `proxy disable` after active session.
- Confirm networksetup direct mode.
- Confirm Codex still works direct.
- `proxy uninstall`.
- Confirm CA removed from keychain, launchd removed, daemon stopped, proxy disabled.

### WP8 - Corpus and savings report

- Export scrubbed corpus from flight logs.
- Required categories:
  - Codex CLI HTTP.
  - Codex App HTTP.
  - Codex App WebSocket if observed.
  - Browser-Use passthrough metadata.
  - disable/uninstall events.
- `scripts/benchmarks benchmark-corpus --check` runs on the scrubbed corpus.
- `docs/savings-assessment.md` updated with real numbers, not synthetic-only claims.

## Acceptance

- [x] Codex CLI transparent traffic certified.
- [ ] Codex App transparent traffic certified.
- [ ] WebSocket behavior certified or explicitly not observed for the tested version.
- [ ] Browser-Use passthrough certified.
- [ ] Voice/microphone bypass certified.
- [ ] Disable/uninstall certified.
- [ ] Scrubbed live corpus committed or operator-approved local-only path documented.
- [ ] Real savings report separates input compression, output reduce, provider cached tokens, and state reuse.
- [ ] `go run ./scripts/ci` passes after any harness/docs updates.

## Notes

- This task is allowed to use the user's live Codex CLI/App only when the operator explicitly starts certification.
- Default CI must never call paid/live provider endpoints.
- 2026-05-13 partial CLI-only proof completed without arming macOS System-HTTPS-Proxy:
  - The first helper version printed a non-mutating per-process command using `openai_base_url="http://127.0.0.1:8990/backend-api/codex"` and `chatgpt_base_url="http://127.0.0.1:8990/backend-api/"`.
  - Live command returned `SLIMFERENCE_CLI_PROXY_OK`.
  - Flight evidence recorded `provider=codex_chatgpt`, `host=127.0.0.1:8990`, `path=/backend-api/codex/responses`, `route_mode=websocket_tunnel`.
  - `~/.codex/config.toml` remained unmodified; no `openai_base_url` or `chatgpt_base_url` lines were present after the test.
  - `slimference proxy status` showed every macOS Network service `off`, so Codex App remained direct for this mode.
- 2026-05-13 follow-up CLI-only proof completed with `[proxy] direct_codex_websocket_policy = "force_https_fallback"`:
  - Current Codex CLI retried the local WebSocket, then fell back to HTTP.
  - Slimference decoded Codex's zstd request body, ran the normal HTTP pipeline, re-encoded zstd upstream, and the live command returned `slimference-cli-zstd-fixed`.
  - A live tool-loop command returned `slimference-cli-tool-output-ok` after the shell tool output was sent back through the fallback HTTP path.
  - Flight evidence for the final tool-loop showed `route_mode=upstream`, `provider=codex_chatgpt`, output-reduce skipped with `reason=exact_reply`, and no negative input-token overhead.
- 2026-05-13 final CLI-only proof switched the preferred helper to a custom Codex provider:
  - `slimference proxy env codex --proxied` now prints `model_provider="slimference-codex"` plus `[model_providers.slimference-codex]` overrides for `base_url="http://127.0.0.1:8990/backend-api/codex"`, `requires_openai_auth=true`, `supports_websockets=false`, and `wire_api="responses"`.
  - Live command returned `slimference-custom-provider-ok` without WebSocket retry/fallback noise.
  - Flight evidence showed one direct HTTP `route_mode=upstream` request on `/backend-api/codex/responses`; the prior `websocket_force_https_fallback` records are now legacy evidence only.
- This proof certifies CLI-only routing, WebSocket continuity, and the zstd HTTP pipeline for current Codex CLI. It does not certify token savings on Codex WebSocket traffic because current `WebSocketTunnel` is byte-for-byte by design; message-boundary compression is now tracked explicitly as T142 and remains blocked on live frame-shape capture before any mutation mode.
- 2026-05-14 CLI-only probe re-run against Codex CLI `0.130.0` with the current repo daemon:
  - macOS System HTTPS proxy stayed disarmed for all active services, so Codex App remained direct.
  - `codex exec` used the per-process `slimference-codex` provider override and ran a shell tool loop successfully, returning `SLIMFERENCE_CLI_ACCOUNTING_OK`.
  - Flight evidence showed two `/backend-api/codex/responses` requests with `route_mode=upstream`, `provider=codex_chatgpt`, and `confidence=provider_reported`.
  - Responses-API nested `response.usage` is now parsed for Codex/OpenAI accounting. The final probe recorded provider-reported input/cache/output tokens in the flight log instead of local-only estimates.
  - The tiny probe correctly skipped output-reduce and compression-heavy layers as below threshold; this certifies routing/accounting, not savings magnitude.
- 2026-05-14 larger CLI-only read-only probe completed through the same `slimference-codex` per-process provider path:
  - The nested Codex CLI ran a read-only repository audit using shell tools (`wc`, `sed`, `rg`, `nl`) against T140 docs and the proxy/streaming tests. No repo files were modified by the nested probe.
  - Nested Codex reported `860138` input tokens, `716544` cached input tokens, `4974` output tokens, and `1220` reasoning output tokens for the whole probe.
  - Flight evidence showed `/backend-api/codex/responses` requests with `route_mode=upstream`, `provider=codex_chatgpt`, `host=127.0.0.1:8990`, and provider-reported cache reads growing from `7552` to `121216` cached tokens across turns.
  - The probe exposed two product gaps now fixed/tracked in code: output-reduce classified explicit read-only audit prompts as `new_file_generation` because file/edit keywords appeared in the request, and the main savings command only showed Layer-0 `filter.db` rows while daemon proxy flights lived in the decision log.
  - Output-reduce task-shape detection now has an explicit `read_only_analysis` shape that wins over edit/new-file keywords only when the prompt says read-only/do-not-edit/inspect/analyze/audit/report.
  - `slimference gain --proxy` and `slimference savings` now aggregate decision-log flight records for real proxied provider requests and report provider input/cache/output tokens, input savings estimate, output-reduce overhead, and cache-read billing-equivalent credit.
  - The first implementation over-counted local hook/readhook bookkeeping flights. The corrected provider-only filter ignores local/unknown providers and non-proxy sources. After the final installed-binary smoke, the local 2026-05-14 decision log reported `16` provider proxy requests, `14` provider-reported requests, `986501` provider input tokens, `769792` provider cached tokens, `5135` provider output tokens, `-1053` billable input savings estimate, `1053` output-reduce input overhead tokens, `692812` cache-read billing-equivalent tokens, and `691759` net billing-equivalent estimate.
  - The negative input-savings value is expected for this probe: output-reduce added directive overhead while deterministic input compression did not have enough compressible content. The real win observed here is provider-reported cache reuse, not local input shrink.
  - `gain --cache` and `gain --output` can still be zero when only decision-log flight recording is configured; for Codex CLI/App proxy proof, `gain --proxy` is the focused evidence command and `savings` is the unified operator view.
- 2026-05-14 final installed-binary smoke:
  - Rebuilt `/Users/christopher/.local/bin/slimference`, restarted the local daemon, and verified it running on PID `35331`, port `8990`.
  - Ran a new Codex CLI exact-reply smoke through the per-process `slimference-codex` provider after the daemon restart. Codex returned `SLIMFERENCE_PROXY_FINAL_OK`.
  - Flight evidence for request `ca67c0f3c12f43fc` showed `route_mode=upstream`, `provider=codex_chatgpt`, `path=/backend-api/codex/responses`, `confidence=provider_reported`, `provider_input_tokens=32459`, `provider_cached_tokens=7552`, `provider_output_tokens=24`, and `output_reduce.reason=below_min_tokens`.
  - `slimference proxy status` still showed every macOS network service `off`, proving this smoke exercised CLI-only routing without arming Codex App/system traffic.
