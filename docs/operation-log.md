# Slimference Operation Log

Append-only protocol of operative steps performed by automation agents.
Each entry: ISO timestamp, phase, command, outcome, decision.

---

## 2026-05-17 — T209 Live Codex-CLI Certification

Driver: Claude Opus 4.7 (1M context, max effort) via Claude Code session.
Pre-state ratified by user: commit `658c94e TASK 219: clarify legacy telemetry noops`.
Slimference daemon: PID 9252, canonical path `~/.local/bin/slimference daemon`, disarmed preflight.

Safety contract:
- Claude Code session talks to `api.anthropic.com`; not in hosts-patch blast radius.
- User opens a second Terminal as emergency-disarm fallback before Phase 1.
- Any anomaly in `degraded_sessions`, `parse_failures`, or unexpected `hosts_active`/`api.anthropic.com` entry triggers immediate `slimference disable && slimference root-disarm`.
- No env vars, no `openai_base_url`, no system proxy. Codex-only hosts.

### Phase 0 — Baseline (read-only)

Executed 2026-05-17.

Verified:
- `which slimference` = `/Users/christopher/.local/bin/slimference`, v2.0.2.
- Daemon: PID 9252, command `/Users/christopher/.local/bin/slimference daemon` (canonical).
- `slimference status --preflight`:
  - CA installed=true, in_keychain=**false** (needs Phase 1)
  - Daemon running=true, health=true
  - Listener: `:443=false`, `:8443=false`, `:8990=true`
  - Network: hosts_active=false, entries=0
  - Apps: codex_cli=true, codex_desktop_app=true, claude_code=false (all detected on disk)
  - DoH chatgpt.com OK → 104.18.32.47
  - DoH api.openai.com OK → 162.159.140.245
- `/etc/hosts`: slimference marker block present BUT all entries commented out (legacy from prior root-arm with anthropic, now inert).
- Listener sockets: only `:8990` LISTEN; no `:443`/`:8443`.
- `which codex` = `/Users/christopher/.npm-global/bin/codex`, `codex-cli 0.130.0` (WSS-capable build).

Anomaly + fix:
- `~/.slimference/run/daemon.pid` contained stale PID **10251** (dead). True daemon PID is **9252** (confirmed via `~/.slimference/slimference.pid` JSON + lock holder).
- Root cause hypothesis: a second `slimference daemon` invocation around 15:44 (2 min after legit start) wrote its own getpid() into the plain-int PID file before failing on lock acquisition. `writePIDFile()` in `cmd/slimference/hosts_lifecycle.go:157` writes before the lock-bound listener confirms uniqueness — that ordering should be inverted in a follow-up task to prevent recurrence.
- Fix applied now: wrote `9252\n` to `~/.slimference/run/daemon.pid`, verified `kill -0 9252` succeeds.
- Impact if unfixed: `slimference enable`/`disable` SIGHUP routes via this file → would have ESRCH'd → daemon would not hot-reload (config still written, but SNI listener wouldn't start without daemon restart).

Baseline ratified. Ready for Phase 1 (CA Keychain trust) when user confirms safety-net Terminal is open.

### Phase 1 — Trust CA (interactive)

Pending user readiness.

---

## 2026-05-17 — PID Reload Drift Closed in Code

Commit: `14a3168 TASK 226: prepare scoped Codex WSS promotion`.

Correction to the Phase 0 anomaly note above:
- The immediate manual PID rewrite was only a temporary recovery.
- The durable fix is now in code: foreground/TUI proxy starts do not write
  `~/.slimference/run/daemon.pid`; only the managed daemon owns that file.
- `signalDaemonReload()` now self-heals: if the legacy reload PID is missing
  or stale, it falls back to the canonical daemon PID record in
  `~/.slimference/slimference.pid`.
- Daemon startup writes the reload PID only after the proxy listener is
  confirmed active, so failed second daemon starts cannot steal the reload
  target.
- Daemon-start tests stub the PID writer so local test runs cannot overwrite
  the user's real reload PID file.

Post-fix live verification:
- `go test ./... -count=1 -timeout 300s` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all 8 gates; aggregate coverage was 99.6%.
- `go run ./scripts/build --install` produced matching stripped 18M binaries at
  `./slimference` and `~/.local/bin/slimference`.
- Daemon restarted as PID `72425`.
- `~/.slimference/run/daemon.pid` and `~/.slimference/slimference.pid` both
  resolved to PID `72425`.
- `slimference status --preflight` no longer changes the reload PID.
- Live state remained disarmed: no hosts patch, no `:8443`, no `:443`, scoped
  Codex route disabled, Claude Code disabled.

---

## 2026-05-18 — T233 Responses-Safe Stop Injection

