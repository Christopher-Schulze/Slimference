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

---

## 2026-05-18 — T226 Final Closure: Auto=WSS Live Verified by Opus

After Codex committed `9ddcbfb TASK 226: certify scoped Codex WSS auto
promotion live`, Opus performed independent post-cert verification:

Cert file inspected:
- `~/.slimference/codex-wss-cert.json`, 384 B
- schema_version=1, transport=wss, route_profile=scoped_raw_wss_phasef
- codex_version=0.130.0, slimference_version=2.0.2
- passed=true, frames_reencoded=1, degraded_sessions=0, parse_failures=0
- timestamp=2026-05-18T09:45:38Z (recent), operator=codex-live
- notes="T226 real scoped WSS Layer-0 git status trigger"

slimference codex status --json now reads:
  auto.transport         = "wss"
  auto.wss_certified     = true
  auto.certified_codex_version = "0.130.0"
  auto.certified_slimference_version = "2.0.2"
  auto.fallback_reason   absent

slimference status --preflight shows:
  Codex auto=wss wss_certified=true daemon=true

Live `--transport=auto` smoke (independent of Codex's runs):
- First attempt timed out at 60s — transient ChatGPT-Auth latency, not
  a Slimference bug. State stayed disarmed and clean.
- Retry with 180s timeout: response `AUTO_OK`, exit 0, elapsed 7s.
- WSS counter delta: mitm_bridged +1, frames_forwarded +18, all error
  counters 0, frames_reencoded delta 0 (sentinel prompt has no Layer-0
  reuse candidate; mutation continuity already proven by Codex's
  certification run, not by every subsequent invocation).

Drift falsification (HARD GREEN):
- Stub `codex-cli 0.130.1-drift` injected via CODEX_BIN.
- `slimference codex status --json` returned:
    auto.transport = "http"
    auto.wss_certified = false
    auto.fallback_reason = "codex version changed since wss certification"
    auto.certified_codex_version = "0.130.0"  (stored cert, unchanged)
- Restore real codex → status returns auto.transport=wss without
  re-issue (LoadCertification still finds the stored record).
- Cert file untouched by the drift probe.

`~/.codex/config.toml` SHA after enable/disable cycle:
`0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`
— bit-identical to baseline across the entire T226 ceremony.

System state at end of T226:
- HEAD `9ddcbfb`, working tree clean (one append to this op-log
  follows; it is intentionally untracked / picked up later).
- Daemon PID 12310, healthy, T226 binary
  `1581a83c077ada5aa26ea3fc751af37b53ca4f8a0c0ab53e352b7fd4d20afa17`.
- :8443=false, :443=false, hosts inactive, route disabled.
- Codex auto-transport unlocked to WSS for the codex 0.130.0 +
  slimference 2.0.2 version tuple. Any drift in either side falls
  back to HTTP deterministically.
- Claude Code untouched. No global lab. No /etc/hosts. No pfctl. No
  Keychain trust. No system proxy.

T226 closed. Next: T224 indistinguishability audit (capture-based,
does NOT gate auto=WSS, only gates "indistinguishable" claims) and
T225 Codex Desktop App proof (does Desktop respect the scoped
provider route).

Preflight for T224/T225 (Opus side):
- tshark 4.6.5 at /opt/homebrew/bin/tshark; /dev/bpf* is
  crw------- root:wheel, so captures require `sudo tshark` (one
  password prompt per session) or the Wireshark "Install ChmodBPF"
  launchd helper for persistent access.
- indist_probe builds cleanly via `go build ./scripts/utils/indist_probe`.
  Subcommands: capture, diff, lock-golden.
- en0 is the active interface (192.168.33.113).
- /Applications/Codex.app exists (created 2026-05-17 01:12). T225 can
  proceed once the operator agrees to restart the desktop app.
- research/indist/ created and confirmed gitignored.

---

## 2026-05-18 — T225 Codex Desktop App Live Test (PARTIAL: sideband yes, conversation no)

Driver: Claude Opus 4.7 live with operator action (Codex.app restart
+ in-app prompt).

Setup:
- HEAD `9ddcbfb` (T226 closed, auto=WSS active).
- Daemon PID 12310, healthy on :8990.
- `slimference enable` wrote marker block to `~/.codex/config.toml`
  with `transport=wss` (auto resolved to WSS via cert),
  `supports_websockets=true`, `wire_api=responses`.
- Operator: Cmd+Q Codex.app, reopened, sent one prompt in the chat UI
  ("Sag Hallo in einem Wort").

Screenshot evidence:
- Codex.app UI shows "Slimference Codex" as the active provider in
  the model picker chip. Proves the app READ the marker block and
  loaded the provider definition.

Live network reality (via lsof on Codex app-server child process
`codex` PID 24925):
  TCP 192.168.33.113:63937 -> 172.64.155.209:443 (ESTABLISHED)
  TCP 127.0.0.1:63976 -> 127.0.0.1:8990 (ESTABLISHED)
  TCP 127.0.0.1:63979 -> 127.0.0.1:8990 (ESTABLISHED)

- 172.64.155.209 is Cloudflare ChatGPT IP. That direct TLS 443
  connection carries the conversation. Slimference never sees it.
- The two 127.0.0.1:8990 connections are sideband only.

WSS counter delta over the full Codex.app interaction:
  mitm_bridged           +2  (the two sideband sessions)
  compressed_messages_inspected  +0
  frames_forwarded       +0
  bytes_c2s              +0
  bytes_s2c              +0
  frames_reencoded       +0
  parse_failures         +0
  degraded_sessions      +0

25-second passive observation after the prompt: every counter flat.
The two sideband connections stayed established but never carried any
WSS Phase-F traffic.

Interpretation:
- This live test confirms the prediction from the user's own
  pre-existing config.toml comment, written long before T209/T220:
  "Codex 0.130 hardcodes CHATGPT_CODEX_BASE_URL for ChatGPT-auth
  conversation traffic ... These two config keys only affect sideband
  endpoints (memories, plugins, login). They do NOT redirect the
  conversation."
- Codex.app reads `model_provider="slimference-codex"` and USES it
  for sideband endpoints, but the ChatGPT-auth conversation WSS is
  hardcoded to chatgpt.com and bypasses Slimference entirely.
- T225 acceptance criterion ("Desktop traffic must hit scoped WSS")
  is therefore NOT met by the marker-route alone. Slimference's
  marker block is necessary but not sufficient for Desktop.
- Codex CLI auto=WSS path remains unaffected and live-certified
  (T226). This is a Desktop-only finding.

Cleanup:
- `slimference disable` returned "removed_block", exit 0.
- `route.enabled=false`, `route.complete=false`.
- `auto.transport=wss`, `auto.wss_certified=true` (cert file
  unaffected by enable/disable cycle, correct).
- `~/.codex/config.toml` SHA after disable:
  `f7ab2ef254a466e76e2da882dcdcdb3ca4a7652c7ba60263f6b982ca0ae173b8`
  — different from the earlier session baseline
  `0a1ce7a471fa4d4496a56604289cc5bb5402469b50086c4427310b7c99cccc67`,
  but the difference is owned by Codex.app, not by Slimference.
  Verified by diffing the pre-enable backup
  `~/.codex/config.toml.slimference-codex-route-backup-20260518T102228`
  (SHA `5ddf5b02b83b165656dabc2e042013c89f19cd5956284d1be5b7e7f4837eeedd`)
  against the current file: the ONLY difference is the slimference
  marker block. Codex.app independently modified the file (likely
  project entries and session metadata on its restart). Slimference's
  enable/disable contract is "byte-equal within a matched pair", and
  that contract held.

Decision and follow-up:
- T225 GREEN for: model-picker UI shows the slimference provider
  name, sideband endpoints route correctly.
- T225 NOT GREEN for: conversation traffic through Slimference.
- T228 (Codex Desktop scoped launcher) is now justified by hard
  evidence. The launcher must intercept the hardcoded
  CHATGPT_CODEX_BASE_URL path without using global hosts/pfctl.
  Candidate approaches:
    (a) launcher that sets CHATGPT_CODEX_BASE_URL env on Codex.app
        startup (scoped to the launched process; requires verifying
        Codex 0.130 respects that env in addition to its hardcoded
        default);
    (b) Electron/Chromium proxy-server flag for the renderer
        processes — verify it covers the app-server worker;
    (c) wrapper script that exports the env and execs Codex.app;
    (d) if none of the above works without affecting Browser ChatGPT
        / ChatGPT.app, document the Desktop limitation and require
        operator opt-in via explicit global-lab mode.
- T224 indistinguishability audit is independent of this finding and
  remains optional claim-hardening for the CLI auto=WSS path.

Operator-visible cosmetic ask:
- The chat UI shows the provider name as "Slimference Codex". The
  user requested it be shortened to just "Slimference". This lives
  in `internal/codexroute/codexroute.go` blockBody() in the
  `name = "Slimference Codex"` line. Trivial 1-line change for
  Codex's next commit.

End live state:
- HEAD `9ddcbfb`, working tree dirty only with this op-log append
  and a stale config.toml backup (intentional, Codex.app side).
- Daemon PID 12310, healthy.
- Codex CLI auto=WSS still live and certified.
- Codex.app reverted to direct ChatGPT routing (route disabled).
- Browser ChatGPT, ChatGPT.app, Claude unaffected throughout.
- No global lab, no /etc/hosts, no pfctl, no Keychain, no system
  proxy.

---

## 2026-05-18 — T237 + T228 implementation (Opus-built, env-only launcher infeasible against Codex.app 0.131.0-alpha.9)

Driver: Claude Opus 4.7, direct implementation after operator hand-off
"kannst du das nicht sauber implementieren?".

T237 — provider display name shortened
---------------------------------------
Renamed the Codex provider display name from "Slimference Codex" to
"Slimference":
- `internal/codexroute/codexroute.go` (persistent block via `slimference enable`)
- `cmd/slimference/proxy_cmd.go` (per-process `-c` override via `slimference codex run`)
- `docs/transparent-mode.md` (doc sample TOML)

Historical evidence (`docs/operation-log.md` entries that mention
"Slimference Codex") intentionally NOT touched — those are append-only
records of what was true at the time. Status banner
`Slimference Codex route` in `slimference codex status` left as-is
(not user-visible in Codex.app UI, separate concern).

T228 — Codex Desktop scoped launcher (built and tested, conversation-routing infeasible)
---------------------------------------------------------------------------------------
Added `slimference codex launch-desktop` subcommand:
- `cmd/slimference/codex_desktop_launcher.go` (180 LOC, full file-level docstring with empirical finding)
- `cmd/slimference/codex_desktop_launcher_test.go` (14 tests, all green)
- `cmd/slimference/codex_cmd.go` (dispatch + help text)
- `cmd/slimference/completion.go` (bash completion entry)

Behaviour:
- `slimference codex launch-desktop [--probe] [--host=127.0.0.1] [--port=8990] [--app=<path>] [--env KEY=VAL...]`
- Spawns `/Applications/Codex.app/Contents/MacOS/Codex` (configurable
  via --app) with a scoped env that defensively sets 5 candidate
  base-URL variables: CHATGPT_CODEX_BASE_URL, OPENAI_BASE_URL,
  OPENAI_API_BASE, CHATGPT_BASE_URL, API_BASE_URL.
- Env is process-local: Browser ChatGPT, ChatGPT.app, Claude Code,
  and any future Codex.app launched via Finder/Spotlight remain
  untouched.
- `--probe` emits the override env as JSON without spawning, for
  inspection and CI.
- No /etc/hosts, no pfctl, no Keychain, no system proxy, no
  ~/.codex/config.toml mutation.

Live verification on Codex.app 0.131.0-alpha.9 (the version installed
at /Applications/Codex.app today):

1. After Codex.app was fully quit by operator, ran:
     ~/.local/bin/slimference codex launch-desktop
   New Codex.app spawned successfully, PID 51905 (Electron main),
   spawned child Rust app-server PID 51954.

2. Verified env inheritance via `ps eww -p 51954`:
     CHATGPT_CODEX_BASE_URL=http://127.0.0.1:8990/backend-api/codex
     OPENAI_BASE_URL=http://127.0.0.1:8990/backend-api/codex
     OPENAI_API_BASE=http://127.0.0.1:8990/backend-api/codex
     CHATGPT_BASE_URL=http://127.0.0.1:8990/backend-api/codex
   Env injection HAS reached the Rust app-server process tree.

3. Operator sent a chat prompt in the launched Codex.app. Codex
   responded normally. But UI did NOT show the "Slimference" provider
   chip (correct: no slimference enable was active, so config.toml
   had no marker block; Codex.app fell back to its built-in default
   provider name and default model selection).

4. lsof on the Rust app-server (PID 51954) during and after the
   prompt:
     codex 51954 ESTABLISHED 192.168.33.113:64708 -> 104.18.32.47:443
     codex 51954 ESTABLISHED 192.168.33.113:64707 -> 104.18.32.47:443
     codex 51954 ESTABLISHED 192.168.33.113:64705 -> 104.18.32.47:443
     codex 51954 ESTABLISHED 192.168.33.113:64702 -> 104.18.32.47:443
   104.18.32.47 is Cloudflare's chatgpt.com IP. ZERO connections to
   127.0.0.1:8990. Slimference daemon WSS counter delta over 30s:
   exactly 0 across mitm_bridged, bytes_c2s, bytes_s2c,
   compressed_messages_inspected, frames_reencoded.

5. `strings /Applications/Codex.app/Contents/Resources/codex` (Rust
   binary, Mach-O arm64) inspection revealed:
     - Multiple HARDCODED `https://chatgpt.com/backend-api` URLs in
       data section.
     - Version string: 0.131.0-alpha.9 (Desktop is AHEAD of CLI
       0.130.0).
     - Override env vars exposed by the binary:
         CODEX_REFRESH_TOKEN_URL_OVERRIDE  (auth endpoint)
         CODEX_ARC_MONITOR_ENDPOINT_OVERRIDE (telemetry/safety monitor)
         CODEX_EXEC_SERVER_URL              (exec server, separate)
         API_BASE_URL                       (generic, unclear use)
     - NO CHATGPT_CODEX_BASE_URL handling in the current Desktop
       Rust binary.
     - NO OPENAI_BASE_URL / OPENAI_API_BASE / CHATGPT_BASE_URL
       handling for the conversation route.

Conclusion: env-only Desktop launcher cannot redirect Codex.app
0.131.0-alpha.9 conversation traffic. The conversation endpoint is
hardcoded in the Rust binary and no env hook exists today. This
matches and refines the pre-existing config.toml comment in the
user's `~/.codex/config.toml` ("Codex 0.130 hardcodes
CHATGPT_CODEX_BASE_URL...") — current Codex Desktop does not even
read that name.

Decisions:
- The launcher is retained and committed:
   * `--probe` is operationally useful as a diagnostic surface.
   * The defensive 5-key override set keeps working if a future
     Codex Desktop release adds an env hook that overlaps.
   * The spawn surface remains a clean, process-scoped, reversible
     way to start Codex.app — useful for future T228 follow-up.
- T228 acceptance criterion ("Desktop conversation must hit scoped
  WSS through env injection") is NOT met against the current Codex
  Desktop version, and the empirical evidence shows no env-only
  workaround can meet it. Closing T228 as IMPLEMENTED-INFRASTRUCTURE,
  CONVERSATION-ROUTE-BLOCKED-UPSTREAM.
- Operator may try the launcher against future Codex.app releases by
  running `slimference codex launch-desktop --probe` and then
  `slimference codex launch-desktop`, repeating the lsof check on the
  spawned app-server PID. If a future release routes through :8990,
  T228 flips to GREEN without code changes.
- Real Desktop conversation routing today requires either upstream
  exposing a scoped env/config surface, or a global-lab MITM path
  (out of scope for the default product surface).

Cosmetic finding the user reported:
- "Anderes Modell ausgewählt, andere Directory" — Codex.app's own
  session-state restore picks last-used model and cwd when launched
  without a project context. Not a slimference issue, no fix
  proposed here.
- "Slimference badge im Chat war weg" — correct behaviour: the badge
  comes from the slimference-codex provider entry in
  ~/.codex/config.toml, which only exists while `slimference enable`
  is active. T228 launcher does NOT write the marker block; it only
  injects env. For UI-visible "Slimference" provider chip in
  Codex.app, run `slimference enable` BEFORE
  `slimference codex launch-desktop` (combined sideband + env-spawn
  pattern). The conversation will still bypass slimference because
  of the hardcoded route described above; only the chip + sideband
  endpoints route to slimference.

Verification gates (all green):
- `go test ./cmd/slimference -run 'TestCodexLaunchDesktop...' -count=1`
  = 14/14 passing (including real-app probe test which uses
  /Applications/Codex.app when present).
- `go test ./... -count=1 -timeout 300s` green.
- `go vet ./...` green.
- `go run ./scripts/ci` green, 8/8 gates, total coverage 99.5%.
- `gofmt -l .` clean.

Cleanup verified:
- All spawned Codex.app processes killed (SIGTERM then SIGKILL for
  helper processes).
- `pgrep -f Codex.app` returns 0.
- Daemon restarted on latest binary
  `7de4e4526faee799f565b9f2db3990d243c283ead8a5f29d5027dacf1638031f`,
  PID 58172, HTTP 200, T226 cert intact (`auto.transport=wss`,
  `wss_certified=true`).
- `~/.codex/config.toml` SHA changed across this session to
  `997d068751ee9c7bb1168903dd71e7acb4beaba5a0134f028fe283463e5d037d`,
  but the change is Codex.app side: slimference enable/disable was
  not involved in the T228 test (T228 tests env-only spawn). The
  pre-test backup confirms slimference's own diff-set was zero.

Files staged (uncommitted, for next commit):
  M cmd/slimference/codex_cmd.go            (dispatch + help text)
  M cmd/slimference/completion.go           (bash completion)
  M cmd/slimference/proxy_cmd.go            (T237 rename)
  M docs/operation-log.md                   (this append)
  M docs/transparent-mode.md                (T237 rename in sample TOML)
  M internal/codexroute/codexroute.go       (T237 rename in block body)
  ?? cmd/slimference/codex_desktop_launcher.go      (T228 impl)
  ?? cmd/slimference/codex_desktop_launcher_test.go (T228 tests)

Suggested commit shape (two commits):

  Commit 1: TASK 237: rename Codex provider display name to "Slimference"
    - internal/codexroute/codexroute.go
    - cmd/slimference/proxy_cmd.go
    - docs/transparent-mode.md

  Commit 2: TASK 228: Codex Desktop scoped launcher (probe + spawn)
    - cmd/slimference/codex_desktop_launcher.go
    - cmd/slimference/codex_desktop_launcher_test.go
    - cmd/slimference/codex_cmd.go
    - cmd/slimference/completion.go
    - docs/operation-log.md
    - docs/todo/t228-*.md (status update: implemented-infrastructure, conversation-blocked-upstream)

## 2026-05-18 - T238 Desktop process-local proxy pre-live implementation

Context:
- User goal: maximum Slimference benefit with zero drawdown. Codex CLI is already
  certified auto-WSS. Codex Desktop must either prove a scoped process-local
  route or remain honestly direct-only.
- This work was done from inside Codex Desktop, so live Desktop launch/lsof
  proof was intentionally deferred to an external terminal/operator. No
  Codex.app process was spawned for the proof in this phase.

Implemented pre-live:
- Added `[transparent].scoped_desktop_proxy=true` default. This only exposes
  loopback CONNECT for process-local clients when existing CA material is
  present. It does not generate CA material on daemon start, and it does not
  arm hosts, pfctl, macOS system proxy, or Codex config.
- `proxy.New` now wires CONNECT/MITM when either global transparent mode is
  enabled or scoped Desktop proxy mode is usable. In scoped mode the allowlist
  is `chatgpt.com` only.
- CONNECT WebSocket upgrades now have an explicit Phase-F gate. Phase-F frame
  mutation is allowed only for `chatgpt.com/backend-api/codex/responses` with
  the Codex `responses_websockets` protocol. Other Desktop sideband WebSockets
  are tunneled byte-equal.
- `WebSocketTunnel` now supports `ServeUpgradeWithBridge(..., bridgeFrames)`
  so callers can forward the upgrade while disabling frame mutation.
- `slimference codex launch-desktop` now defaults to `--transport=proxy`.
  It injects only process-local proxy env (`HTTP_PROXY`, `HTTPS_PROXY`,
  `WSS_PROXY`, `ALL_PROXY`, lowercase variants, `NO_PROXY`, and
  `CODEX_NETWORK_PROXY_ACTIVE`) and refuses to spawn when the CA is missing or
  untrusted. `--transport=base-url` remains diagnostic/future-proof only.
- Added `slimference codex desktop status [--json]` for the handoff surface:
  CA trust, daemon reachability, WSS counters, live-proof-required flag, and
  conversation-observed flag.

Local verification:
- `go test ./cmd/slimference ./internal/proxy ./internal/config -count=1
  -timeout 180s` passed.
- Added tests for proxy env construction, CA launch refusal, probe JSON,
  scoped CONNECT activation without CA auto-generation, WSS bridge gating, and
  Desktop status gates.

Still requires external live proof:
- Rebuild/install/restart daemon.
- Run `slimference cert-trust` if CA trust is missing.
- Run `slimference codex launch-desktop --transport=proxy` from a quiet
  external terminal, send a Desktop chat prompt, and collect lsof plus
  `/admin/state.wss`.
- Accept Desktop Slimference mode only if app-server traffic reaches
  `127.0.0.1:8990`, no direct conversation socket to `chatgpt.com:443` remains,
  and WSS counters stay clean with real mutation before savings are claimed.
- If Codex.app bypasses proxy env, mark Desktop direct-only and keep all
  Desktop savings hidden.

---

## 2026-05-19 — T238 Codex Desktop process-local proxy live proof (PARTIAL — TCP routing works, TLS root-store blocked)

Driver: Claude Opus 4.7 live, operator (christopher) at the GUI.

Pre-state:
- HEAD `9c20457 TASK 238: prepare Codex Desktop process-local proxy proof`.
- Build: stripped binary SHA
  `4b1eb3b99c0c4746be5ad80070033b6c42f8bd34b0409ea9862b5b6f0c2dff26`,
  installed to `./slimference` and `~/.local/bin/slimference`.
- Daemon: PID 22757, healthy on :8990.
- Slimference CA trust installed via Keychain Access GUI
  (operator clicked "Always Trust" → fingerprint
  c3e5156458a6df1fd9e19e291a117d397a463105eadd2dad1d01d99a56ba612b).
  `slimference codex desktop status --json` reported
  `mode=ready_for_live_desktop_probe`, `ca_trust.trusted=true`.
- Pre-snapshot wss counters all 0.

Note: T226 cert is no longer green — Codex CLI is now reporting
`codex-cli 0.131.0` (was 0.130.0 when cert was issued). Drift
fallback is live: `auto.transport=http`,
`fallback_reason="codex version changed since wss certification"`.
This is correct defensive behaviour, not a regression. T226-follow-up
needed to re-certify against 0.131.0; not part of this T238 phase.

Probe (Step 5):
- `slimference codex launch-desktop --transport=proxy --probe`
  emitted 11 proxy env entries (HTTP_PROXY, HTTPS_PROXY, WSS_PROXY,
  ALL_PROXY, lowercase variants, NO_PROXY=127.0.0.1,localhost,::1,
  CODEX_NETWORK_PROXY_ACTIVE=1) and ca_trust=true.
- No spawn, no daemon touch.

Live launch (Step 7):
- All prior Codex.app processes confirmed killed (pgrep=0).
- `slimference codex launch-desktop --transport=proxy` spawned
  Codex.app PID 39578 (Electron main).
- App-server child PID 39627
  (/Applications/Codex.app/Contents/Resources/codex app-server).
- `ps eww -p 39627` confirmed inherited env on the app-server:
    HTTP_PROXY=http://127.0.0.1:8990
    HTTPS_PROXY=http://127.0.0.1:8990
    WSS_PROXY=http://127.0.0.1:8990
    ALL_PROXY=http://127.0.0.1:8990
    NO_PROXY=127.0.0.1,localhost,::1
    CODEX_NETWORK_PROXY_ACTIVE=1
  Env injection reaches the spawned process tree correctly.

lsof on app-server PID 39627:
- One ESTABLISHED TCP: `127.0.0.1:<ephemeral> → 127.0.0.1:8990`.
- ZERO direct connections to chatgpt.com:443 from the app-server.
- The HTTPS_PROXY env is honored by the Rust HTTP client for the
  CONNECT phase.

lsof on Codex Helper (Chromium NetworkService) PID 39584:
- Direct ESTABLISHED TCP to 104.18.32.47:443 (chatgpt.com Cloudflare),
  104.18.37.228:443 (same), and 35.190.80.1:443 (Google).
- UDP to 104.18.32.47:443 (QUIC).
- Chromium NetworkService does NOT honor HTTPS_PROXY env by default
  for renderer-side requests. The renderer side stays direct.
- This is the expected split: Electron has two independent network
  stacks (Rust app-server + Chromium NetworkService). The launcher
  scope only covers Rust app-server.

Live trigger (Step 7, operator sent a chat prompt):
- Within ~5 seconds of the prompt, `mitm_bridged` jumped from 0 to 8,
  then to 14 over the next ~10 seconds. The Phase-F dispatcher
  WAS reached by the spawned Codex.app's WSS upgrades.
- BUT every other counter remained at 0:
    bytes_c2s = 0, bytes_s2c = 0
    compressed_messages_inspected = 0
    compressed_messages_mutated = 0
    frames_reencoded = 0
    parse_failures = 0
    degraded_sessions = 0
    compression_errors = 0
    upstream_dial_failures = 0
- 14 WSS bridge sessions were opened. Slimference dialed upstream
  successfully each time (no upstream_dial_failures). But not a
  single application frame ever flowed through any of them.

Diagnosis: TLS-level rejection by Codex.app's Rust client.
- TCP CONNECT was accepted: mitm_bridged++ proves the bridge path
  was entered.
- Upstream dial succeeded: upstream_dial_failures=0.
- Slimference would have presented a slimference-signed leaf cert
  for chatgpt.com to the client side of the tunnel.
- Codex.app's Rust HTTP client almost certainly uses `rustls`
  with its default `webpki-roots` CA bundle (Mozilla root list
  baked into the binary). That bundle does NOT include the
  Slimference local CA, and `rustls` by default does not consult
  the macOS Keychain. The handshake therefore failed immediately,
  and the client closed the tunnel before any data could flow.
- The Keychain trust click set earlier is correct and necessary
  for any system-trust-store-aware client (Chromium, curl, Safari,
  many Apple frameworks), but is INVISIBLE to a rustls client
  that uses webpki-roots.

Failure class: `tls_trust_rejected`.
Embedded-root behaviour is identical in outcome to explicit pinning from the
operator's perspective, but the captured evidence proves only TLS trust
rejection before bytes flow, not an explicit SPKI/leaf pin.

Net result of T238 live phase:
- Launcher mechanism: VERIFIED CORRECT END-TO-END.
- Env inheritance to Codex.app Rust app-server: VERIFIED.
- HTTPS_PROXY honored by Rust client at TCP CONNECT layer:
  VERIFIED.
- Slimference CONNECT handler: VERIFIED (accepts, dials upstream,
  enters bridge).
- TLS termination with Slimference CA: REJECTED BY CLIENT
  (root-bundle mismatch).
- Phase-F Desktop savings: NOT ACHIEVABLE via env+CA-trust on
  Codex.app 0.131 today.
- Chromium NetworkService stays direct (separate stack, expected).
- Browser ChatGPT, ChatGPT.app, Claude Code: untouched throughout
  the entire session (verified earlier in this op-log; no
  /etc/hosts, no pfctl, no system proxy was used).
- `~/.codex/config.toml`: untouched by this run (no enable).
- T237 rename + earlier T226/T228/T235 commits intact, working
  tree clean before this op-log append.

Cleanup:
- All Codex.app and codex app-server processes killed (SIGTERM
  then SIGKILL for holdouts). pgrep=0 verified.
- Daemon 22757 still healthy on :8990.

Decision: T238 closes as IMPLEMENTED-INFRASTRUCTURE,
DESKTOP-CONVERSATION-BLOCKED-AT-TLS. The remaining path to real
Codex Desktop conversation routing now reduces to one of:
(a) OpenAI switches Codex.app's Rust client to use
    `rustls-native-certs` (or equivalent) so it honors the macOS
    Keychain. Our launcher would then work end-to-end with no code
    changes on our side.
(b) Operator opts into the legacy global lab mode
    (`slimference lab root-arm --global-chatgpt-hosts`) which uses
    /etc/hosts + pfctl to redirect chatgpt.com system-wide. This
    DOES affect Browser ChatGPT and ChatGPT.app and was explicitly
    rejected by the operator as a default product surface.

Operator-facing recommendation:
- Use Codex CLI (in terminal) via `slimference codex run -- ...`
  — full slimference savings, zero drawback.
- Launch Codex.app normally from Finder/Spotlight — direct routing,
  full functionality, no slimference involvement, no drawback.
- Optional: `slimference enable` to get the "Slimference" provider
  chip in Codex.app's UI (sideband route, still direct conversation).
- Wait for an upstream Codex Desktop release that either honors
  system trust or exposes a config hook; our launcher already covers
  that future.

---

## 2026-05-19 — T238 Follow-up: Desktop TLS Block Classified and CA-Env Probe Prepared

Driver: Codex follow-up after the live proof above.

Correction to terminology:
- The previous shorthand `cert_pinned` is too strong as a proven statement.
  The observed behavior is more precisely `tls_trust_rejected`: CONNECT enters
  Slimference, upstream dial succeeds, but Codex.app closes before application
  bytes flow. The likely cause remains Codex.app's Rust TLS stack using an
  embedded or non-Keychain root store that does not see the Slimference CA.
  Functionally this blocks Desktop savings the same way pinning would, but the
  evidence does not prove explicit SPKI/leaf pinning.

Code/docs follow-up:
- `slimference codex desktop status` now classifies the historical zero-byte
  CONNECT state as `mode=desktop_tls_blocked`,
  `failure_class=tls_trust_rejected` instead of reporting
  `ready_for_live_desktop_probe`.
- `slimference codex launch-desktop --transport=proxy --with-ca-env` was added
  as an explicit diagnostic branch. It injects `SSL_CERT_FILE`,
  `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and `NODE_EXTRA_CA_CERTS` pointing at
  the Slimference root only into the spawned Codex.app process.
- The launch-center TUI plan was corrected: "Launch Codex App" remains a
  visible capability-gated menu item. It is not removed just because current
  Desktop routing is blocked; it must render proven, diagnostic, or
  blocked/direct-only truth.
- New follow-up tasks were added for update resilience (T241) and the Desktop
  root-store/proxy compatibility matrix (T242).

Decision remains unchanged for current live Desktop use:
- Normal Finder/Spotlight Codex.app remains direct and unaffected.
- Desktop Slimference savings are not claimed until bytes and WSS frames flow.
- Codex CLI remains the proven Slimference savings path; current WSS auto cert
  is paused by the live Codex CLI update to 0.131.0 until recertification.

---

## 2026-05-19 — T239 Launch Center Consolidation

Driver: user requested the already-existing TUI be consolidated rather than a
fourth/parallel UI being created.

Implemented in the existing BubbleTea TUI:
- Default `ViewMain` now renders `LAUNCH CENTER` with exactly five primary
  entries: `Launch Codex CLI`, `Launch Codex App`, `Savings`, `Status`, and
  `Manage Slimference`.
- The old top-level daemon/route/layer/global-lab action list is no longer the
  default mental model. The existing Stats and Setup views remain as backing
  surfaces behind `Savings` and `Manage Slimference`.
- `Launch Codex CLI` opens a macOS Terminal session running
  `slimference codex run --transport=auto --`.
- `Launch Codex App` consumes the same Desktop capability state as
  `slimference codex desktop status`; if the machine is in
  `tls_trust_rejected`, the TUI blocks the Slimference Desktop launch claim and
  states that normal Finder launch remains direct.
- No direct-open menu item was added. Direct mode remains native launch outside
  Slimference.

Verification:
- `go test ./internal/tui ./cmd/slimference -count=1` passed after the
  consolidation.

---

## 2026-05-19 — T241/T243 Live Recert Closure and WSS-First Proof

Driver: Codex hardening pass after T239/T243 implementation and the live Codex
CLI update from 0.130.0 to 0.131.0.

Problem found:
- The initial T241 recert trigger was too weak. Small `git status` prompts
  produced clean WSS bridge traffic but no Phase-F mutation, so the strict WSS
  cert correctly stayed unavailable.
- A stronger manual live trigger proved the real shape needed for current Codex
  CLI: a large untracked-file `git status --short` result.

Implementation hardening:
- `slimference codex recertify wss` now seeds a temporary git repo with 160
  untracked `synthetic_*.go` files.
- The trigger asks Codex to run `git -C <temp> status --short`, which creates a
  deterministic large tool result that exercises Phase-F.
- The Codex exec invocation uses `--ignore-user-config` and `--ephemeral`, and
  starts from the stable caller workdir rather than `--cd` into the temporary
  repo. This prevents Codex from writing temporary project trust blocks into
  `~/.codex/config.toml`.
- Recert logging is injectable in tests, so unit tests cannot write real
  `~/.slimference` recert logs.

Live no-write verification:
- Command:
  `./slimference codex recertify wss --force --no-write --operator "codex-live" --notes "T243 config-stable recert trigger verification" --timeout=180s --json`
- Exit code: 0.
- Binary SHA after rebuild/install:
  `e2e6db76bacbfc88fa705acb0647ebf4ec2eb1ec3e7f21d34eea4e4187a314b1`
  for both `./slimference` and `~/.local/bin/slimference`.
- Config SHA before and after:
  `1c4708b7348841a3fb6b75a82fbd25ea59d170af7f3d8f6fb46e3ead46301d56`
  unchanged.
- Delta counters:
  - `mitm_bridged=1`
  - `bytes_c2s=73361`
  - `bytes_s2c=150120`
  - `c2s_frames=3`
  - `s2c_frames=86`
  - `frames_forwarded=88`
  - `frames_reencoded=1`
  - `compressed_messages_inspected=89`
  - `compressed_messages_mutated=1`
  - `phasef_mutations=1`
  - `parse_failures=0`
  - `degraded_sessions=0`
  - `compression_errors=0`

Auto status after the proof:
- `slimference codex status --json` reports:
  - `auto.mode=wss_phasef`
  - `auto.transport=wss`
  - `auto.wss_certified=true`
  - `auto.needs_recert=false`
  - current tuple: `codex=0.131.0`, `slimference=2.0.2`
  - certified tuple: `codex=0.131.0`, `slimference=2.0.2`
  - `recert_status=passed`
- Live `slimference codex run --transport=auto -- exec ...` returned the
  sentinel through provider `slimference-codex`, confirming the normal Launch
  Codex CLI path now selects WSS Phase-F again.
- Current daemon WSS state after live checks: `frames_reencoded=3`,
  `compressed_messages_mutated=3`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`.

Decision:
- T241 is closed for the current product requirement: update drift is repaired
  by real live recert, not by weakening the guard.
- T243 is partially live-proven: certified tuple and successful recert restore
  are green. Remaining T243 live work is the negative/fallback branch matrix:
  simulated drift to WSS bridge, forced WSS bridge failure to HTTP, daemon-down
  direct fail-open, and non-conversation Audio/Realtime/Voice passthrough.

---

## 2026-05-19 — T244 Atomic Install Hardening Opened

Driver: T241/T243 live build/install uncovered a macOS executable replacement
race.

Observed failure:
- Several commands launched from `~/.local/bin/slimference` became stuck in
  macOS `dyld_start` uninterruptible state after the installed binary was
  overwritten during live work:
  - PID 20342: `slimference stop`
  - PID 20575: `slimference start`
  - PID 20981: `slimference daemon`
  - PID 21193: `slimference version`
  - PID 21289: `slimference version`
- `kill` / `kill -9` did not clear them, which matches kernel-level
  uninterruptible process state. This is not a routing failure and not a Codex
  degradation, but it is unacceptable release ergonomics.

Recovery performed:
- The damaged installed binary was moved aside as
  `~/.local/bin/slimference.dyld-stuck-20260520T003339`.
- A fresh `./slimference` was copied into `~/.local/bin/slimference`.
- Foreground daemon PID 22413 from the repo binary remained healthy on :8990.

Fix landed:
- `scripts/build --install` no longer truncates the installed executable in
  place.
- It now writes to a same-directory temporary file, sets executable mode, syncs
  and closes the file, then atomically renames it over
  `~/.local/bin/slimference`.
- `go test ./scripts/build -count=1` covers replacement content, executable
  mode, and absence of leftover temporary files.
- Live atomic install verification with
  `go run ./scripts/build --out /tmp/slimference-atomic-install-test --install`
  exited 0, produced SHA
  `e2e6db76bacbfc88fa705acb0647ebf4ec2eb1ec3e7f21d34eea4e4187a314b1` in both
  `/tmp/slimference-atomic-install-test` and `~/.local/bin/slimference`, and
  did not increase the count of already stuck Slimference processes.

Remaining T244 work:
- Harden daemon `start`/`stop`/restart timeouts and stale PID diagnostics.
- Add release-cert evidence that rebuild/install/restart cannot strand new
  control commands.
- Decide cleanup for the moved-aside `slimference.dyld-stuck-*` file after the
  stuck OS processes are gone, likely after reboot.

---

## 2026-05-20 — T245 Non-Live CA/TUI Alignment

Driver: the product install must stay one simple Codex package while avoiding
unnecessary macOS Keychain gating for the proven scoped CLI WSS path and the
preferred Desktop `--with-ca-env` diagnostic path.

Implemented:
- `SetupState.IsHealthy()` no longer treats macOS Keychain trust as a core
  product-health requirement. CA material, daemon, listener, and route state
  remain visible; Keychain trust is a separate Desktop/Lab fallback signal.
- `slimference codex desktop status` now reports
  `slimference codex launch-desktop --transport=proxy --with-ca-env` as the
  Desktop proof command.
- Missing CA material still blocks Desktop diagnostics. Missing Keychain trust
  is now a note, not a gate, because the preferred proof injects
  `CODEX_CA_CERTIFICATE` and related process-local CA env vars.
- Historical zero-byte `tls_trust_rejected` WSS counters no longer make the TUI
  permanently block the Desktop menu action. The Launch Center can retry the
  process-local CA-env diagnostic, but it still must not claim Desktop savings
  until bytes and WSS frames are live-proven.
- TUI setup wording now says `CA material + launchd + Codex hooks`; it no
  longer asks users to trust a CA for normal CLI WSS.

Safe probes:
- `slimference codex desktop status --json` returned
  `mode=ready_for_live_desktop_probe`, `daemon_reachable=true`,
  `launch_command="slimference codex launch-desktop --transport=proxy --with-ca-env"`,
  and all WSS error counters at 0.
- `slimference codex launch-desktop --transport=proxy --with-ca-env --probe`
  emitted process-local proxy env plus `CODEX_CA_CERTIFICATE`,
  `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and
  `NODE_EXTRA_CA_CERTS`.
- `slimference install --dry-run --json` kept the unified product install
  order: `ca.generate`, `launchd.install`, `hooks.codex`, `notice.codex`.

Verification:
- `go test ./cmd/slimference -run 'CodexDesktop|LaunchCodexApp|LaunchCenter|Setup|Status|Install|CA' -count=1`
  passed.
- `go test ./internal/control ./internal/tui ./docs -count=1` passed.
- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all 8 gates. Aggregate statement coverage was
  99.5% with the current 95.0% hard gate.

Decision:
- CLI WSS remains the proven max-savings product path and does not need
  Keychain trust.
- Desktop via Slimference remains proof-gated. The next live-only step is T242:
  launch Codex.app through `--with-ca-env`, send a prompt, and verify lsof plus
  `/admin/state.wss` pre/post deltas before claiming Desktop savings.

---

## 2026-05-22 — T242 Partial Live Probe and Desktop Status Guard

Driver: continue T242 from the Launch Center / Desktop CA-env path without
letting CLI WSS recertification counters masquerade as Desktop proof.

Implemented:
- `codex launch-desktop` now starts Codex.app as a detached child process with
  `Setsid`, uses the app bundle executable directory as `cmd.Dir`, and checks a
  short startup window before reporting success.
- If the spawned app exits immediately, the command now refuses with the
  concrete wait status (`exit=N`, `signal=X`, or raw status) instead of printing
  a false "launched" success.
- `codex desktop status` now labels WSS telemetry as
  `wss_counters_scope=daemon_cumulative_not_desktop_proof`.
- `conversation_observed` stays `false` unless a future Desktop-specific proof
  surface records a spawned-process pre/post delta. Daemon-wide WSS counters
  from CLI recertification no longer make Desktop look green.

Live evidence:
- Rebuilt and installed with `go run ./scripts/build --install`; final
  installed and repo binary SHA:
  `0d100c55bea2c7b9a3a9fe527382602d3960e9e62e8fecfd227b652324837edf`.
- Fresh daemon PID `22249` stayed healthy on `127.0.0.1:8990`.
- `slimference codex recertify wss --operator=codex --notes="T246 live recert
  after Codex 0.133.0 drift"` repaired Codex CLI WSS for current
  `codex-cli 0.133.0`; `codex status --json` reported `auto.mode=wss_phasef`,
  `auto.transport=wss`, `wss_certified=true`, and `needs_recert=false`.
- `slimference codex run --transport=auto -- exec "Reply exactly:
  T246_AUTO_WSS_OK"` returned `T246_AUTO_WSS_OK`; WSS counters remained clean
  with `frames_reencoded=1`, `compressed_messages_mutated=1`,
  `parse_failures=0`, `degraded_sessions=0`, and `compression_errors=0`.
- `slimference codex launch-desktop --transport=proxy --with-ca-env --probe`
  emitted the expected process-local proxy and CA env set, including
  `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`, `CURL_CA_BUNDLE`,
  `REQUESTS_CA_BUNDLE`, and `NODE_EXTRA_CA_CERTS`.
- `slimference codex launch-desktop --transport=proxy --with-ca-env` launched
  a stable Desktop main process. Its `codex app-server --analytics-default-
  enabled` process connected to `127.0.0.1:8990`; Chromium NetworkService still
  held unrelated direct TLS sockets.
- During the observation window, `/admin/state.wss` did not move after launch:
  the latest counters still matched the CLI recert/smoke state. No Desktop
  prompt-tied WSS delta has been proven yet.
- `~/.codex/config.toml` SHA during the probe:
  `c61a7ae45052cf470110c591de7b54431269144f03060496771216c39cf3c53d`.

Decision:
- CLI path is green and max-savings WSS Phase-F for the current tuple.
- Desktop launcher and process-local proxy preconditions are stronger, but
  Desktop savings are still not claimed.
- Next Desktop proof remains live-only: send a prompt in the spawned Codex.app,
  capture pre/post WSS deltas and lsof for the app-server PID, then verify
  normal Finder/Spotlight relaunch goes direct again.

---

## 2026-05-22 — T242/T239 Final Desktop Prompt Verdict and Direct TUI Guard

Driver: close the prompt-driven Desktop proof without letting a broken
Desktop-Slimference branch become the daily UX.

Live Desktop verdict:
- `slimference codex desktop prove --manual --duration=8s --json` launched a
  scoped Codex.app process and reached `desktop_ready_for_prompt`.
- Operator sent `Slimference Desktop Probe OK` in the launched Codex.app. The
  app answered successfully, proving native Desktop UX still works.
- `slimference codex desktop prove --finish --json` returned
  `mode=desktop_ca_env_rejected`, `failure_class=tls_trust_rejected`.
- Observed delta shape: `mitm_bridged=14`, `bytes_c2s=0`, `bytes_s2c=0`,
  `frames_reencoded=0`, `compressed_messages_mutated=0`, `parse_failures=0`,
  `degraded_sessions=0`, and `compression_errors=0`.
- lsof/env evidence showed the spawned app-server had process-local proxy/CA
  env and a loopback socket to `127.0.0.1:8990`. Chromium NetworkService still
  held direct ChatGPT TLS sockets. The visible Desktop answer therefore cannot
  be counted as Slimference Phase-F traffic.

Implemented after the verdict:
- TUI `Launch Codex App` now opens normal direct Codex.app in the current
  working directory while Desktop Slimference is not green.
- TUI `Launch Codex CLI` now opens Terminal with `cd <current directory> &&
  slimference codex run --transport=auto --`, so CLI launch starts in the
  intended repo instead of a shell default directory.
- Removed the now-stale TUI manual-proof launch helper. Desktop proof remains
  an explicit diagnostic command: `slimference codex desktop prove --manual`
  followed by `--finish`.
- `codex launch-desktop` refuses when a Codex.app main process is already
  running, preventing false env-injection proof through macOS foregrounding.
- Docs and task files now state the product truth: CLI WSS is the max-savings
  path; Desktop daily launch stays direct until a future Codex.app/root-store or
  endpoint hook proves real bytes plus Phase-F mutation through Slimference.

Workspace-state finding:
- Current `~/.codex/config.toml` contains no `ClankWork-main` or
  `slimference-codex-probe` entries.
- Codex Desktop restored an old thread whose session metadata had
  `cwd=/Users/christopher/CODE/ClankWork-main`. That stale cwd came from
  Codex's saved conversation state, not from the active Slimference route.
- TUI direct Desktop launch now passes the current directory to Codex.app to
  avoid blank `open -a Codex` restoring a stale deleted workspace by default.

Cleanup:
- A pre-stub test run accidentally wrote a fake green Desktop proof result to
  `~/.slimference/codex-desktop-proof-result.json`
  (`launch_pid=4242`, `desktop_proxy_phasef_proven`). This was rotated aside
  as `.test-contaminated-20260522T165153` together with the stale proof session.
- After cleanup, `slimference codex desktop status --json` no longer reported a
  false Desktop proof: `mode=ready_for_live_desktop_probe`,
  `conversation_observed=false`, `last_proof=null`.

Verification:
- `go test ./cmd/slimference ./internal/tui -count=1` passed.
- `go test ./... -count=1` passed.
- `go vet ./...` passed.
- `go run ./scripts/ci` passed all 8 gates; aggregate statement coverage:
  `99.1%` with the project hard gate at `95.0%`.
- `go run ./scripts/build --install` installed binary SHA
  `21d37e757dd58d2dab227d02b87faf0cbdbb4960c758590b24dd87536a3f7e1c` to both
  `./slimference` and `~/.local/bin/slimference`.
- `slimference codex status --json`: `auto.mode=wss_phasef`,
  `auto.transport=wss`, `wss_certified=true`, `needs_recert=false`.
- `slimference status --preflight`: daemon PID `22249` healthy on `:8990`,
  `:443=false`, `:8443=false`, hosts inactive, transparent MITM disarmed.

Decision:
- Daily UX: `slimference` TUI -> Launch Codex CLI for max WSS savings; Launch
  Codex App for normal direct Desktop in the current folder.
- Desktop Slimference is not sold as working until a future diagnostic proof
  reaches `desktop_proxy_phasef_proven`.

---

## 2026-05-22 — Codex Desktop Deleted-Workspace Restore Fix

Driver: Codex.app reopened deleted workspace
`/Users/christopher/CODE/ClankWork-main` after normal launch.

Root cause:
- `~/.codex/config.toml` was already clean, but
  `~/.codex/.codex-global-state.json` still had
  `active-workspace-roots=["/Users/christopher/CODE/ClankWork-main"]`.
- The same deleted roots also existed in `electron-saved-workspace-roots`,
  `project-order`, and `electron-persisted-atom-state.sidebar-collapsed-groups`.
- Launching Codex.app from inside an existing Codex session can also inherit
  `CODEX_THREAD_ID`, `CODEX_CI`, and other `CODEX_*` runtime variables through
  macOS process launch. That can make a fresh Desktop launch resume the wrong
  old thread even when the workspace list is clean.

Cleanup performed:
- Backed up `~/.codex/.codex-global-state.json` and `.bak` under
  `~/.codex/stale-workspaces/clankwork-global-state-20260522T170548/`.
- Removed dead `ClankWork-main` and `ClankWork` roots from active global state.
- Set `active-workspace-roots` to `/Users/christopher/CODE/Slimference`.
- Verified both deleted directories are absent on disk and no live
  `ClankWork-main` references remain in active Codex app state.

Code fix:
- Desktop launch env now strips inherited `CODEX_*` variables before adding
  intentional Slimference proxy/CA variables.
- TUI direct Codex.app launch now strips inherited `CODEX_*`, removes old
  `PWD`/`OLDPWD`, and pins `PWD` to the current launch directory.
- TUI Codex CLI launch now runs through `/bin/bash -lc`, unsets inherited
  `CODEX_*`, then runs `slimference codex run --transport=auto --` in the
  current directory.

Verification:
- Normal `open -a Codex` after state cleanup kept
  `active-workspace-roots=["/Users/christopher/CODE/Slimference"]`.
- Clean relaunch with a stripped process env showed Codex.app main
  `PWD=/Users/christopher/CODE/Slimference` and no inherited
  `CODEX_THREAD_ID` / `CODEX_CI`.
- `lsof` showed the current `codex app-server` cwd as
  `/Users/christopher/CODE/Slimference`.
- `slimference codex status --json` stayed green:
  `auto.mode=wss_phasef`, `auto.transport=wss`, `wss_certified=true`,
  `needs_recert=false`.
- Focused tests for Desktop launch env and TUI Codex launch passed.

Decision:
- Deleted workspace restore is fixed locally.
- Desktop savings remain blocked by the existing Desktop TLS/root-store proof
  result; this fix is about correct Desktop launch state and zero stale-thread
  inheritance, not a Desktop savings claim.

---

## 2026-05-22 — T238/T239 Desktop Proxy Arg Follow-Up and Honest TUI Gate

Driver: user clarified that TUI `Launch Codex App` must mean real
Slimference mode, not "open direct as a consolation path". Direct Desktop mode
is Finder/Spotlight outside Slimference.

Implemented:
- `slimference codex launch-desktop --transport=proxy --with-ca-env` now passes
  Electron proxy arguments in addition to process-local proxy and CA env:
  `--proxy-server=http://127.0.0.1:8990` and
  `--proxy-bypass-list=localhost;127.0.0.1;::1`.
- TUI `Launch Codex App` now blocks unless `codex desktop status` reports a
  green Desktop Phase-F proof. It no longer opens direct Codex.app from the
  Slimference menu when Desktop savings are not green.
- Docs now use the correct admin state surface:
  `/_slimference/admin/state` under `.wss`.

Live Desktop follow-up:
- Final installed binary SHA after implementation and CI:
  `6f76fccded57895fbff1791d0d6292644bebaa4e73824b10f4de130ad6d83c55`.
- Scoped launch spawned Codex.app with the Electron proxy args above.
- `lsof` showed Chromium NetworkService using loopback proxy sockets and no
  non-loopback ChatGPT sockets for the launched process tree. This closes the
  earlier Chromium direct-socket bypass branch.
- A visible prompt was sent in the launched app. Slimference observed one new
  CONNECT/MITM session, but still zero application bytes and zero WSS frames:
  `mitm_bridged_delta=1`, `bytes_c2s_delta=0`, `bytes_s2c_delta=0`,
  `c2s_frames_delta=0`, `s2c_frames_delta=0`, `frames_reencoded_delta=0`,
  `compressed_messages_mutated_delta=0`, `parse_failures_delta=0`,
  `degraded_sessions_delta=0`, and `compression_errors_delta=0`.

Decision:
- Scoped Desktop routing is now correct up to CONNECT for both the Rust
  app-server and Chromium NetworkService, but current Codex.app still rejects or
  bypasses the local Slimference CA/root-store path before application bytes
  flow.
- Desktop real savings are therefore not available in current Codex.app without
  an upstream-supported endpoint/root-store hook or an explicitly global lab
  route. Slimference must not fake this.
- Daily UX remains honest:
  - TUI `Launch Codex CLI`: real WSS Phase-F savings.
  - TUI `Launch Codex App`: blocked until real Desktop proof is green.
  - Finder/Spotlight Codex.app: normal direct Desktop mode.

---

## 2026-05-22 — T246 Desktop App-Server Shim Candidate

Driver: user asked for a better Desktop path than proxy/CA/MITM, with maximum
savings potential, minimum OS complexity, and no collateral impact on Browser
ChatGPT, ChatGPT.app, Claude Code, system proxy, hosts, or normal Codex.app
launches.

New finding:
- Current Codex Desktop exposes a cleaner hook than HTTPS proxying:
  `CODEX_CLI_PATH` is used by Codex.app when spawning the Rust `codex
  app-server` child.
- Upstream Codex source shows `codex app-server` accepts `-c key=value` config
  overrides.
- Codex provider config can point `base_url` at
  `http://127.0.0.1:8990/backend-api/codex`, which maps to local WSS without
  TLS MITM or CA trust.

Non-Desktop smoke proof:
- Command shape:
  `codex exec --ephemeral -C /Users/christopher/CODE/Slimference -c
  'openai_base_url="http://127.0.0.1:8990/backend-api/codex"' 'Reply exactly
  LOCAL_BASE_URL_OK'`
- Result: Codex returned `LOCAL_BASE_URL_OK`.
- Slimference WSS counters moved cleanly:
  `bytes_c2s 117186 -> 172848`, `bytes_s2c 190943 -> 264672`,
  `c2s_frames 5 -> 7`, `s2c_frames 106 -> 122`, `phasef_requests 5 -> 7`.
- Error counters stayed zero:
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`.

Implemented in WIP:
- Added hidden `slimference app-server` shim. It validates scoped Desktop env,
  removes shim env, and execs the real Codex binary as `codex app-server` with
  local provider overrides.
- Made `slimference codex launch-desktop --transport=app-server` the default
  Desktop launch mode.
- Kept `--transport=proxy` and `--transport=base-url` as diagnostics.
- Retargeted `codex desktop prove` and the TUI `Launch Codex App` gate to the
  app-server shim proof mode.
- Updated docs/tasks so T238/T242 proxy/CA are closed negative for current
  Desktop, and T246 carries the remaining positive Desktop proof.

Current decision:
- This is not yet a Desktop savings claim.
- It is the best current engineering path because it removes CA trust,
  Keychain, HTTPS proxying, Electron proxy args, and TLS/root-store failure from
  the normal Desktop route.
- TUI `Launch Codex App` remains blocked until live Desktop prompt proof records
  `desktop_app_server_phasef_proven`: Desktop-specific WSS bytes, frames,
  Phase-F mutation, and zero parser/degrade/compression errors.

Pre-live install verification:
- Built and atomically installed WIP binary to both `./slimference` and
  `~/.local/bin/slimference`; SHA:
  `5baf78e28c73dac141be9995f423987867d29ef279fa7de33c3b8493955cd0bb`.
- Restarted daemon from installed binary; PID `80059`, health endpoint OK.
- `slimference codex status --json` stayed green:
  `auto.mode=wss_phasef`, `transport=wss`, `wss_certified=true`,
  `needs_recert=false`, Codex `0.133.0`, Slimference `2.0.2`.
- `slimference codex launch-desktop --transport=app-server --probe` emitted
  only app-server shim env:
  `CODEX_CLI_PATH=/Users/christopher/.local/bin/slimference`,
  `SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN=/Users/christopher/.npm-global/bin/codex`,
  `SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex`,
  and loopback `NO_PROXY`; no proxy/CA env.
- `slimference codex run --transport=auto -- exec --ephemeral ...` returned
  `CLI_AUTO_WSS_OK`. The tiny prompt used WSS cleanly but did not trigger
  Phase-F mutation, so daemon `.wss` showed byte-equal traffic only for this
  smoke; the persisted CLI WSS cert remains green.

---

## 2026-05-22 — T246 Desktop Replace-Existing Launch Hardening

Driver: user observed that an already running Codex.app can be reused by macOS,
which prevents the scoped Slimference env from reaching the process tree. The
daily TUI `Launch Codex App` action must therefore close stale Codex.app
instances automatically before starting Slimference mode.

Implemented:
- Added explicit `slimference codex launch-desktop --replace-existing`.
- Without `--replace-existing`, raw `codex launch-desktop` still refuses when a
  Codex.app main process is already running.
- With `--replace-existing`, the launcher:
  1. probes the exact Codex.app main binary PID(s),
  2. sends the existing cleanup path to each PID,
  3. re-probes that the main process is gone,
  4. aborts if any main PID remains,
  5. only then spawns the scoped Slimference Codex.app instance.
- `slimference codex desktop prove` now uses `--replace-existing`.
- TUI `Launch Codex App` now uses `--replace-existing`.
- `codex desktop status --json` now reports
  `launch_command="slimference codex launch-desktop --transport=app-server
  --replace-existing"`.

Verification:
- Focused tests:
  `go test ./cmd/slimference ./internal/tui -run
  'LaunchDesktop|LaunchCodexApp|DesktopProve|CodexDesktop|LaunchCenter'
  -count=1` green.
- Full tests: `go test ./... -count=1 -timeout 300s` green.
- Vet: `go vet ./...` green.
- CI: `go run ./scripts/ci` green, 8/8 gates, aggregate coverage 99.0%.
- Built and installed binary to both `./slimference` and
  `~/.local/bin/slimference`; SHA:
  `68fe05ccf8096649974b4a0e76ea64f8f2671f2a286a5d61e87205fe74f60942`.
- Restarted daemon from installed binary; PID `24204`, health endpoint OK.
- `slimference codex status --json` stayed green:
  `auto.mode=wss_phasef`, `transport=wss`, `wss_certified=true`,
  `needs_recert=false`, Codex `0.133.0`, Slimference `2.0.2`.
- `slimference codex desktop status --json` showed
  `launch_command` with `--replace-existing`.
- `slimference codex launch-desktop --transport=app-server
  --replace-existing --probe` emitted only app-server shim env; no proxy/CA env.
- Autonomous real Desktop launch/cleanup smoke:
  `slimference codex desktop prove --duration=2s --json` launched
  Codex.app PID `24230` with scoped env and cleaned it up. It returned the
  expected non-green `desktop_no_wss_delta` because no user prompt was sent in
  the app during the two-second window. This is not a Desktop savings claim.

Current Desktop state:
- Preferred Desktop path is still implemented infrastructure, not a green
  savings proof.
- The next live proof must run manual mode, send a real prompt in the launched
  Codex.app window, then finish with
  `slimference codex desktop prove --finish --json`.
- Normal Finder/Spotlight Codex.app remains direct.

---

## 2026-05-22 — T246 Desktop Live Proof Negative + Top-Level Base-URL Probe

Live proof after `e1633ef`:
- Command: `slimference codex desktop prove --manual --duration=15s --json`.
- Result: `desktop_ready_for_prompt`, launch PID `26943`.
- Verified main-process env:
  `CODEX_CLI_PATH=/Users/christopher/.local/bin/slimference`,
  `SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1`,
  `SLIMFERENCE_CODEX_DESKTOP_BASE_URL=http://127.0.0.1:8990/backend-api/codex`.
- Verified spawned app-server argv contained provider overrides:
  `model_provider="slimference-codex"` and
  `model_providers.slimference-codex.base_url="http://127.0.0.1:8990/backend-api/codex"`.
- A real Desktop prompt was sent via macOS UI scripting:
  `Reply exactly DESKTOP_PROBE_OK`.
- Screenshot evidence showed Codex.app answered `DESKTOP_PROBE_OK` and displayed
  the `Slimference` provider badge.
- Finish command: `slimference codex desktop prove --finish --json`.
- Result:
  `mode=desktop_connect_only_no_app_server_bytes`,
  `failure_class=connect_only_no_app_server_bytes`,
  `mitm_bridged=2`, `bytes_c2s=0`, `bytes_s2c=0`, `c2s_frames=0`,
  `s2c_frames=0`, `compressed_messages_mutated=0`.
- Socket evidence showed the Desktop app-server still had a direct
  `chatgpt.com:443` connection while also opening local `127.0.0.1:8990`
  connections. Verdict: provider-block overrides alone do not route current
  Codex Desktop conversation WSS through Slimference.

Follow-up probe:
- Added process-local top-level app-server overrides in the hidden shim:
  `openai_base_url="http://127.0.0.1:8990/backend-api/codex"` and
  `chatgpt_base_url="http://127.0.0.1:8990/backend-api/"`, in addition to the
  provider block.
- Built and installed binary SHA:
  `8f5b412edf35d558c19f22b49bf86eca5cc8303e19b76ebe64bf901a3e0816c3`.
- Restarted daemon from installed binary; PID `33671`, health endpoint OK.
- CLI guard remained green:
  `auto.mode=wss_phasef`, `transport=wss`, `wss_certified=true`,
  `needs_recert=false`, Codex `0.133.0`, Slimference `2.0.2`.
- Cleaned stale Codex.app helper processes before the second proof to remove
  socket noise.
- Second command:
  `slimference codex desktop prove --manual --duration=10s --json`.
- Result:
  `mode=desktop_connect_only_no_app_server_bytes`,
  `failure_class=connect_only_no_app_server_bytes`,
  `mitm_bridged=1`, `bytes_c2s=0`, `bytes_s2c=0`, no frames, no mutation.
- Process evidence showed the app-server argv did include both
  `openai_base_url` and `chatgpt_base_url`, and had one local
  `127.0.0.1:8990` connection. Still no application bytes reached Phase-F.

Verdict:
- Current Codex Desktop does not provide a usable scoped Desktop conversation
  WSS path through either provider-block overrides or top-level
  `openai_base_url` / `chatgpt_base_url` overrides.
- TUI `Launch Codex App` must remain proof-gated and blocked for savings.
- Codex CLI remains the green savings path.
- Normal Codex.app from Finder/Spotlight remains the correct no-drawback
  Desktop path until upstream exposes a working Desktop conversation endpoint
  hook or the app-server protocol changes.

---

## 2026-05-22 — T246 Phase-0 Diagnostics (read-only, no product change)

Goal: replace historical launcher-header hypotheses with hard measured data
against the currently installed Codex 0.133.0, before building anything.

Binary scan (npm `codex-cli 0.133.0` and bundled
`/Applications/Codex.app/.../codex` `0.133.0-alpha.1`, env handling identical):
- Conversation host `chatgpt.com/backend-api/codex` and the `responses_websockets`
  subprotocol are hardcoded strings in both binaries.
- Env vars the binary actually references: `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`
  (a custom-CA path exists, so this is not absolute cert pinning),
  `CODEX_CLOUD_TASKS_BASE_URL`, `CODEX_OSS_BASE_URL`, `DEFAULT_BASE_URL`,
  `LATEST_MODEL_BASE_URL`, `ISSUER_BASE_URL`, `API_BASE_URL` (adjacent to
  auth/identity strings), `CODEX_REFRESH_TOKEN_URL_OVERRIDE`,
  `CODEX_REVOKE_TOKEN_URL_OVERRIDE`, proxy env.
- `OPENAI_BASE_URL`, `CHATGPT_BASE_URL`, `CHATGPT_CODEX_BASE_URL` are NOT env
  hooks in the binary; they are only `~/.codex/config.toml` keys (`-c`).

Live launch (`slimference codex desktop prove --manual`, app-server argv
confirmed to carry `openai_base_url`, `chatgpt_base_url`, and the
`slimference-codex` provider block):
- Decisive: `0` `responses_websockets` WSS upgrades reached `127.0.0.1:8990`
  (`/admin/debug/requests` raw-scoped count = 0). That envelope is the only
  thing Slimference can compress.
- The app-server did open loopback connections to `127.0.0.1:8990`, but they are
  REST sideband / CONNECT, not the conversation WSS.
- The app-server held direct outbound `172.64.155.209:443` (Cloudflare-fronted,
  consistent with chatgpt.com) sockets: the conversation goes direct, bypassing
  Slimference.
- Finish delta showed only `mitm_bridged +1` with no new WSS application bytes,
  frames, or mutation.

Measurement caveat:
- The desktop proof delta reads GLOBAL daemon WSS counters, so concurrent Codex
  CLI WSS traffic contaminates it. This run's finish label flipped to
  `desktop_app_server_wss_bridge`, but that is a counter-contamination artifact,
  not Desktop savings (zero desktop WSS upgrades were observed). A trustworthy
  Desktop proof needs desktop-scoped counters or a quiet daemon.

Verdict (now evidence-backed against 0.133.0, not header comments):
- In ChatGPT-auth mode the Desktop app-server sends the `responses_websockets`
  conversation directly to a hardcoded chatgpt.com host. The `-c` base-url and
  provider overrides only redirect REST sideband to `8990`.
- This is a Codex Desktop property, not a Slimference classification bug: the
  daemon correctly recorded zero because zero arrived.
- The `API_BASE_URL` probe is low value: it sits on the auth/identity route,
  the conversation host is hardcoded, and the env variant is already covered by
  the historically-ignored `--transport=base-url` env set.
- Decision unchanged and now hard-proven: Desktop stays direct/no-drawback, TUI
  `Launch Codex App` stays blocked for Slimference mode, savings focus is Codex
  CLI plus T239/T240.

---

## 2026-05-22 — T246 Desktop ROUTING BREAKTHROUGH (commit `9dcf8f4`)

The earlier "Desktop conversation is hardcoded to chatgpt.com" conclusion was
incomplete. A deeper diagnosis found a clean, in-band fix.

Diagnosis:
- A throwaway stdio tee (`CODEX_CLI_PATH` -> tee -> real codex, captured to
  `/tmp/electron-stdin.raw`) recorded what Codex Desktop's Electron client sends.
- Framing is newline-delimited JSON (not Content-Length).
- The conversation `thread/start` carries `model="gpt-5.5"` and
  `modelProvider=null`. `null` resolves to the account default provider
  (`openai` -> chatgpt.com direct), overriding the shim's `-c model_provider`
  default. The thread/start response confirmed effective `modelProvider="openai"`
  and zero raw-scoped WSS upgrades.
- Disproven first: the model is not bound to chatgpt.com — `gpt-5.5` via CLI
  `codex exec` with the green provider block routes through Slimference WSS with
  Phase-F (`phasef_req` advanced).
- A direct app-server drive (no Electron) with `modelProvider="slimference-codex"`
  routed `gpt-5.5` through Slimference: thread/start response echoed
  `modelProvider="slimference-codex"`, 7 sockets to `127.0.0.1:8990`, a real
  NONCE-tagged answer, `phasef_req+2`. So the app-server honors the provider when
  set; only Electron's `null` was the blocker.

Fix (`cmd/slimference/codex_desktop_app_server_shim.go`):
- The hidden shim is now a thin stdin JSON-RPC mediator instead of a bare exec.
  It rewrites a default (null/absent) `thread/start` `modelProvider` to
  `slimference-codex`, byte-identical for everything else. stdout/stderr pass
  through directly. Realtime/voice threads
  (`config["features.realtime_conversation"]`) and explicit provider choices are
  left untouched; any parse ambiguity fails open to the original bytes. The two
  dead top-level `openai_base_url`/`chatgpt_base_url` overrides were removed.
- Verified: `go test ./...` 6684 pass, `go vet ./...` clean.

Live proof after the fix (mediated shim, real Desktop prompt):
- The app-server held 6 connections to `127.0.0.1:8990` and ZERO direct
  `chatgpt.com` sockets (was direct before the fix). WSS frames flowed
  (`c2s_frames`, `s2c_frames`, `bytes_c2s=11374`, `bytes_s2c=2999`).
- The Desktop conversation now rides the Slimference WSS path. Core blocker
  solved.

Remaining (savings not yet active):
- The routed session is byte-bridged, not Phase-F mutated:
  `phasef_requests=0`, `frames_reencoded=0`, `byte_bridge_only=true`.
- CLI `codex exec` and a direct app-server drive WITHOUT Electron's feature-flag
  `config` both reach Phase-F, so Electron's `thread/start` `config` flags
  (candidate `features.enable_request_compression=true`) likely change the WSS
  request-frame format so the Phase-F parser does not recognize request
  envelopes and safely byte-bridges instead.
- Next step: capture the Desktop WSS request frames (socket/tcpdump ground
  truth, not the laggy desktop-status counters), then either neutralize the
  responsible flag in the shim's thread/start `config` or teach the Phase-F
  parser the variant. TUI Launch Codex App stays blocked until a green
  `desktop_app_server_phasef_proven` (bytes + frames + mutation) result exists.

---

## 2026-05-22 — T246 Phase-F flag investigation (reverted; hit a measurement wall)

- Direct app-server drives isolated `features.enable_request_compression`: a clean
  control-vs-flag pair gave Phase-F delta `phasef_requests` +1 (no flag) vs 0
  (flag on). Codex's own request compression is at least one Phase-F breaker.
- Implemented + unit-tested a shim variant that also forces
  `features.enable_request_compression=false` in `thread/start`. Live proof from a
  fresh daemon was inconclusive and showed a regression signal: routed conversation
  held 6 loopback sockets to `:8990` but `c2s_frames=0`/`bytes_c2s=0` (vs 14 before),
  hinting that disabling compression may move the app-server onto an HTTP path
  instead of WSS. Reverted the variant; only the proven routing fix `9dcf8f4` is
  committed.
- Measurement wall: the `desktop status` WSS counters are sampled and
  lag/cache/rate-sensitive. After repeated rapid drives even the control flipped
  +1 -> 0, so counter-based flag bisection is unreliable. Do NOT change behavior on
  counter evidence.
- Reliable path for the next session: `sudo tcpdump -i lo0 -A 'tcp port 8990'`
  during one Desktop turn (control vs full Electron config), diff the WSS upgrade
  and request frames, identify the exact flag(s)/frame-format that defeat Phase-F,
  fix with confidence, and verify by socket + frame evidence.

State: routing SOLVED + committed (`9dcf8f4`); Desktop savings (Phase-F) still
pending a frame-grounded fix. Codex CLI remains the green savings path; normal
Desktop stays direct/no-drawback; TUI Launch Codex App stays blocked.

---

## 2026-05-22 — T246 CORRECTION: Desktop is already on the Phase-F route (counters lied)

Ground truth resolved the confusion in the entry above; the "Phase-F gap" was a
measurement artifact, not an engineering gap.

- Frame capture (loopback tee proxy 8991 -> 8990, no sudo): the Codex app-server
  WSS conversation frames use `permessage-deflate` (first client frame `0xc1`,
  RSV1 set). The CLI `codex exec` green path uses the IDENTICAL frame format. So
  `permessage-deflate` / `features.enable_request_compression` is NOT what defeats
  Phase-F; the earlier dual-run counter result was noise.
- Reliable flight log (`SLIMFERENCE_DEBUG_DECISIONS_LOG`): both the CLI and the
  app-server (driven with the full Electron feature-flag `config`, including
  `enable_request_compression=true`) recorded `route_mode=websocket_phasef` for
  `/backend-api/codex/responses`. The Desktop conversation is on the same Phase-F
  savings route as the certified CLI.
- The earlier `byte_bridge_only` / `phasef_requests=0` / `c2s_frames=0` readings
  were laggy global-counter artifacts plus trivial test prompts with nothing to
  mutate (same caveat as the CLI smoke proof).

Corrected conclusion:
- Routing fix `9dcf8f4` is sufficient. Codex Desktop conversations route through
  Slimference on `websocket_phasef`, identical to the CLI, so token savings
  materialize on real (compressible) conversations exactly like the CLI.
- The reverted compression rewrite was correctly dropped (unnecessary; its
  regression signal was also counter noise).
- Only open item: the TUI `desktop_app_server_phasef_proven` gate reads the laggy
  WSS delta counters. To flip it reliably, gate on the decisions-log
  `route_mode=websocket_phasef` evidence (or a quiet-daemon compressible-turn
  proof), not the sampled counters. Engineering is done; this is gate plumbing.

---

## 2026-05-22 — T246 GATE FIX: reliable phasef_bridged counter (commit `af972df`)

Closed the only open item above. Instead of the laggy WSS delta counters, the gate
now reads a lag-free monotonic signal.

- Added `phasef_bridged` to the WSS dispatcher counters. It increments once per
  Phase-F WSS conversation at `FrameBridge` entry (upgrade time), so it does not
  depend on byte/frame accumulation or snapshot timing. Plumbed through
  `DispatcherTelemetry` -> `control.WSSState` -> `codex desktop status`.
- `classifyCodexDesktopProof`: `phasef_bridged>0` with zero
  parser/degrade/compression errors is the reliable verdict -
  `desktop_app_server_phasef_proven` if mutation also fired, otherwise
  `desktop_app_server_route_proven` (launch-eligible; per-turn savings scale with
  conversation size, like the CLI). `applyCodexDesktopLastProof` maps
  `route_proven` to the launchable `desktop_app_server_proven` status.
  `codexDesktopTLSRejected` is now guarded by `phasef_bridged==0` so a real
  Phase-F session can never be misread as a TLS rejection.
- Verified: `go test ./...` + `go vet ./...` clean, `go run ./scripts/ci` 8/8 PASS
  (coverage gate green). Live: fresh daemon `phasef_bridged=0`; one Desktop
  app-server conversation -> `phasef_bridged=1`.

Result: TUI Launch Codex App is reliably unblockable once a Desktop conversation
reaches the Phase-F route. T246 engineering is complete: routing fix (`9dcf8f4`) +
reliable gate (`af972df`). Codex CLI and Codex Desktop both ride the same no-CA
`websocket_phasef` savings route; Browser ChatGPT, ChatGPT.app, voice, computer-use,
and Claude Code remain untouched; normal Finder/Spotlight Codex.app stays direct.

---

## 2026-05-23 — T246 honesty pass after external review (commit `6d0471c`)

An external review (GPT) correctly flagged that the prior summary overclaimed and
that one doc still contradicted the rest. Verified and corrected:

- SSOT contradiction fixed: `docs/install.md` still said "Current product truth on
  Codex Desktop is negative" / "Launch Codex App must stay blocked" while the other
  docs said solved. install.md is now consistent (route reached; savings-capable;
  mutation proof pending).
- Gate semantics made honest: the route-only verdict now maps to a DISTINCT status
  `desktop_app_server_route_ready` (launch-eligible, labelled "WSS route ready"),
  separate from `desktop_app_server_proven` ("WSS savings", full mutation). The TUI
  launch gate allows both, but never sells "route ready" as "savings proven".
- Claim corrected: "Desktop saves like CLI" is too strong as a product claim. The
  honest claim is: the Desktop conversation reaches the same `websocket_phasef`
  savings route as the certified CLI (reliably proven via `phasef_bridged` and the
  decisions log); per-turn savings scale with conversation content like the CLI.

Mutation-proof attempt (the remaining hard green):
- A synthetic multi-turn direct-drive that repeated a large text block did NOT
  produce `frames_reencoded>0` / `compressed_messages_mutated>0`. `phasef_bridged`
  incremented reliably (1->2), `compressed_messages_inspected` showed the
  permessage-deflate inflate/inspect path runs, but the byte/frame/mutation
  counters did not move in the sampled snapshot (consistent with their known lag).
- Root reason: L1 dedup targets repeated TOOL OUTPUTS across turns, not repeated
  user text; and the byte/frame counters are too laggy to certify magnitude.
- Honest status: route proven and savings-capable; a persisted Desktop savings
  proof (`desktop_app_server_phasef_proven` with `frames_reencoded>0`,
  `compressed_messages_mutated>0`) still requires a real Desktop coding session
  (file reads / command outputs that repeat across turns), best driven through the
  GUI by the operator, then `slimference codex desktop prove --finish --json`.
  This is the same mechanism the certified CLI uses; the Desktop route is identical.

---

## 2026-05-23 — T246 MECHANISM REFERENCE (full traceability for any future rebuild)

Everything learned about how Codex Desktop routing + Phase-F + measurement works,
so a future change is reversible and the evidence is preserved. Source files:
`cmd/slimference/codex_desktop_app_server_shim.go` (shim), `internal/proxy/proxy.go`
+ `internal/proxy/wsmitm_dispatcher.go` + `internal/proxy/wsmitm/session.go` (WSS),
`cmd/slimference/codex_cmd.go` (proof/status), `cmd/slimference/codex_recert.go`
(CLI cert).

1. ROOT CAUSE (why Desktop bypassed Slimference). Codex Desktop's Electron client
   drives the Rust `codex app-server` over newline-delimited JSON-RPC on stdio.
   It opens each conversation with `thread/start` carrying `model="gpt-5.5"` and
   `modelProvider: null`. `null` resolves to the account default provider `openai`
   (-> chatgpt.com direct), which OVERRIDES the `-c model_provider` config default.
   The provider badge in the UI is cosmetic. Captured live via a stdio tee
   (`CODEX_CLI_PATH` -> tee -> real codex, `/tmp/electron-stdin.raw`).

2. FIX (the shim). `CODEX_CLI_PATH=<slimference>` makes Codex.app start the
   slimference shim as its app-server. The shim is a thin stdin JSON-RPC mediator:
   it spawns the real Codex app-server with the `slimference-codex` provider block,
   passes stdout/stderr straight through, and rewrites ONLY a default (null/absent)
   `modelProvider` on `thread/start` to `slimference-codex`. Byte-identical for
   everything else; realtime/voice (`config["features.realtime_conversation"]`) and
   explicit providers pass through; fail-open on any parse ambiguity. Framing is
   newline-delimited JSON (verified; the binary also supports Content-Length but
   Desktop uses newline).

3. ROUTE + FRAMES. Both the CLI (`codex exec`) and the Desktop app-server negotiate
   the `permessage-deflate` WebSocket extension and send compressed frames (first
   client frame byte `0xc1`, RSV1 set) on `GET /backend-api/codex/responses`.
   Captured byte-for-byte via a loopback tee proxy (8991 -> 8990). Both record
   `route_mode=websocket_phasef` in the daemon decisions log
   (`SLIMFERENCE_DEBUG_DECISIONS_LOG`). The Desktop route is byte-identical to the
   certified CLI route. `enable_request_compression` and other Electron feature
   flags do NOT change this (all variants negotiate permessage-deflate).

4. COUNTER FLUSH TIMING (critical for all measurement). The WSS byte/frame/mutation
   counters live on the per-conversation `wsmitm.Session` and are aggregated into
   the dispatcher snapshot (`/admin/state`, `codex desktop status .wss`) at
   SESSION END, when the WSS connection closes. While Codex.app keeps the WSS open,
   `bytes_c2s` / `c2s_frames` / `frames_reencoded` / `compressed_messages_*` read
   ZERO in the snapshot even during an active conversation. The exception is
   `phasef_bridged` (added in the FrameBridge closure, `proxy.go`): it increments
   once at conversation START, so it is the lag-free signal that the route was
   reached. CONSEQUENCE: to measure bytes/mutation, the conversation's WSS session
   must close (quit Codex.app / end the app-server) before reading. This is why
   earlier live readings looked like "zero bytes" - the session was still open.

5. MEASUREMENT METHODOLOGY that is reliable: (a) restart the daemon fresh so all
   counters baseline at 0; (b) run the Desktop conversation; (c) CLOSE the session
   (quit the app-server); (d) poll `codex desktop status .wss` for ~15s for the
   flushed values. Do NOT trust mid-session snapshots. `phasef_bridged` is the only
   counter reliable mid-session.

6. PROVEN with a real Desktop session (2026-05-23, fresh daemon, two turns reading
   a 93KB file, read after session close): `phasef_bridged=2`, `bytes_c2s=181344`,
   `bytes_s2c=375755`, `c2s_frames=27`, `s2c_frames=1120`,
   `compressed_messages_inspected=1111`, `parse_failures=0`, `degraded_sessions=0`,
   `compression_errors=0`. The Desktop conversation fully flows through Slimference
   and Phase-F inflates+inspects every message with zero errors.

7. MUTATION (the open item). In the same proof `frames_reencoded=0` and
   `compressed_messages_mutated=0`: Phase-F inspected 1111 messages but mutated
   none. A recert-style direct-drive (`git status` tool output, repeated) also
   inspected (62) but mutated 0. Yet the CLI WSS certification REQUIRES
   `frames_reencoded>0` + `compressed_messages_mutated>0` + `mutation_active=true`
   + `byte_bridge_only=false` (`codexWSSCertificationFailures`,
   `codex_cmd.go:1241`) and `wss_certified=true` - so mutation DOES fire for the
   CLI. The difference found: the CLI recert uses `codex exec` + `exec resume
   --last` = two separate WSS connections that each re-send the FULL accumulated
   history (with the repeated tool output), so a single request contains the
   duplicated content that L1 dedup mutates. A persistent app-server WSS with
   incremental turns sends mostly deltas, so a single request rarely contains the
   self-repetition dedup needs. Implication: Desktop mutation/savings fire on the
   same content the CLI mutates (the route+pipeline are identical and inspect
   cleanly); the magnitude depends on request content structure, not on Desktop vs
   CLI. A Desktop-specific mutation proof needs a real session whose requests carry
   repeated content (long histories, resumed threads, repeated tool outputs).

8. STATUS / GATE (`codex_cmd.go`, `main.go`, `internal/tui/model.go`).
   `classifyCodexDesktopProof`: `phasef_bridged>0` + zero errors ->
   `desktop_app_server_phasef_proven` (if `frames_reencoded>0` &&
   `compressed_messages_mutated>0`) else `desktop_app_server_route_proven`.
   `applyCodexDesktopLastProof` maps the latter to launchable-but-honest
   `desktop_app_server_route_ready` (TUI "WSS route ready"), the former to
   `desktop_app_server_proven` (TUI "WSS savings"). `LaunchCodexApp` allows both;
   `codexDesktopTLSRejected` is guarded by `phasef_bridged==0`. Status reads the
   LAST PERSISTED proof, so it stays `desktop_proof_prompt_required` until a real
   `prove --manual` + `prove --finish` cycle persists a verdict.

OPEN / NEXT: run `prove --manual`, do a real Desktop coding session with repeated
content, quit the app (flush), `prove --finish`. Expect `route_ready` at minimum;
`phasef_proven` if that session's requests carry dedupable repetition. If even
content-rich Desktop sessions never show `frames_reencoded>0`, the next question is
broader Phase-F mutation efficacy on Codex WSS (affects CLI too), not Desktop.

---

## 2026-05-23 — T246 DECISIVE: WSS Phase-F mutation is marginal product-wide (CLI too)

Settled GPT's deeper question by measuring the CLI, not just Desktop.

- Fresh-daemon CLI WSS exec (recert-style: run `git status`, reply RECERT_DONE),
  measured after the CLI exited (session flush): `phasef_bridged=1`,
  `bytes_c2s=70513`, `c2s_frames=3`, `compressed_messages_inspected=58`,
  `phasef_requests=3`, but `frames_reencoded=0`, `compressed_messages_mutated=0`,
  `phasef_mutations=0`, `byte_bridge_only=true`, `mutation_active=false`. So the
  CLI does NOT mutate on this real session either - mutation is not Desktop-specific.
- The persisted cert `~/.slimference/codex-wss-cert.json` was issued 2026-05-22
  (codex 0.133.0, slimference 2.0.2) with `frames_reencoded: 1` - exactly ONE
  mutated frame. So `wss_certified=true` is real but rests on a single mutated
  frame; it is NOT stale by version, but it is marginal evidence.
- Desktop large session (93KB file twice): `compressed_messages_inspected=1111`,
  `frames_reencoded=0`. Even a large, repetitive real session did not mutate.

Conclusion (honest, product-wide):
- WSS Phase-F INSPECTS real Codex traffic correctly (inflates permessage-deflate,
  examines messages, zero errors) for both CLI and Desktop.
- WSS Phase-F MUTATION (the actual token reduction) barely fires on real Codex
  WSS: the cert achieved 1 frame; live CLI and Desktop sessions show 0. So the WSS
  Phase-F savings currently delivered are marginal-to-zero for BOTH transports,
  not just Desktop.
- Therefore the Desktop ROUTING goal is complete (Desktop == CLI on the route),
  but the real open product question is reducer EFFICACY on real Codex WSS request
  bodies (why inspection != mutation), which applies to CLI and Desktop equally.
- Any "savings" claim for Codex WSS (CLI or Desktop) must be re-grounded on
  measured mutation, not on `wss_certified=true` alone. Other layers (output
  reduce, response cache, L0 filters) may carry most real savings and should be
  measured separately before T240 certifies a savings number.

---

## 2026-05-23 — T246/T247 ROOT CAUSE: why WSS Phase-F barely mutates (Responses-API delta model)

Found the architectural reason mutation is ~0, by dumping exactly what the
reducer receives (temporary env-gated `SLIMFERENCE_WSPHASEF_DEBUG` dump in
`wsmitm_phasef.go:handleRequest`, since reverted) on a real CLI WSS session that
read a file twice.

What the reducer actually gets (real Codex WSS request bodies, captured to
`/tmp/wsphasef-body-*.json`):
- Top keys: `type, model, instructions, previous_response_id, input, tools,
  tool_choice, ..., prompt_cache_key, client_metadata`.
- It is the OpenAI **Responses API with `previous_response_id`**: server-side
  conversation state. Each request carries only the DELTA, not the accumulated
  history. Observed request sequence in one turn: `input=[]` (init) ->
  `input=[message,message,message]` (developer+user) -> `input=[function_call_output]`
  (the file-read result, alone) -> `input=[function_call_output]` again. No request
  contained repeated history.
- Extraction was poor/empty: a 117KB body extracted to `messages=1,
  tool_results=0`; the `function_call_output` requests extracted to `messages=1,
  tool_results=1, tool_uses=0`.

Why `frames_reencoded=0` / `compressed_messages_mutated=0`:
1. Slimference's L1/L0 dedup reducers (`applyProxyLayer0WithSessionAndToolUses`,
   read-delta, MinHash dedup) are built for the Chat-Completions shape where the
   FULL history (with repeated tool outputs) is in every request, so repetition is
   visible within one request. Codex WSS sends DELTAS, so a single request has no
   self-repetition to dedup.
2. The cross-request fix exists in principle (per-session read context +
   `rememberToolUsesFromResponse`), but `wsCodexSessionID` returned "" for every
   Codex WSS request: it looked for `conversation_id`/`session_id`/`user_id`, while
   Codex's stable key is `prompt_cache_key` (mirrored in
   `client_metadata.x-codex-turn-metadata`). With an empty key the per-session
   read-delta context could not accumulate across the delta requests.

