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
- 2026-05-14 fresh CLI-only certification smoke after the session-store/turn-state fixes:
  - Worktree started clean at commit `131656e`; installed Codex CLI was `0.130.0` at `/Users/christopher/.npm-global/bin/codex`.
  - `slimference proxy status` showed the CA trusted, transparent runtime enabled, daemon running on PID `66484` port `8990`, and all nine macOS Network services still `off`. This preserves the split-test invariant: Codex App remains direct while this CLI process is routed through Slimference.
  - The smoke used only process-local `codex exec` config overrides from `slimference proxy env codex --proxied`: `model_provider="slimference-codex"`, `base_url="http://127.0.0.1:8990/backend-api/codex"`, `requires_openai_auth=true`, `supports_websockets=false`, and `wire_api="responses"`. No persistent `~/.codex/config.toml` mutation was used.
  - Live command returned exactly `SLIMFERENCE_T140_FRESH_OK` with no tool calls.
  - Flight evidence for request `341e3ac951300784` showed `source=proxy`, `provider=codex_chatgpt`, `host=127.0.0.1:8990`, `path=/backend-api/codex/responses`, `route_mode=upstream`, `confidence=provider_reported`, `provider_input_tokens=32431`, `provider_cached_tokens=7552`, `provider_output_tokens=12`, and `output_reduce.reason=below_min_tokens`.
  - Planner evidence was correct for a tiny exact-reply prompt: L0 bypassed as `small_request`, L1 ran cheap-only, L2 bypassed below ROI threshold, L3 bypassed because the local prefix was too small, L4 bypassed because output was too small, and WebSocket mutation bypassed because this route was not WebSocket.
  - `slimference gain --proxy` and `slimference savings` then reported `17` provider proxy requests for the day, `15` provider-reported requests, `1.0M` provider input tokens, `777.3K` provider cached tokens, `5.1K` provider output tokens, `699.6K` cache-read discount equivalent, and `698.6K` net billable-equivalent estimate. This remains accounting proof, not a local-compression percentage claim.
  - CLI-only routing/accounting is now certified for current Codex CLI. Remaining T140 scope is App/system-proxy E2E, Browser-Use passthrough, voice/WebRTC bypass, disable/uninstall proof, scrubbed corpus capture, and live WebSocket frame-shape evidence.
- 2026-05-14 post-install smoke after T138 turn-key convergence:
  - Rebuilt and installed `/Users/christopher/.local/bin/slimference`, restarted the daemon, and verified PID `93769` on port `8990`.
  - Live Codex CLI command returned exactly `SLIMFERENCE_T138_TURN_KEYS_OK` through the same process-local `slimference-codex` provider, with all macOS Network services still `off`.
  - Flight `63e141f7330a2fdb` showed `source=proxy`, `provider=codex_chatgpt`, `host=127.0.0.1:8990`, `path=/backend-api/codex/responses`, `route_mode=upstream`, `confidence=provider_reported`, `provider_input_tokens=32450`, `provider_cached_tokens=7552`, `provider_output_tokens=26`, and `output_reduce.reason=below_min_tokens`.
  - `slimference gain --proxy` then reported `18` provider proxy requests for the day, `16` provider-reported requests, `1.1M` provider input tokens, `784.9K` provider cached tokens, `5.2K` provider output tokens, and `705.4K` net billable-equivalent estimate.