Problem:
- T209 scoped HTTP smoke reached the `slimference-codex` provider, but OpenAI
  Responses upstream rejected the request with `Unsupported parameter: stop`.
- Cause: `outstop.MergeIntoBody` injected Chat-Completions `stop` into
  Responses-shaped Codex bodies.

Fix:
- `outstop.MergeIntoBody` now injects only when the top-level body has a
  `messages` array and no `input` field.
- Responses-shaped bodies (`input`) are safe no-ops for both OpenAI and
  CodexChatGPT providers.
- HTTP and WSS call sites inherit the guard through the shared outstop package.

Verification:
- Focused tests passed for `internal/outstop` and `internal/proxy`.
- Full `go test ./... -count=1 -timeout 300s` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all 8 gates; aggregate coverage was 99.6%.
- Rebuilt and installed the stripped binary, then restarted daemon as PID
  `87481`.
- Live scoped HTTP smoke passed:
  `slimference codex run --transport=http -- exec "Reply with exactly: PING"`
  returned `PING`, exit 0.
- `/admin/state.savings.stop_seq_injections` stayed at 0 for the live smoke.

Remaining:
- Resume T209/T224 with explicit WSS smoke and certification capture. Do not
  promote `auto` to WSS until WSS proof records `frames_reencoded > 0`,
  `parse_failures = 0`, and `degraded_sessions = 0`.

---

## 2026-05-18 — T209 Live Sequence Resumed (Phases 1–4)

Driver: Claude Opus 4.7 (1M context) live audit after T233 fix landed.
Pre-state ratified: HEAD `0488068 TASK 233: skip stop injection for Responses API`.
Daemon PID `87481`, working tree clean, `~/.codex/config.toml` baseline SHA
`0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`.

Safety contract (unchanged):
- No `slimference lab *`, no global hosts/pfctl/Keychain.
- No `OPENAI_API_BASE`, `HTTPS_PROXY`, macOS System Proxy mutation.
- Browser ChatGPT, ChatGPT.app, Claude Code untouched.

### Phase 1 — Scoped HTTP smoke (per-process, no enable)

Command:
`slimference codex run --transport=http -- exec "Reply with exactly: PING"`

Result:
- Codex banner reports `provider: slimference-codex` (routing confirmed).
- Response body: `PING`; exit 0.
- `/admin/state.savings.stop_seq_injections` stayed at 0 (T233 verified live).
- WSS counters all 0 (HTTP path bypasses raw WSS frontdoor, as designed).

### Phase 2 — `slimference enable` (persistent scoped route)

Command: `slimference enable`

Result:
- Marker block written to `~/.codex/config.toml` (+11 lines, 212 → 223).
- Block content: `model_provider = "slimference-codex"`,
  `base_url = "http://127.0.0.1:8990/backend-api/codex"`,
  `supports_websockets = false`, `wire_api = "responses"`.
- Backup written to
  `~/.codex/config.toml.slimference-codex-route-backup-<timestamp>`.
- `/admin/state.codex_route`: `enabled=true`, `complete=true`,
  `transport="http"`, `auto_transport="http"`, `wss_certified=false`,
  `fallback_reason="wss certification missing"`.
- `status --preflight` confirmed: `:8443=false`, `hosts_active=false`,
  `claude_code=enabled=false`. Scope contract held.

### Phase 3 — Scoped WSS smoke (`--transport=wss`)

Command:
`slimference codex run --transport=wss -- exec "Reply with exactly: WSS_OK"`

Result:
- Codex banner reports `provider: slimference-codex`.
- Response body: `WSS_OK`; exit 0.
- WSS counter delta:
  - `engine_active=true`, `mitm_bridged=1`, `passthrough_bridged=0`.
  - `bytes_c2s=67617`, `bytes_s2c=99682`.
  - `c2s_frames=2`, `s2c_frames=17`, `frames_forwarded=19`.
  - **`frames_reencoded=0`**.
  - **`parse_failures=1`**.
  - **`degraded_sessions=1`**.
  - `byte_bridge_only=true`, `mutation_active=false`.
- `savings.stop_seq_injections=0` confirmed unchanged on WSS path too
  (T233 fix holds at the second call site in `wsmitm_phasef.go:102`).

Diagnosis:
- Codex CLI 0.130 emits at least one WSS frame the `internal/proxy/wsmitm`
  parser cannot decode. Bridge degrades to byte-equal fail-open: Codex
  receives correct upstream answer, but Slimference cannot apply Phase-F
  mutation to that session.
- This is *defensive* behaviour as designed: parse failure → degrade →
  byte-equal forward, never break the conversation to optimise.
- T226 promotion criteria (`parse_failures=0`, `degraded_sessions=0`,
  `frames_reencoded>0`) are therefore NOT met by Codex 0.130 today.
  `auto=http` correctly stays pinned. `~/.slimference/codex-wss-cert.json`
  must NOT be written.