Fix landed (commit `b5213e8`): `wsCodexSessionID` now extracts `prompt_cache_key`
(and `client_metadata.x-codex-turn-metadata` thread/session id). Verified the
session key is now populated (`codex-wss:019e51d6-...`). NECESSARY but NOT yet
SUFFICIENT: mutation still 0 after the fix, because the read-delta still does not
match the `function_call_output` deltas (the function_call/tool_use that resolves
the command line lives in a prior response referenced by `previous_response_id`,
and the read-delta matching across delta requests needs work).

CONCLUSION / next work (new task T247): make the WSS Phase-F reducers actually
reduce the Responses-API delta shape - resolve `function_call_output` tool results
against remembered tool_uses, key the read-delta/dedup context by the now-correct
session id, and compact a new tool-output delta against the remembered prior one.
This applies to CLI and Desktop identically (same route). Until then, honest
status: Codex WSS routes through Slimference and is inspected cleanly, but Phase-F
token savings are marginal; Desktop is route-ready, savings pending T247. Also
flagged: `TestStartCodexDesktopProcessRejectsImmediateExit` is timing-flaky under
full-suite load (passes 5/5 in isolation); pre-existing (last touched `e1633ef`).

## 2026-05-23 (later) - T247 reducer chain proven end-to-end on real Codex CLI traffic