- 2026-05-14 Codex CLI `exec` hook-delivery reality check:
  - Rebuilt and installed `/Users/christopher/.local/bin/slimference`, then re-ran `slimference hook install codex` against Codex CLI `0.130.0`.
  - The installer migrated the managed deprecated `[features] codex_hooks = true` line to `[features] hooks = true`; `codex features list` reports `hooks` as stable and enabled, while `codex_hooks` is no longer listed and current `codex exec` warns that `codex_hooks` is deprecated.
  - `~/.codex/hooks.json` is now written in the current nested `{ "hooks": { ... } }` shape; a temporary backup/restore probe also tested the old top-level event-key shape.
  - Live `codex exec` probes succeeded through the `slimference-codex` provider and returned `CODEX_HOOKS_CONFIRMED`, `CODEX_ENABLE_HOOKS_DONE`, and `TOP_LEVEL_HOOK_DONE`, proving the proxy path, shell tool loop, and provider accounting remain healthy.
  - No real `SessionStart`, `UserPromptSubmit`, `PostToolUse`, or `Stop` Slimference flight events were emitted by Codex CLI `exec` in those probes, even with `--enable hooks` and both hooks.json shapes. Only proxy `/backend-api/codex/responses` flights appeared.
  - Therefore Codex CLI `exec` certification must not count hook-side Layer-0/PostToolUse savings yet. The reliable CLI hot path is the per-process `slimference-codex` HTTP provider route. Hook delivery remains unproven for interactive Codex CLI/App and must be closed separately before hooks are treated as a live savings layer.
- 2026-05-14 CLI max-out validation after hook/config/test hardening:
  - Fixed the local false-negative CI gate where `internal/integrate` assumed the default daemon URL was unreachable even when the real Slimference daemon was running. The detector still defaults to `ProxyURL`; the test now injects an unreachable test default.
  - Closed the remaining coverage gaps with behavior tests for current `hooks=false` conflicts, nested hook removal preserving non-Slimference entries, exported captured-output argv parsing, tokenizer fallback estimation, and in-window tool-output archive/no-shrink branches.
  - Removed a stale unreachable Layer-1 branch that tried to initialize `result.Messages` after it had already been initialized for every non-empty request.
  - Rebuilt `./slimference`, installed it to `/Users/christopher/.local/bin/slimference`, restarted the daemon, and verified PID `39916` on port `8990`.
  - Live CLI-only Codex smoke returned exactly `SLIMFERENCE_CLI_MAX_OK` through the process-local `slimference-codex` provider. The macOS System HTTPS proxy remained disarmed for all nine active network services, so Codex App traffic stayed direct.
  - Flight `5342d2a3cd7fb733` showed `source=proxy`, `provider=codex_chatgpt`, `host=127.0.0.1:8990`, `path=/backend-api/codex/responses`, `route_mode=upstream`, `confidence=provider_reported`, `provider_input_tokens=30630`, `provider_cached_tokens=7552`, `provider_output_tokens=11`, and `output_reduce.reason=below_min_tokens`.
  - `go run ./scripts/ci` passed all 8 steps: gofmt, vet, build, test, 100% coverage gate, Codex smoke gate (`57.14%` fixture ratio), synthetic live-corpus gate (`56.20%` ratio), and leaf-audit gate.
  - `slimference gain --proxy` reported `50` provider proxy requests today, `48` provider-reported requests, `2.2M` provider input tokens, `1.6M` provider cached tokens, `8.3K` provider output tokens, `18.6K` billable input savings estimate, `1.4M` cache-read discount equivalent, and `1.5M` net billable-equivalent estimate. Cache-read discount is observed provider accounting, not claimed Slimference-caused token deletion.
