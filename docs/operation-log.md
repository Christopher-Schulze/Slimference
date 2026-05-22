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