### Phase 4 — `slimference disable` (cleanup verify)

Command: `slimference disable`

Result:
- Exit 0; "Codex route disabled: ... (removed_block)".
- `~/.codex/config.toml` lines back to 212 (baseline).
- SHA256 after disable: `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`
  — **bit-identical** to pre-enable baseline.
- `/admin/state.codex_route.enabled=false`, `complete=false`,
  `transport=""`.
- Marker block fully removed; backup files preserved under
  `~/.codex/config.toml.slimference-codex-route-backup-*`.

### Decisions and next gates

- Phases 1, 2, 4: PASS. Scoped Codex CLI is live-validated; T233 fix holds
  in HTTP and WSS paths; enable/disable is reversible to bit-equality.
- Phase 3: PARTIAL PASS. Codex functional via WSS bridge, but Phase-F
  parser degrades on Codex 0.130. WSS-cert remains blocked.
- Next task is T224: tshark capture of the actual WSS frame stream,
  isolate which frame type / opcode / continuation pattern degrades the
  parser, then patch `internal/proxy/wsmitm` to either decode or
  intentionally pass through that frame class without recording it as a
  parse failure.
- T226 (auto promotion) stays blocked behind T224.
- T225 (Desktop proof) is independent and can start in parallel.
- `tshark` 4.6.5 is installed at `/opt/homebrew/bin/tshark`. ChmodBPF is
  NOT installed, so `/dev/bpf*` is `crw------- root:wheel`. Captures
  require either `sudo tshark ...` or the Wireshark installer's
  "Install ChmodBPF" option.

End live state ratified: daemon disarmed, route disabled,
`~/.codex/config.toml` SHA equal to pre-Phase-2 baseline, working tree
clean, no global side effects.

---

## 2026-05-18 — T234 WSS Parser Safety Fix

Driver: Codex after T209 Phase 3 identified `parse_failures=1` and
`degraded_sessions=1` on a successful scoped WSS run.

Change:
- `internal/proxy/wsmitm.Session` now treats legal text payloads that are not
  JSON object envelopes as non-mutatable and forwards them byte-equal without
  setting degraded mode.
- RSV/compressed-extension text frames are explicitly forwarded byte-equal
  without parse/degrade, matching the existing `wscompact` blocker semantics.
- Malformed object-shaped JSON still increments `parse_failures`, sets
  `degraded`, and byte-bridges the rest of the session.

Verification before live:
- `go test ./internal/proxy/wsmitm ./internal/proxy -run 'TestSession|TestWSPhaseF|TestMITMConversation' -count=1 -timeout 120s`
  passed.
- `go test ./... -count=1 -timeout 300s` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all eight gates; aggregate coverage stayed above
  the 99.5% gate.
- `go run ./scripts/build --install` built the stripped product binary; repo
  binary and `~/.local/bin/slimference` SHA256 matched.
- Daemon restarted healthy on `:8990`; pidfiles matched the live daemon PID;
  status remained disarmed (`:8443=false`, `:443=false`, `hosts_active=false`,
  `codex_route.enabled=false`, `claude_code=false`).

Live scoped WSS retries:
- `./slimference codex run --transport=wss -- exec "Reply with exactly: WSS_OK"`
  returned `WSS_OK`, exit 0.
- WSS counters after the first retry:
  - `mitm_bridged=1`
  - `c2s_frames=2`, `s2c_frames=17`
  - `frames_forwarded=19`
  - `frames_reencoded=0`
  - `parse_failures=0`
  - `degraded_sessions=0`
  - `byte_bridge_only=true`
  - `mutation_active=false`
  - `savings.stop_seq_injections=0`
- Tool-using scoped WSS retries also returned correctly and kept
  `parse_failures=0`, `degraded_sessions=0`, and `stop_seq_injections=0`.
- `~/.codex/config.toml` remained bit-identical to the baseline SHA after every
  run because `codex run` uses the per-process provider override.
- Final clean-build retry after removing temporary diagnostics:
  `./slimference codex run --transport=wss -- exec "Reply with exactly: FINAL_WSS_OK"`
  returned `FINAL_WSS_OK`, exit 0. Counters:
  `mitm_bridged=1`, `c2s_frames=2`, `s2c_frames=18`,
  `frames_forwarded=20`, `frames_reencoded=0`, `parse_failures=0`,
  `degraded_sessions=0`, `stop_seq_injections=0`.
  Repo and installed binary SHA256 both
  `6b6dd9078397f7a59a6b20e860173817cbe526cb0573036aeae12fd57dfb684f`.
  Daemon PID `26546` ran without `SLIMFERENCE_WSS_FRAME_DUMP_DIR`.