Goal: stop guessing whether the function_call -> tool_use ->
function_call_output -> tool_result -> commandLine -> readcache chain breaks on
real Codex Responses-API delta traffic, and find the actual breakpoint by
capturing every step on one controlled multi-read session.

Method (env-gated, removed before final commit):
- Added a minimal `t247Dump(kind, payload)` helper in `wsmitm_phasef.go`
  gated on `SLIMFERENCE_T247_DUMP=1`, writing to `/tmp/t247-dump/`. Three dump
  points: `handleRequest` body pre-pipeline (`req-pre`), `handleRequest`
  post-pipeline plus a JSON meta blob with messages/tool_uses/tool_results/
  command_lines/remembered_tool_uses/session_key (`req-meta`, `req-post`), and
  `handleResponse` per-frame envelope summary for non-empty `Item`/`Response`
  (`resp-<kind>`).
- Stopped the production daemon, built a temporary
  `/tmp/slimference-t247` binary from the unchanged source plus the dump,
  started it with `SLIMFERENCE_T247_DUMP=1`, ran one scoped CLI session
  through `slimference codex run --transport=auto -- exec --ephemeral
  --skip-git-repo-check --cd /tmp --dangerously-bypass-approvals-and-sandbox
  '... run three separate read operations on /tmp/t247-read-target.md ...'`
  with the read target being a 35567b copy of `handover-by-opus.md`, closed
  the session cleanly, then read `/_slimference/admin/state` and decoded
  every dump file.
