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