Diagnosis after parser fix:
- T234 fixed the false-positive degradation.
- `frames_reencoded=0` is now explained by the real blocker: Codex 0.130 WSS
  uses compressed payloads (`permessage-deflate` / RSV1). Those frames are safe
  to bridge but not yet mutation-capable.
- A temporary env-gated frame dump was used during diagnosis and removed from
  product code before commit.

Decision:
- T234 is complete as a parser-safety fix.
- WSS auto-promotion remains blocked.
- New T235 owns extension-aware `permessage-deflate` decode/re-encode so
  Phase-F can mutate compressed scoped WSS traffic without stripping native
  WebSocket extensions.

---

## 2026-05-18 — T235 permessage-deflate Phase-F Mutation

Driver: Codex after T234 proved safe byte-equal WSS bridging but no mutation
on compressed Codex 0.130 payloads.

Scope contract:
- No global lab commands, no `/etc/hosts`, no pfctl, no Keychain trust, no
  system proxy.
- No Claude hooks, no Anthropic routing.
- No manual write to `~/.slimference/codex-wss-cert.json`; T226 owns
  certification persistence.
- WSS streamcut remains disabled and split to T236.

Change:
- `internal/wscompact` now parses negotiated `permessage-deflate` profiles,
  splits RSV1/RSV2/RSV3 bits, writes RSV1 frames deliberately, and implements
  RFC 7692 raw-deflate inflate/deflate with sync-flush tail handling.
- `internal/proxy/wsmitm.Session` can inflate complete compressed text
  messages, run the Phase-F handler, and re-encode only when mutation happens.
  Unmodified compressed frames are forwarded byte-equal after their plaintext
  advances the destination-side rolling dictionary, preserving context takeover
  for later mutated messages.
- Reassembled compressed payloads and inflated plaintext payloads are bounded.
  Size-cap hits fail open to byte-equal forwarding, record `compression_errors`,
  and block further compressed mutation for that direction without degrading the
  session.
- `ServeRawUpgrade`, `ServeUpgrade`, and the transparent WSS dispatcher pass the
  negotiated WebSocket extension profile into the bridge instead of stripping
  `Sec-WebSocket-Extensions`.
- Codex Responses `response_item.payload` wrappers are now reconstructed after
  mutation.
- The WSS Phase-F adapter keeps session-local tool-call metadata learned from
  server-to-client output item frames, then applies it when later
  client-to-server `function_call_output` frames arrive. This matches Codex
  Responses WSS, where tool-call metadata and tool output are split across
  directions and turns.
- `/admin/state.wss` exposes compressed-message and Phase-F mutation counters:
  `compressed_messages_inspected`, `compressed_messages_mutated`,
  `compressed_messages_bypassed`, `compression_errors`,
  `phasef_requests`, `phasef_request_bodies`,
  `phasef_request_messages_indexed`, `phasef_text_deltas`,
  `phasef_terminal_responses`, and `phasef_mutations`.

Safety decision:
- Output streamcut over WSS was tested and rejected for T235: blanking deltas
  produced re-encoded frames but hung Codex CLI. HTTP/SSE streamcut remains
  unchanged; WSS terminal-safe streamcut is T236.

Verification:
- Focused WSS/codec tests passed:
  `go test ./internal/wscompact ./internal/proxy/wsmitm ./internal/proxy -count=1 -timeout 120s`.
- Focused cross-direction tool-state tests passed:
  `go test ./internal/proxy -run 'TestApplyProxyLayer0WithRememberedToolUse|TestWSPhaseFRequestCompactsToolOutputAfterServerToolCallItem|TestWSPhaseFRequestCompactsToolOutputAcrossResponsesRequests|TestWSPhaseFRequestCompactsCodexResponseItemPayloadLayer0' -count=1 -timeout 60s`.
- Full `go test ./... -count=1 -timeout 300s` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all eight gates; aggregate coverage was 99.5%.
- Focused race checks passed for the WSS/session paths:
  `go test -race ./internal/wscompact ./internal/proxy/wsmitm ./internal/proxy -run 'TestSession|TestWSPhaseF|TestApplyProxyLayer0|TestExtractMessages_Codex|TestServeRawUpgradeExtractsExtensions|TestPhaseFAdapterReceivesNegotiatedProfile' -count=1 -timeout 180s`
  and `go test -race ./internal/wscompact -count=1 -timeout 120s`.

Live scoped WSS proof:
- Simple codec sanity:
  `./slimference codex run --transport=wss -- exec "Reply with exactly: T235_CODEC_OK"`
  returned `T235_CODEC_OK`, exit 0, with `parse_failures=0`,
  `degraded_sessions=0`, and `compression_errors=0`.