- 2026-05-14 Codex CLI Layer-0 proxy hot-path fix:
  - Codex CLI `exec` still does not emit reliable PostToolUse hook events, so hook-side L0 must not be required for CLI savings.
  - The proxy now runs the existing Layer-0 captured-output filter bank directly on Codex/OpenAI-style `function_call_output` history by resolving the matching `function_call` arguments (`command`, `cmd`, `command_line`, `argv`, or read-tool `path`).
  - The parser also accepts current CLI-adjacent variants: `local_shell_call`, `local_shell_call_output`, direct `command` arrays, `aggregated_output`, direct `stdout`/`stderr`, and wrapped output objects.
  - 2026-05-15 CLI polish broadened the same safe resolver for low-drawdown future Codex shapes: `cmdline`, `shell_command`, `args`, `local_shell`, `local_shell_call`, `bash_command`, `run_command`, `terminal.exec`, raw-string read inputs, and read path aliases `file_path`, `filepath`, and `absolute_path`. Unknown shapes still fail open.
  - Codex CLI exec envelopes are handled without throwing away metadata: the proxy preserves `Chunk ID`/wall-time/exit-code headers, compacts only the payload after `Output:`, and writes the compacted text back into the original JSON field.
  - This makes Codex CLI tool-output compaction work on the real `/backend-api/codex/responses` HTTP path, without modifying Codex CLI config beyond the per-process `slimference-codex` provider override and without relying on Codex hook delivery.
  - Safety gates: only command-resolved tool results are touched; compaction is accepted only when token count decreases; the original message slice is cloned before mutation; the existing zero-downside whole-request guard still reverts if later stages expand the request.
  - Planner polish: Codex CLI HTTP provider requests now mark WebSocket mutation as bypassed for the explicit `codex_cli_http_provider` reason, classify Codex cache telemetry without a previous response id as `codex_cache_accounting_only`, and keep `exact_reply` prompts out of Layer-4 directive plans.
  - UX polish: `slimference proxy run codex --proxied -- <args>` now executes the same one-process Codex CLI environment that `proxy env codex --proxied` prints, so normal repo-local testing no longer requires copy/paste or shell `eval`.
  - Unit proof: `TestServeHTTP_CodexResponsesProxyLayer0CompactsToolOutput` sends a Codex Responses body containing `function_call(shell, {"command":"git status --short"})` plus a large `function_call_output`; `TestServeHTTP_CodexResponsesProxyLayer0CompactsLocalShellEnvelope` covers the local-shell envelope path. With Layer 1/2/3 and output-reduce disabled, the upstream body is still shortened, the tool output becomes `[git status]...`, and the debug summary records layer `0` with lower `after_layer0` tokens.
  - Live proof after rebuild/restart: nested Codex CLI `exec --ignore-rules` ran raw `/opt/homebrew/bin/bash -lc 'git status --short .'` through the per-process `slimference-codex` provider and returned `SLIMFERENCE_CLI_PROXY_L0_FINAL_OK`. Flight `3b2bad28691dbf9d` recorded `layers=[0]`, estimated input `359 -> 142`, provider-reported `input=32721`, `cached=32128`, `output=14`, and `billable_savings_estimate=217`.
  - Output-reduce guard proof: a long German exact-reply CLI prompt first exposed negative overhead (`+132` input tokens for a 15-token answer). Shape detection now treats German exact-output phrases as `exact_reply`. After rebuild/restart, the same class of prompt returned `SLIMFERENCE_CLI_L4_SKIP_OK`; flight `7999f20d585c9c60` recorded `output_reduce.applied=false`, `reason=exact_reply`, `estimated_original_input_tokens=2792`, `estimated_final_input_tokens=2792`, `provider_input_tokens=34929`, `provider_cached_tokens=7552`, and `provider_output_tokens=13`.
  - Coverage proof: `go run ./scripts/ci` passes all 8 steps with total coverage `100.0%`, proxy coverage `100.0%`, outputreduce coverage `100.0%`, Codex smoke gate `57.14%`, and synthetic live-corpus gate `56.20%`.
- 2026-05-14 interactive hook UX correction:
  - Fresh interactive Codex evidence showed the default PostToolUse replacement path was chat-hostile: `continue:false` + `additionalContext` created visible `PostToolUse hook (stopped)` blocks, duplicate archive previews, and output-token overhead.
  - The architecture is corrected: Codex CLI tool-output savings stay on the proxy `/backend-api/codex/responses` path; default hooks are now silent precision/telemetry only.
  - `SessionStart` emits no context by default, `PreToolUse` no longer blocks/reruns Bash by default, and `PostToolUse` archives/records silently by default.
  - Visible PostToolUse replacement blocks are opt-in only with `SLIMFERENCE_CODEX_HOOK_MODE=compact` or `aggressive`; PreToolUse block-and-rerun is `aggressive` only; SessionStart debug context is `debug` only.