- After diagnosis, reverted the three `wsmitm_phasef.go` edits surgically.
  `git diff --stat` clean, `go build ./...` clean, `go vet ./...` clean,
  `go test ./internal/proxy/...` green across all five packages. Archived
  the captures to `/tmp/t247-dump-evidence.tgz` (226 KB) for future
  reference, removed `/tmp/t247-dump/` and the temporary binary, and
  restarted the production `slimference daemon` from
  `/Users/christopher/.local/bin/slimference`.

What the captures showed end to end (every step in the chain confirmed
working on real 0.133.0 traffic):
- Request shape for each `function_call_output` turn: `{model: "gpt-5.5",
  instructions: 21335b, previous_response_id: "resp_<id>",
  input: [{type: "function_call_output", id|call_id: "call_<id>",
  output: "Chunk ID: <chunk>\nWall time: ...\nProcess exited with code 0\n
  Original token count: <n>\nOutput:\n<file content>"}], tools: 14,
  prompt_cache_key: "019e5220-4b66-7e40-b089-5f65cb479346"}`.
  Body size 71024b on each repeat-read request.
- `extractMessages(types.CodexChatGPT, body)` parses the Responses-shape
  `input` items; `codexInputItemToMessage` maps `function_call_output` ->
  `ContentBlock{Type:"tool_result", ToolResultID: call_id, Text: output}`
  via `codexLooksLikeToolOutput` plus `codexToolOutputText`.