- Mutation proof 1:
  `./slimference codex run --transport=wss -- exec "<two-step git status prompt>"`
  returned `L0_GIT_OK`, exit 0.
  Counters: `frames_reencoded=1`, `compressed_messages_mutated=1`,
  `phasef_mutations=1`, `input_tokens_saved=1059`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`, `streamcut_fires=0`,
  `stop_seq_injections=0`, `codex_route.enabled=false`.
- Mutation proof 2 after daemon restart:
  same prompt returned `L0_GIT_OK`, exit 0.
  Counters: `frames_reencoded=1`, `compressed_messages_mutated=1`,
  `phasef_mutations=1`, `input_tokens_saved=1035`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`, `streamcut_fires=0`,
  `stop_seq_injections=0`, `codex_route.enabled=false`.
- `~/.codex/config.toml` stayed bit-identical to baseline SHA
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`
  after the scoped runs.

Decision:
- T235 acceptance is met.
- T226 is unblocked to record version-matched WSS certification through the
  product certification path and then promote `transport=auto` to WSS for the
  certified Codex/Slimference tuple.
- T236 remains open and must pass before WSS streamcut joins the default WSS
  savings set.

Post-review hardening:
- Added compressed-message and inflated-plaintext size caps after review noted
  the reassembly buffer had no explicit bound.
- Cap hits forward byte-equal, record `compression_errors`, block further
  compressed mutation for that direction, and do not increment
  `parse_failures` or `degraded_sessions`.
- Added focused fail-open tests for both cap paths.
- Re-verified after the hardening:
  `go test ./internal/proxy/wsmitm -run 'TestSessionCompressedPayloadLimitBypassesAndBlocks|TestSessionInflatedPayloadLimitBypassesAndBlocks|TestSessionDecompressesAndMutatesNoContextTakeover|TestSessionCompressedReplaceFalseKeepsContextForLaterMutation' -count=1 -timeout 60s`,
  `go test ./internal/wscompact ./internal/proxy/wsmitm ./internal/proxy -count=1 -timeout 120s`,
  `go test -race ./internal/wscompact ./internal/proxy/wsmitm -count=1 -timeout 120s`,
  `go test ./... -count=1 -timeout 300s`,
  `go vet ./...`, and `go run ./scripts/ci` all passed. CI aggregate coverage
  remained 99.5%.

---

## 2026-05-18 — Post-Commit Verify of T235 + Caps (HEAD a2e3d92)

Driver: Claude Opus 4.7 live, after Codex committed T235 with
permessage-deflate codec plus 16/64 MiB cap fix as
`a2e3d92 TASK 235: permessage-deflate Phase-F for scoped Codex WSS`.

Pre-state:
- HEAD `a2e3d92`, working tree clean.
- Daemon PID `74071` had been running 7h04m on stale pre-cap binary
  SHA `603addfe18e907ea2ea4490d87da6d815d6959a501c44851f0994281efd13c4d`.
- All earlier Opus live retries ran against the pre-cap binary.

Re-build + restart to ratify HEAD source actually runs in the daemon:
- `go run ./scripts/build --install` produced new stripped binary at
  `./slimference` and `~/.local/bin/slimference`, SHA
  `8ae76d230658d23571126f3fef16bfe602af0efc423f4d0e5325ae82c1426e5b`.
- Old daemon `74071` SIGTERM'd cleanly.
- Fresh daemon spawned via `nohup ~/.local/bin/slimference daemon`.
- New daemon PID `89080`, HTTP 200 on `/_slimference/admin/state`.
- Note: this daemon is not launchd-managed; it inherits the
  reload-PID owner discipline from the earlier PID-drift fix. Reload
  PID file matches running PID.

Live smoke A — sentinel prompt on post-cap binary:
- `slimference enable` → marker block written, route enabled.
- `slimference codex run --transport=wss -- exec "Reply with exactly: POST_COMMIT_OK"`
  returned `POST_COMMIT_OK`, exit 0.
- `slimference disable` → marker removed.
- Counters delta from baseline 0:
    parse_failures = 0
    degraded_sessions = 0
    compression_errors = 0
    compressed_messages_inspected = 3061
    compressed_messages_mutated = 0
    frames_reencoded = 0
    stop_seq_injections = 0
    mitm_bridged = 3
- `~/.codex/config.toml` SHA after disable:
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`
  — bit-identical to baseline.

Live smoke B — tool-use prompt on post-cap binary:
- `slimference enable`.
- `slimference codex run --transport=wss -- exec "Run 'git ls-files | head -3' then reply with exactly: TOOL_USE_OK"`.
- Codex executed the shell tool (visible output `AGENTS.md\nCLAUDE.md\n`),
  returned `TOOL_USE_OK`, exit 0.
