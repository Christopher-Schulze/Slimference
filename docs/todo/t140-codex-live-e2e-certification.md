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

- [ ] Codex CLI transparent traffic certified.
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