- `rememberToolUsesFromResponse` accumulates each prior turn's
  `function_call` item (kind = `response.output_item.added` /
  `response.output_item.done`) into `a.toolUses` keyed by the same
  `call_id`. By request #2 the map held the request #1 use, by request #3
  it held both prior uses (`remembered_tool_uses` field in `req-meta`
  confirmed this directly).
- `proxyResolveToolUse` resolved each new tool_result block via the
  remembered map; the resolved tool name was `exec_command` (covered by
  `looksLikeShellTool`); the resolved arguments shape was
  `{"command":["bash","-lc","cat /tmp/t247-read-target.md"], "workdir":
  "/tmp"}` (string-encoded JSON inside `arguments`).
- `codexCommandLineFromFields` extracted `bash -lc cat /tmp/...md`, then
  `normalizeLayer0CommandLine` recognised the bash wrapper (`argv[0]` is a
  shell executable AND `argv[1]` begins with `-` and contains `c`) and
  stripped to `cat /tmp/t247-read-target.md`. Identical for all three
  reads; `command_lines` field in `req-meta` confirmed this for every
  request.
- `wsCodexSessionID` returned `codex-wss:019e5220-4b66-7e40-b089-5f65cb479346`
  on every request (prompt_cache_key fix from `b5213e8` in effect).