- `slimference disable`.
- Counters delta from smoke A end:
    compressed_messages_inspected: 3061 -> 3125 (+64)
    parse_failures = 0
    degraded_sessions = 0
    compression_errors = 0
    frames_reencoded = 0
    compressed_messages_mutated = 0
    stop_seq_injections = 0
    mitm_bridged = 3 -> 4 (+1)
- `~/.codex/config.toml` SHA after disable:
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`.
- Daemon `89080` still healthy, HTTP 200.

Interpretation:
- Post-cap binary is FUNCTIONALLY safe. The Phase-F pipeline runs end
  to end (3125 compressed messages inspected, 0 parse failures, 0
  degrade events, 0 compression errors). Caps are not triggered by
  normal Codex CLI traffic (well below 16 MiB).
- Neither Opus smoke produced a Phase-F mutation. Both prompts were
  short, with at most a single tool call. Codex's original L0_GIT_OK
  proof on pre-cap binary did mutate — that proof used a longer flow
  with repeated tool calls and produced
  `frames_reencoded=1, compressed_messages_mutated=1,
  input_tokens_saved=1035` cumulative. Layer-0 dedup needs a reuse
  candidate; trivial sentinel prompts do not create one.
- Conclusion: live smokes confirm the post-cap binary is stable and
  reversal is bit-perfect. They do NOT prove mutation continuity on
  the post-cap binary. The T226 certify command path will perform
  that proof properly — its acceptance criterion includes
  `frames_reencoded > 0` at issue time, so the cert can only be
  written when a live mutation has actually occurred against the
  current binary + Codex version tuple.

End live state ratified:
- HEAD `a2e3d92`, working tree clean.
- Binary `8ae76d23...26e5b` in both `./slimference` and
  `~/.local/bin/slimference`.
- Daemon PID `89080` healthy on `:8990`.
- `~/.slimference/run/daemon.pid` matches running PID.
- `:8443=false`, `:443=false`, hosts inactive, route disabled.
- `~/.codex/config.toml` SHA baseline preserved.
- Claude Code untouched.
- No global lab. No `/etc/hosts`/pf/Keychain side effects.
- Ready for Codex to land T226 certify command on top of this state.

---

## 2026-05-18 — T226 Live Verify (Phase 1: Negative-Path Hardening, Positive-Path Blocked)

Driver: Claude Opus 4.7 live, after Codex committed T226 as
`f0041e1 TASK 226: scoped Codex WSS auto-promotion via codex certify command`.

Goal of this phase: rebuild on T226 binary, restart daemon, generate a
live Phase-F mutation, then run `slimference codex certify wss` to
issue the cert and verify auto=WSS promotion.

What landed:
- Build + install: stripped binary at `./slimference` and
  `~/.local/bin/slimference`, SHA
  `1581a83c077ada5aa26ea3fc751af37b53ca4f8a0c0ab53e352b7fd4d20afa17`.
- Daemon restarted: old PID 89080 SIGTERM'd, fresh PID `7290` running
  the T226 binary on :8990, HTTP 200.
- `slimference codex --help` now lists `certify` subcommand.

Mutation-trigger attempts (3 prompts, all returned correct responses,
none triggered Phase-F mutation):

1. `exec "Execute the shell command 'git ls-files | head -5' a first
   time, then execute the EXACT same shell command a second time.
   After both executions reply with exactly: REUSE_OK"`
   - Codex executed git ls-files ONCE (model planner optimised away
     the redundant call). Response: `REUSE_OK`, exit 0.
   - Counters: parse_failures=0, degraded_sessions=0,
     compression_errors=0, frames_reencoded=0,
     compressed_messages_mutated=0, compressed_messages_inspected=844.

2. `exec "Output the exact line 'REPDET_TEST_LINE' fifteen times,
   each on its own line, then the final line 'REUSE_OK'. No
   commentary, no other text."`
   - Codex emitted 15 identical lines + REUSE_OK, exit 0.
   - Counters: same green flags, frames_reencoded still 0,
     compressed_messages_inspected 936, repdet_rewrites=0,
     repdet_bytes_saved=0. Repdet did not fire on a single-turn
     repeated literal pattern (likely requires multi-turn context or
     a different trigger shape).

Both attempts confirm: post-T226 binary is functionally safe (Phase-F
pipeline inspects every compressed message, no parse / degrade /
compression errors, caps unhit), but neither prompt generated a
StaleReadAging, ObsoleteReadPrune, Layer-0 dedup, or Repdet
mutation. Codex's original L0_GIT_OK proof on the pre-cap binary
(saving 1059 input tokens cumulative) was almost certainly a
multi-turn / multi-tool-call setup not reproducible from a single
`codex exec` prompt.

Negative-path verification of the certify command (HARD GREEN):

- `slimference codex certify wss --dry-run` (with live state showing
  frames_reencoded=0): refused with exit code 1 and printed:
    codex certify: WSS proof is not green
      wss.frames_reencoded got=0 want=>0
      wss.compressed_messages_mutated got=0 want=>0
      wss.mutation_active got=false want=true
      wss.byte_bridge_only got=true want=false
  No file written.

- `slimference codex certify wss` (live, no flags): identical refusal,
  exit code 1, no file written.

- `~/.slimference/codex-wss-cert.json`: absent (verified by `ls`).

- `slimference codex status --json`:
    auto.transport         = "http"
    auto.wss_certified     = false
    auto.fallback_reason   = "wss certification missing"
    auto.certification_path = "/Users/christopher/.slimference/codex-wss-cert.json"

- CODEX_BIN override accepted: stub script returning
  `codex-cli 0.99.0-drift-test` was honored by the certify command's
  version probe. Drift falsification of an EXISTING cert cannot be
  observed live until a cert exists; the eight DecideAutoTransport
  fallback branches are covered by Codex's certification_test.go
  contract.

State after the live phase:
- Daemon PID `7290` healthy on :8990, T226 binary.
- `slimference disable` returned the marker block.
- `~/.codex/config.toml` SHA after disable:
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`
  — bit-identical to baseline.
- `:8443=false`, `:443=false`, hosts inactive, route disabled.
- No `~/.slimference/codex-wss-cert.json`.
- Drift-test stub removed.
- Working tree before this op-log append: clean. Will be one modified
  file (this op-log) afterwards.
- No global lab, no /etc/hosts, no pfctl, no Keychain, no env routing.
- Claude Code untouched.

Decision and handoff:
- T226 NEGATIVE PATHS are LIVE-VERIFIED and behave exactly as
  specified. The cert command refuses correctly, exits 1, lists every
  violated criterion with got/want values, never touches the
  filesystem on refusal.
- T226 POSITIVE PATH (live cert issue + auto=WSS proof + drift
  falsification of an existing cert) is BLOCKED on producing a real
  Phase-F mutation. From a single `codex exec` prompt this is not
  reliably reproducible.
- Next handover: Codex must specify a reproducible mutation trigger.
  Two viable shapes:
    (a) A multi-turn Codex session prompt that exercises
        StaleReadAging or ObsoleteReadPrune via re-reading the same
        file across turns. Sequence:
          slimference codex run --transport=wss -- exec "..."
          slimference codex run --transport=wss -- exec "..." (resume)
        Document the exact two prompts that triggered L0_GIT_OK
        originally so Opus can reproduce it byte-for-byte.
    (b) A repdet-triggering single-turn prompt that is known to fire
        applyRepdetDelta or applyRepdetResponse on Codex 0.130. The
        prompt must produce a response stream whose pattern shape
        matches what wsmitm_phasef.go:183-188 detects. The 15-line
        identical literal did not match.
- Once Codex supplies the trigger, Opus runs:
    1. The trigger prompt -> confirm /admin/state.wss shows
       frames_reencoded>0 and compressed_messages_mutated>0 in a
       single observation cycle.
    2. `slimference codex certify wss --dry-run` -> JSON shape check.
    3. `slimference codex certify wss --operator "opus-verify"
       --notes "T226 live issue"` -> writes the cert.
    4. `slimference codex status --json` -> auto.wss_certified=true.
    5. Two `--transport=auto` runs with daemon restart between.
    6. Drift falsification: stub codex version via CODEX_BIN, observe
       DecideAutoTransport fallback to http with correct
       fallback_reason.
    7. Append "T226 WSS Auto Promotion" final section to this log.

Ratified end state for Codex handover:
- HEAD `f0041e1`, working tree has this op-log append as the only
  modification.
- Binary `1581a83c...0afa17` in both binary paths.
- Daemon PID `7290` healthy.
- `~/.codex/config.toml` SHA equal to baseline.
- No cert file.
- Codex CLI fully functional via slimference scoped path; verified by
  three live runs returning correct sentinel responses with exit 0.

---

## 2026-05-18 — T226 Live Verify (Phase 2: Real Mutation, Cert Issue, Auto-WSS Promotion)

Driver: Codex live, continuing from Opus Phase 1 evidence on
`f0041e1 TASK 226: scoped Codex WSS auto-promotion via codex certify command`.

Initial state:
- Daemon PID `7290` healthy on `:8990`.
- Binary SHA in both `./slimference` and `~/.local/bin/slimference`:
  `1581a83c077ada5aa26ea3fc751af37b53ca4f8a0c0ab53e352b7fd4d20afa17`.
- `~/.codex/config.toml` SHA:
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`.
- `:8443=false`, `:443=false`, hosts inactive, route disabled.
- No `~/.slimference/codex-wss-cert.json`.
- `slimference codex certify wss --dry-run` correctly refused because
  `frames_reencoded=0`, `compressed_messages_mutated=0`,
  `mutation_active=false`, and `byte_bridge_only=true`.