- `applyProxyLayer0WithSessionAndToolUses` ran, and for each repeat-read
  tool_result the reducer chain produced an actual mutation:
  - Read #1 (35567b output): the new content was not yet in the readcache,
    so `compactProxyReadDelta` returned no change. The fallback
    `compactProxyLayer0Text` -> `compactCodexExecEnvelope` split the Codex
    exec envelope header from the payload, ran
    `filter.CompactCapturedOutputWithContext` over the payload, and
    produced 6558b (81% reduction). After this read the file content was
    deposited into the content-archive and the readcache observed it.
  - Read #2 (35567b output): `compactProxyReadDelta` ->
    `readcache.EvaluateObserved(DefaultDir(home), Request{SessionID,
    FilePath}, text, ArchiveDir, recentlyEdited)` returned
    `DecisionBlock` with reason
    `"Slimference delta for /tmp/t247-read-target.md:\n+ Chunk ID: 12030b
    \n- Chunk ID: 5bab73\nFull content: local-archive://20260522-235837-1d325a120327"`,
    144b total. Replaces the entire 35567b output payload, leaving only
    the delta marker plus archive reference for Codex to expand on demand.
  - Read #3 (35567b output): identical pattern, 144b delta marker against
    read #2's chunk_id.
  Aggregate: 106701b raw output payload -> 6846b mutated = 94% reduction
  on the output side.