Real mutation trigger:

1. Created a temporary Git repo:
   `tmpdir=/tmp/slimf-l0-live.5lN7we`,
   `git -C "$tmpdir" init -q`, then 160 untracked
   `synthetic_*.go` files.
2. Ran:
   `slimference codex run --transport=wss -- exec "Run exactly this shell command once: git -C /tmp/slimf-l0-live.5lN7we status --short . After the command finishes, reply with exactly: L0_LIVE_OK"`.
3. Codex executed:
   `/opt/homebrew/bin/bash -lc 'git -C /tmp/slimf-l0-live.5lN7we status --short .'`
   and returned `L0_LIVE_OK`, exit 0.

Counter snapshot after the live WSS mutation:
- `mitm_bridged=5`
- `c2s_frames=11`
- `s2c_frames=990`
- `frames_reencoded=1`
- `compressed_messages_inspected=1001`
- `compressed_messages_mutated=1`
- `compressed_messages_bypassed=0`
- `compression_errors=0`
- `phasef_requests=11`
- `phasef_request_bodies=11`
- `phasef_request_messages_indexed=8`
- `phasef_text_deltas=926`
- `phasef_terminal_responses=9`
- `phasef_mutations=1`
- `mutation_active=true`
- `byte_bridge_only=false`
- `parse_failures=0`
- `degraded_sessions=0`
- `input_tokens_saved=939`
- `stop_seq_injections=0`

Cert issue:
- `slimference codex certify wss --dry-run --operator codex-live --notes "T226 real scoped WSS Layer-0 git status trigger"` printed a valid JSON proof:
  `schema_version=1`, `transport=wss`,
  `route_profile=scoped_raw_wss_phasef`, `codex_version=0.130.0`,
  `slimference_version=2.0.2`, `passed=true`, `frames_reencoded=1`,
  `degraded_sessions=0`, `parse_failures=0`.
- `slimference codex certify wss --operator codex-live --notes "T226 real scoped WSS Layer-0 git status trigger"` wrote:
  `/Users/christopher/.slimference/codex-wss-cert.json`.
- Cert JSON verified with `jq`; timestamp:
  `2026-05-18T09:45:38.152136Z`.

Auto-WSS proof:
- `slimference codex status --json` reported:
  `auto.transport=wss`, `auto.wss_certified=true`,
  `certified_codex_version=0.130.0`,
  `certified_slimference_version=2.0.2`.
- `slimference status --preflight` reported:
  `Codex route_enabled=false complete=false transport=off auto=wss wss_certified=true daemon=true`.
- `slimference codex run --transport=auto -- exec "Reply with exactly: AUTO_WSS_OK"`
  returned `AUTO_WSS_OK`, exit 0. WSS counters advanced:
  `mitm_bridged 5 -> 6`, `c2s_frames 11 -> 13`,
  `compressed_messages_inspected 1001 -> 1019`, with
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`.

Version-drift falsification:
- A temporary `CODEX_BIN` stub returning `codex-cli 0.130.1` made
  `slimference codex status --json` resolve:
  `auto.transport=http`, `auto.wss_certified=false`,
  `fallback_reason="codex version changed since wss certification"`.
- Stub removed after the check.

Daemon-restart persistence:
- `slimference restart` stopped PID `7290` and started PID `12310` on `:8990`.
- `~/.slimference/run/daemon.pid` contains `12310`; `ps` confirms
  `/Users/christopher/CODE/Slimference/slimference daemon`.
- After restart, `slimference codex status --json` still reported
  `auto.transport=wss`, `auto.wss_certified=true`.
- `slimference codex run --transport=auto -- exec "Reply with exactly: AUTO_RESTART_OK"`
  returned `AUTO_RESTART_OK`, exit 0. Fresh-daemon WSS counters:
  `mitm_bridged=1`, `c2s_frames=2`, `s2c_frames=16`,
  `compressed_messages_inspected=18`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`.

Final state:
- Daemon PID `12310` healthy on `:8990`.
- `~/.codex/config.toml` SHA remains
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`.
- `:8443=false`, `:443=false`, hosts inactive, route disabled.
- Claude Code untouched; no Anthropic routing, no Claude hooks.
- No global lab, no `/etc/hosts`, no pfctl, no Keychain, no system proxy.
- T226 positive path is complete: real scoped Codex CLI WSS mutation,
  cert issue, auto-WSS promotion, daemon-restart persistence, and version
  drift fallback are all live-verified.