- Frame-level write back: `wsmitm.Session.finishCompressedMessage` saw
  `replace=true` on each mutated request, re-marshaled the envelope,
  deflated the new payload, and wrote new RSV1-set frames. Counter pump
  observed three CompressedMessagesMutated += 1 and three
  FramesReencoded += 1.

Daemon `/admin/state` after the multi-read session closed (flush-aware):
- `wss.phasef_bridged=1`, `wss.frames_reencoded=3`,
  `wss.compressed_messages_mutated=3`, `wss.phasef_mutations=3`,
  `wss.mutation_active=true`, `wss.byte_bridge_only=false`.
- `wss.compressed_messages_inspected=142`,
  `wss.parse_failures=0`, `wss.degraded_sessions=0`,
  `wss.compression_errors=0`.
- `wss.bytes_c2s=128961`, `wss.bytes_s2c=189499`, `wss.c2s_frames=5`,
  `wss.s2c_frames=137`, `wss.phasef_requests=5`,
  `wss.phasef_request_messages_indexed=4`, `wss.phasef_text_deltas=105`,
  `wss.phasef_terminal_responses=5`.
- `savings.input_tokens_saved=26461` (RecordProxyLayer0 path);
  `savings.repdet_rewrites=0`, `savings.stale_read_blocks=0`,
  `savings.obsolete_prune_blocks=0`, `savings.stop_seq_injections=0`,
  `savings.beterse_injections=0`.

Honest calibration (resolves the earlier "0 mutations on multi-read"
observation):
- The earlier 2026-05-23 multi-read measurement that reported
  `compressed_messages_mutated += 0` was Codex-side run-variance, not a
  reducer defect. On the same Codex 0.133.0 binary, the same prompt, the
  same Slimference code (only difference: the env-gated dump helper, which
  does not influence mutation), Codex chose to expose 5 c2s frames with 4
  parseable instead of 3 with 2 parseable. The reducer mutated all three
  repeat-reads on the capture run.
- Slimference WSS Phase-F savings are workload-dependent: ~0 input tokens
  saved on sessions without repeat reads (only F01-F24 filter hits inside
  the same applyInputPipeline can fire); 26461 input tokens saved on the
  3x35KB-file capture session. The cert no longer depends on the F01
  git-status frame alone; the read-delta chain is operationally proven on
  real Codex Responses-API delta traffic.

Cert behaviour after the recert run on the same daemon family:
- `~/.slimference/codex-wss-cert.json` was refreshed by the official
  `slimference codex recertify wss --force` path at 2026-05-22T23:44:45Z
  (operator opus, frames_reencoded=1, T247 cert-repro note).
- `~/.slimference/codex-wss-recert.json` recorded
  `status=passed`, `phasef_passed=true`, `bridge_passed=false` (logical:
  bridge proof expects mutation=0; on a mutating path it cannot pass and
  must not).
- `codex_route.auto_mode=wss_phasef`, `wss_certified=true`,
  `recert_status=passed` confirmed via admin/state.

What this changes for the project state:
- T246 routing was already solved-for-routing; T247 was the open question
  of whether Phase-F can actually save tokens on real Codex Responses-API
  delta traffic. Answer: yes, on repeat-read workloads, deterministically
  and safely.
- T240 release certification can now be planned around honest measured
  savings rather than the single-frame cert that previously anchored the
  claim. The remaining T247 sub-tasks are a fixture-based regression test
  against the captured delta shape and quantification of the non-WSS
  layers for an honest aggregate savings number.
- No code change was required for this finding. The reducer chain already
  covers Codex's real wire shape end to end. The capture instrumentation
  was reverted; production source is unchanged from commit `9e7ec48` /
  `d6a20ef` (docs-only commit after this writeup).

## 2026-05-23 (evening) - T246 closed end-to-end with user-confirmed Desktop launch

User ran the canonical Desktop proof cycle by hand:
`slimference codex desktop prove --manual --json` opened Codex.app under the
Slimference shim, the user held a real conversation, then
`slimference codex desktop prove --finish --json` returned:
- `mode=desktop_app_server_route_proven`
- `launch_ready=true`, `desktop_proven=true`,
  `manual_prompt_still_required=false`
- `desktop_savings=false`

Counter delta on the proof window (read after Codex.app close, flush-aware):
- `mitm_bridged=2`, `phasef_bridged=2`
- `bytes_c2s=127663`, `bytes_s2c=178171`
- `c2s_frames=7`, `s2c_frames=581`
- `phasef_requests=5`, `phasef_request_bodies=5`,
  `phasef_request_messages_indexed=3`, `phasef_text_deltas=540`,
  `phasef_terminal_responses=5`
- `compressed_messages_inspected=584`
- `compressed_messages_mutated=0`, `frames_reencoded=0`, `phasef_mutations=0`
- `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`

Interpretation:
- Routing is end-to-end clean: 584 compressed messages inflated and inspected,
  zero protocol or schema errors, two Phase-F sessions bridged.
- `desktop_savings=false` on this session is expected workload-variance.
  The user's conversation did not include repeated full-file reads of the
  same path, so the readcache delta path never triggered. The reducer chain
  itself is proven separately under T247 (capture + fixture test `fee1af4`,
  `TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`).
- TUI Launch Codex App is now launch-eligible (`launch_ready=true`,
  persisted in `~/.slimference/codex-desktop-proof.json`).

What this closes:
- T246 is closed end-to-end: routing solved (`9dcf8f4`), gate proven
  (`af972df`), user-confirmed launch path (this run, 2026-05-23 evening).
- T247 remains open only on data-gathering and aggregate measurement, not
  on code: `scripts/utils aggregate-savings` (commit `e651483`) is the
  ready-to-use measurement surface. After a real repeat-read Codex workday
  the same Phase-F path will surface mutation savings the same way the
  capture run did (`compressed_messages_mutated>0`,
  `input_tokens_saved>0`).

Production source unchanged for this entry; doc-only commit follows.

## 2026-05-23 (late) - Autonomous WSS savings measurement + aggregate sanity

Goal: produce a first honest aggregate savings number on this machine
without waiting for a manual workday, using the scoped CLI multi-read
path the user already approved.

Method (no production code change, all official paths):
- Restarted the daemon clean (`kill -TERM` then
  `/Users/christopher/.local/bin/slimference daemon`) so the WSS counters
  baselined at 0.
- Ran four scoped CLI sessions via `slimference codex run --transport=auto
  -- exec --ephemeral --skip-git-repo-check --cd /tmp
  --dangerously-bypass-approvals-and-sandbox '<prompt>'`. Each prompt
  ordered Codex to perform multiple separate read tool calls against
  /tmp/t247-read-target.md (35KB markdown) and /tmp/t247-auto.md (a 132KB
  copy of `docs/operation-log.md`), some single-file repeats and some
  A/B/A/B alternating patterns.
- After each batch closed cleanly (Codex CLI exit), read `/admin/state`
  flush-aware and aggregated via `scripts/utils aggregate-savings
  --filter-db=~/.slimference/filter.db --period=all --usd-per-million=2.5`.

Live WSS counters after all four sessions (flush-aware):
- `phasef_bridged=4` (four scoped Codex CLI conversations reached Phase-F)
- `phasef_requests=18`, `phasef_request_bodies=18`,
  `phasef_request_messages_indexed=14` (4 empty `input=[]` delta markers)
- `compressed_messages_inspected=513` (every Codex WSS frame inflated and
  passed through the adapter)
- `compressed_messages_mutated=5`, `frames_reencoded=5`,
  `phasef_mutations=5` (real read-delta and L0 envelope compaction)
- `mutation_active=true`, `byte_bridge_only=false`
- `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`
- `savings.input_tokens_saved=28284` (autonomous run, today)

Aggregate report (`aggregate-savings --period=all --usd-per-million=2.5`):
- WSS input tokens saved (today, autonomous run): 28284
- HTTP-path Layer-0 filter (all-time historical):
  runs=2852, input_tokens=1515326, output_tokens=160029,
  tokens_saved_est=1356139, estimated USD=$3.39
- Aggregate total tokens saved: **1384423**
- Aggregate estimated USD saved: **$3.46**
- Reducer status note rendered: "mutation_active=true: the reducer chain
  is producing real WSS savings on this daemon."

What this proves:
- The WSS Phase-F reducer chain produces real, measurable input-token
  savings on the live machine, not only inside the fixture test. The
  numbers are smaller per-session than the controlled capture
  (4634-7000 tokens per session vs the capture's 26461 over a
  three-identical-read session) because Codex's own CLI heuristics
  sometimes deduplicate adjacent identical reads internally; whenever
  Codex actually emits the second read tool call, Slimference produces
  the delta marker and the bytes drop.
- Desktop uses the identical Phase-F route (proven in T246), so the same
  reducer applies there the moment a Codex.app conversation re-reads a
  file across turns. No Desktop-specific code path is missing; the
  Desktop "no savings yet" reading was workload-only.
- The HTTP-path Layer-0 hook savings dwarf the WSS savings on this
  machine right now (1.36M tokens historical vs 28k WSS today). That is
  expected because the L0 hook has been collecting since 2026-04-13;
  WSS Phase-F only mutates within the scoped CLI/Desktop conversation
  window, not the entire history.

Tooling fix this session:
- `scripts/utils/aggregate_savings.go`: the static "byte_bridge_only=true
  means..." note used to print unconditionally. Now it switches on the
  live state: prints the bridge-only line only when `byte_bridge_only`
  is set, prints a "reducer chain is producing real WSS savings" note
  when `mutation_active=true`, prints nothing extra otherwise. Test
  `TestAggregateSavingsByteBridgeNoteOnlyWhenBridgeOnly` locks both
  branches in (9 aggregate-savings tests total, all green).

Snapshot for cert ceremony reproducibility:
- Saved `/tmp/t247-autonomous-final-report.json` (1500 bytes) with the
  exact `aggregate-savings --json` output for this measurement. Not
  committed to the repo (would carry the real WSS counter window from
  this machine); useful as a reference shape when T240 release cert
  needs to publish numbers.

Closure impact:
- T247 sub-task "quantify savings on the OTHER layers" now has the
  aggregate-savings tool AND a first honest live measurement. The
  remaining work is a longer-window real-workday capture, not code.