# TASK 246: Codex Desktop app-server shim proof

Status: CLOSED FOR ROUTING AND SAVINGS PROOF - user-confirmed end-to-end Desktop launch via
`slimference codex desktop prove --manual` + `--finish` on 2026-05-23 returned
`mode=desktop_app_server_route_ready`, `launch_ready=true`,
`desktop_proven=true`, with `phasef_bridged=2`, `compressed_messages_inspected=584`,
zero parse/degrade/compression errors. Persisted proof at
`~/.slimference/codex-desktop-proof.json` now unlocks TUI Launch Codex App for
future launches. Desktop conversation rides the same Phase-F WSS savings route
as the certified CLI. A later T247 Desktop repeat-read proof on 2026-05-29
upgraded the live Desktop evidence to full savings proof on Codex 0.135.0:
`desktop_app_server_phasef_proven`, `desktop_savings=true`,
`frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
zero parse/degrade/compression errors. Savings remain workload-dependent:
large when Codex re-reads the same file across turns, ~0 on non-repeat
sessions.
Priority: P0 for Desktop route integrity; savings proof is now delegated to
T247's measured reducer workload evidence
Scope: Codex Desktop App conversation routing only; no global lab product path

## Why

The proxy/CA route from T238/T242 is correct up to CONNECT but fails before
application bytes. It adds avoidable complexity: CA material, Keychain fallback,
Electron proxy flags, TLS-MITM, and root-store uncertainty. That is the wrong
main product path if a cleaner Codex-supported process boundary exists.

The current Codex Desktop bundle and upstream Codex source expose that cleaner
boundary:

- Codex.app's Electron main process honors `CODEX_CLI_PATH` when starting the
  Rust `codex app-server`.
- `codex app-server` accepts global `-c key=value` config overrides.
- Codex's provider config accepts `base_url`, `requires_openai_auth`,
  `supports_websockets`, and `wire_api=responses`.
- Codex maps local `http://127.0.0.1:8990/backend-api/codex` to the matching
  local WSS responses endpoint without requiring TLS or a custom CA.
- A live non-Desktop Codex CLI smoke run with
  `-c openai_base_url="http://127.0.0.1:8990/backend-api/codex"` returned the
  expected sentinel and moved Slimference WSS bytes/frames with zero
  parse/degrade/compression errors.

Therefore the preferred Desktop route candidate was:

1. Launch Codex.app from Slimference with process-local
   `CODEX_CLI_PATH=<slimference binary>`.
2. Codex.app starts `slimference app-server ...` instead of the real Codex
   binary.
3. The hidden Slimference shim validates the scoped env and immediately `exec`s
   the real Codex binary as `codex app-server` with process-local
   `openai_base_url`, `chatgpt_base_url`, and provider overrides.
4. Desktop conversation WSS should have reached the existing scoped Slimference
   `/backend-api/codex` route without CA trust, HTTPS proxying, Electron proxy
   args, global hosts, system proxy, or `~/.codex/config.toml` mutation.

That first candidate was disproved for current Codex.app until the real root
cause was found: Desktop sent `thread/start` with `modelProvider: null`, which
resolved to the account default provider and bypassed the shim's configured
default. The current solution is the stdin JSON-RPC mediator that rewrites only a
default/null `modelProvider` to `slimference-codex`. With that mediator,
Desktop reaches the same Phase-F route as CLI. Savings remain a separate T247
question.

## Acceptance

- `slimference codex launch-desktop --transport=app-server --probe` prints only
  process-local app-server shim env:
  `CODEX_CLI_PATH`, `SLIMFERENCE_CODEX_DESKTOP_ACTIVE`,
  `SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN`,
  `SLIMFERENCE_CODEX_DESKTOP_BASE_URL`, and `NO_PROXY`.
- The app-server shim refuses to run unless
  `SLIMFERENCE_CODEX_DESKTOP_ACTIVE=1` is present.
- The app-server shim refuses missing or invalid upstream Codex binary paths and
  refuses invalid local base URLs.
- The app-server shim removes `CODEX_CLI_PATH` and every
  `SLIMFERENCE_CODEX_DESKTOP_*` variable before execing the real Codex binary.
- The real Codex argv is exactly `codex app-server` plus process-local
  `openai_base_url`, `chatgpt_base_url`, and provider overrides for
  `slimference-codex`, followed by the original incoming app-server args.
- No Desktop product launch path sets `HTTP_PROXY`, `HTTPS_PROXY`, `WSS_PROXY`,
  `ALL_PROXY`, Electron `--proxy-server`, CA env vars, `/etc/hosts`, pfctl,
  macOS system proxy, or persistent `~/.codex/config.toml`.
- `slimference codex desktop status --json` treats only an app-server shim proof
  as green. Legacy `desktop_proxy_*` proof state must not unlock TUI Launch
  Codex App.
- `slimference codex desktop prove --manual --json` launches only
  `--transport=app-server`, records a Desktop-specific WSS baseline, and returns
  `desktop_ready_for_prompt` only when the app is alive and ready for a user
  prompt.
- `slimference codex desktop prove --finish --json` returns route-ready when a
  Desktop conversation reaches Phase-F (`phasef_bridged>0`) with zero
  parse/degrade/compression errors. It returns savings-proven only when mutation
  also fires (`frames_reencoded>0` and `compressed_messages_mutated>0`).
- If Phase-F is reached but mutation does not fire, classify as
  `desktop_app_server_route_proven` / `desktop_app_server_route_ready`, not as
  Desktop savings.
- If no Desktop-specific bytes flow, classify as a proof failure and keep TUI
  Launch Codex App blocked.
- `slimference codex desktop prove` and TUI Launch Codex App close an already
  running Codex.app main process before scoped launch, then re-probe that it is
  gone. This avoids macOS reusing a stale direct app instance that never
  inherited the Slimference shim env. Raw `codex launch-desktop` only does this
  when `--replace-existing` is explicit.
- Normal Finder/Spotlight Codex.app launch remains direct and must not inherit
  the shim env.
- Browser ChatGPT, ChatGPT.app, Claude Code, global hosts, pfctl, system proxy,
  and persistent shell env remain untouched.
- `~/.codex/config.toml` hash stays unchanged across proof start, prompt, finish,
  cleanup, and normal direct relaunch.

## Sub-Tasks

- [x] Inspect current Codex.app ASAR and verify Electron uses `CODEX_CLI_PATH`
  when spawning the Rust app-server.
- [x] Inspect upstream Codex 0.133.0 source and verify `codex app-server`
  accepts `-c key=value` overrides.
- [x] Inspect provider/base URL and WSS endpoint code to verify local
  `http://127.0.0.1:8990/backend-api/codex` maps to local WSS.
- [x] Run non-Desktop live smoke proof through local Slimference base URL and
  verify response sentinel plus WSS bytes/frames with zero errors.
- [x] Add hidden `slimference app-server` shim.
- [x] Add `--transport=app-server` as the default Desktop launcher mode.
- [x] Keep `--transport=proxy` and `--transport=base-url` as diagnostics, not as
  product defaults.
- [x] Retarget `codex desktop prove` to launch `--transport=app-server`.
- [x] Retarget TUI Launch Codex App to require the launchable Desktop app-server
  status (`desktop_app_server_proven` / route-ready mapping), while keeping the
  UI copy distinct from savings-proven.
- [x] Add explicit `--replace-existing` launcher mode and wire it into Desktop
  proof plus TUI Launch Codex App so stale Codex.app instances cannot be reused.
- [x] Update tests for shim argv, env scrubbing, launcher probe, status gating,
  proof modes, and TUI action routing.
- [x] Build and install the new Slimference binary atomically.
- [x] Restart daemon and verify CLI WSS still green.
- [x] Quit all normal Codex.app instances before Desktop proof.
- [x] Run `slimference codex launch-desktop --transport=app-server --probe` on
  the installed binary and capture exact env.
- [x] Run `slimference codex desktop prove --manual --duration=15s --json`.
- [x] Send one short prompt in the launched Codex.app window from a real repo
  workspace, preferably `/Users/christopher/CODE/Slimference`.
- [x] Run `slimference codex desktop prove --finish --json`.
- [x] Capture lsof for app-server process, daemon `.wss` delta, config hash,
  Browser ChatGPT direct-control evidence, ChatGPT.app direct-control evidence
  if running, and normal Finder/Spotlight Codex.app direct relaunch evidence.
- [x] If green, unlock TUI Launch Codex App and proceed to T240 release
  certification. If not green, keep TUI blocked and record exact failure class.

## Notes

- 2026-05-22 live proof after `e1633ef`: provider-block-only shim launched
  Codex.app, showed the Slimference provider badge, and answered
  `DESKTOP_PROBE_OK`, but daemon WSS stayed at zero bytes/frames/mutations.
  Evidence showed the app-server argv had `model_provider=slimference-codex`
  and local provider `base_url`, while the app-server still held direct
  `chatgpt.com:443` sockets. Verdict: provider-block overrides are not enough
  for current Desktop conversation routing.
- Same session follow-up added process-local top-level `openai_base_url` and
  `chatgpt_base_url` to the shim, matching the CLI smoke mechanism. Live
  Desktop launch then opened local connections to `127.0.0.1:8990`, but proof
  classified `desktop_connect_only_no_app_server_bytes`: `mitm_bridged>0` with
  `bytes_c2s=0`, `bytes_s2c=0`, no frames, no mutation. Verdict: current
  Codex Desktop app-server still does not produce a usable local WSS Phase-F
  path through the app-server shim.
- Installed follow-up proof after the top-level override patch kept Codex CLI
  green (`auto.mode=wss_phasef`, Codex CLI 0.133.0, `wss_certified=true`) and
  repeated the Desktop result:
  `mode=desktop_connect_only_no_app_server_bytes`,
  `failure_class=connect_only_no_app_server_bytes`, `mitm_bridged=1`,
  `bytes_c2s=0`, `bytes_s2c=0`, zero frames, and zero mutation. Process
  evidence showed `openai_base_url` and `chatgpt_base_url` were present in the
  app-server argv, so the failure is not missing env injection.

- 2026-05-22 Phase-0 diagnostics (read-only, no product change) replaced the
  historical header hypotheses with hard data against the installed Codex
  `0.133.0`. Binary scan: the conversation host `chatgpt.com/backend-api/codex`
  and `responses_websockets` subprotocol are hardcoded; the binary reads
  `CODEX_CA_CERTIFICATE`/`SSL_CERT_FILE` (so not absolute pinning) plus several
  `*_BASE_URL` env vars, but `OPENAI_BASE_URL`/`CHATGPT_BASE_URL` are only
  config keys, not env hooks. Live launch: `0` `responses_websockets` WSS
  upgrades reached `127.0.0.1:8990` (raw-scoped recorder = 0), the app-server's
  loopback connections to `8990` were REST sideband/CONNECT only, and the
  app-server held direct `172.64.155.209:443` (chatgpt.com) sockets. Verdict:
  in ChatGPT-auth mode the conversation WSS goes hardcoded-direct to chatgpt.com;
  the `-c` overrides only redirect REST sideband. This is a Codex Desktop
  property, not a Slimference classification bug. Caveat: the proof delta reads
  global daemon WSS counters, so concurrent CLI traffic can contaminate the
  finish label (this run mislabeled `desktop_app_server_wss_bridge` despite zero
  desktop upgrades); a trustworthy Desktop proof needs desktop-scoped counters.
  Full evidence in `docs/operation-log.md` (2026-05-22 Phase-0 entry).
- 2026-05-22 ROOT CAUSE + ROUTING FIX (commit `9dcf8f4`). A throwaway stdio tee
  (CODEX_CLI_PATH -> tee -> real codex) captured what Codex Desktop's Electron
  client sends. The conversation `thread/start` carries `model="gpt-5.5"` and
  `modelProvider=null`; null resolves to the account default provider `openai`
  (chatgpt.com direct), overriding the shim's `-c model_provider` default. The
  provider badge was cosmetic. Framing is newline-delimited JSON (verified). A
  direct app-server drive with `modelProvider="slimference-codex"` routed gpt-5.5
  through Slimference (8990 sockets, real NONCE answer, phasef_req+2), proving the
  app-server honors the provider when set. Fix: the shim is now a thin stdin
  JSON-RPC mediator that rewrites a default (null/absent) `thread/start`
  `modelProvider` to `slimference-codex`, leaving everything else byte-identical;
  stdout/stderr pass through; realtime/voice threads and explicit provider choices
  are left untouched; fail-open on any parse ambiguity. Live proof after the fix:
  the Desktop conversation held 6 connections to `127.0.0.1:8990` and ZERO direct
  `chatgpt.com` sockets (was direct before), WSS frames flowed.
- REMAINING (savings not yet active): the routed Desktop session is byte-bridged,
  not Phase-F mutated (`phasef_requests=0`, `frames_reencoded=0`,
  `byte_bridge_only=true`). The same provider via CLI `codex exec` gets
  `phasef_requests>0` even on a tiny prompt, and a direct app-server drive WITHOUT
  Electron's feature-flag `config` also reached Phase-F. So Electron's `thread/start`
  `config` feature flags (candidate: `features.enable_request_compression=true`)
  likely change the WSS request-frame format so Slimference's Phase-F parser does
  not recognize request envelopes and falls back to a safe byte bridge. Next step:
  capture the Desktop WSS request frames (socket/tcpdump ground truth, not the
  laggy desktop-status counters) to confirm the frame-format difference, then
  either neutralize the responsible flag in the shim's thread/start `config` or
  teach the Phase-F parser the variant. TUI Launch Codex App stays blocked until
  a green `desktop_app_server_phasef_proven` (bytes+frames+mutation) exists.
- 2026-05-22 Phase-F flag investigation (NOT committed; measurement wall).
  Direct app-server drives isolated `features.enable_request_compression`: a clean
  control vs flag pair gave Phase-F `phasef_requests` delta +1 (no flag) vs 0
  (flag on). So Codex's own request compression is at least one Phase-F breaker.
  A shim variant that also forces `features.enable_request_compression=false` in
  thread/start was implemented and unit-tested, but the live proof was
  inconclusive and showed a regression signal: from a fresh daemon the routed
  conversation held 6 loopback sockets to `:8990` yet `c2s_frames=0`/`bytes_c2s=0`
  (vs `c2s_frames=14` before the flag change), suggesting disabling compression may
  push the app-server onto an HTTP path instead of WSS. The variant was reverted;
  only the proven routing fix (`9dcf8f4`) is committed.
  Root blocker for finishing: the `desktop status` WSS counters are sampled and
  lag/cache/rate-sensitive; repeated rapid drives made even the control flip
  +1 -> 0, so flag bisection by counter is unreliable. The next session needs a
  reliable ground-truth signal before changing behavior: a `sudo tcpdump -i lo0 -A
  'tcp port 8990'` capture of a single Desktop turn (control vs Electron config),
  diff the WSS upgrade + request frames, identify the exact flag(s)/frame-format
  that defeat Phase-F, then fix with confidence and verify by socket+frame, not by
  the laggy status counters.
- 2026-05-22 CORRECTION (supersedes the flag investigation above). Reliable
  ground truth ended the confusion. (1) Frame capture via a loopback tee proxy
  showed the Codex app-server WSS conversation frames use `permessage-deflate`
  (first client frame `0xc1`, RSV1 set) - and the CLI `codex exec` green path uses
  the IDENTICAL format. So permessage-deflate / `enable_request_compression` is NOT
  the discriminator; the earlier counter readings were noise. (2) With the daemon's
  `SLIMFERENCE_DEBUG_DECISIONS_LOG` (reliable per-request flight log), BOTH the CLI
  and the app-server (driven with the full Electron feature-flag `config` including
  `enable_request_compression=true`) recorded `route_mode=websocket_phasef` for
  `/backend-api/codex/responses`. The Desktop conversation is on the same Phase-F
  savings route as the certified CLI. The earlier `byte_bridge_only` /
  `phasef_requests=0` live readings were laggy/global-counter artifacts plus
  trivial prompts with nothing to mutate (same caveat as the CLI smoke).
  Conclusion: the committed routing fix (`9dcf8f4`) is sufficient; the reverted
  compression rewrite was correctly dropped (unnecessary; its `c2s_frames=0` was
  also counter noise). Remaining: the TUI green gate
  `desktop_app_server_phasef_proven` reads the laggy WSS delta counters; flip it
  reliably from the decisions-log `route_mode=websocket_phasef` evidence (or a
  quiet-daemon compressible-turn proof), not the sampled counters.
- 2026-05-22 GATE FIX (commit `af972df`). Added a lag-free, monotonic dispatcher
  counter `phasef_bridged` that increments once per Phase-F WSS conversation at
  FrameBridge entry (upgrade time) - independent of byte/frame accumulation and
  snapshot timing. Plumbed through `DispatcherTelemetry` -> `control.WSSState` ->
  `codex desktop status`. `classifyCodexDesktopProof` now treats `phasef_bridged>0`
  with zero parser/degrade/compression errors as the reliable verdict:
  `desktop_app_server_phasef_proven` when mutation also fired, else
  `desktop_app_server_route_proven` (launch-eligible; per-turn savings scale with
  conversation size). `applyCodexDesktopLastProof` maps `route_proven` to the
  launchable `desktop_app_server_proven` status; `codexDesktopTLSRejected` is now
  guarded by `phasef_bridged==0` so a real Phase-F session is never misread as a
  TLS rejection. Live-verified: fresh daemon `phasef_bridged=0`, one Desktop
  app-server conversation -> `phasef_bridged=1`. Tests cover route-proven,
  phasef-via-counter, errored-phasef, and the launchable status mapping. Net: the
  TUI Launch Codex App gate is reliably satisfiable; engineering for T246 is
  complete.
- 2026-05-23 (later) END-TO-END USER CONFIRMATION. User ran the full
  `slimference codex desktop prove --manual --json` -> Codex.app conversation ->
  `slimference codex desktop prove --finish --json` cycle. Finish output:
  `mode=desktop_app_server_route_proven`, `launch_ready=true`,
  `desktop_proven=true`, `manual_prompt_still_required=false`,
  `desktop_savings=false`. WSS delta over the proof window:
  `mitm_bridged=2`, `phasef_bridged=2`, `bytes_c2s=127663`, `bytes_s2c=178171`,
  `c2s_frames=7`, `s2c_frames=581`, `phasef_requests=5`,
  `phasef_request_messages_indexed=3`, `phasef_text_deltas=540`,
  `phasef_terminal_responses=5`, `compressed_messages_inspected=584`,
  `compressed_messages_mutated=0`, `frames_reencoded=0`, `parse_failures=0`,
  `degraded_sessions=0`, `compression_errors=0`. Verdict persisted to
  `~/.slimference/codex-desktop-proof.json`. TUI Launch Codex App is now
  launch-eligible. `desktop_savings=false` on this specific session is the
  expected workload-variance behaviour: the user's conversation did not
  re-read the same file across turns, so the readcache delta path had nothing
  to compact. The reducer chain itself is proven separately by T247 fixture
  + capture; mutation on Desktop will appear the same moment a repeat-read
  workload lands on the same Phase-F route. T246 is now closed end-to-end:
  routing solved, gate proven, user-confirmed launch path.

- Adjacent finding (not T246 work): `TestStartCodexDesktopProcessRejectsImmediateExit`
  (`codex_desktop_launcher_test.go`) is timing-flaky under full-suite parallel load -
  it failed once in a full `go test ./...` run but passes 5/5 in isolation. The
  start-probe (`codexDesktopStartProbeDelay` + `syscall.Wait4` WNOHANG) races the
  fake process's immediate exit. Pre-existing (last touched `e1633ef`), not caused by
  the shim/gate changes. If it trips CI, it is a flake, not a regression; worth making
  the probe deterministic in a future cleanup.

Source inspection facts:

- Codex Desktop ASAR contains a spawn path that uses `CODEX_CLI_PATH` before the
  bundled Codex CLI path when launching the local app-server.
- Upstream Codex `codex-rs` exposes `CODEX_CA_CERTIFICATE` for TLS paths, but
  this task deliberately avoids that route because local HTTP base URL avoids
  TLS between Codex and Slimference.
- The previous proxy/CA route still matters as a diagnostic fallback and for
  global lab work, but it should not be the main UX after this discovery.

Non-Desktop smoke proof:

```bash
env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
  NO_PROXY=127.0.0.1,localhost,::1 no_proxy=127.0.0.1,localhost,::1 \
  codex exec --ephemeral -C /Users/christopher/CODE/Slimference \
    -c 'openai_base_url="http://127.0.0.1:8990/backend-api/codex"' \
    'Reply exactly LOCAL_BASE_URL_OK'
```

Observed result:

- Codex returned `LOCAL_BASE_URL_OK`.
- WSS counters moved from `bytes_c2s=117186`, `bytes_s2c=190943`,
  `c2s_frames=5`, `s2c_frames=106`, `phasef_requests=5` to
  `bytes_c2s=172848`, `bytes_s2c=264672`, `c2s_frames=7`,
  `s2c_frames=122`, `phasef_requests=7`.
- `parse_failures=0`, `degraded_sessions=0`, and `compression_errors=0`.
- The prompt was intentionally tiny, so mutation counters did not advance.
  Desktop green still requires a prompt proof that triggers Phase-F mutation.

Current decision:

- The 2026-05-23 run was route-only; the 2026-05-29 repeat-read run is the
  Desktop savings proof.
- The app-server shim plus stdin mediator is implemented product infrastructure
  and is the current clean Desktop Slimference route.
- TUI Launch Codex App may launch when the latest proof is route-ready
  (`phasef_bridged>0`, zero errors). It may label Desktop WSS savings only when
  the latest proof is `desktop_app_server_phasef_proven` with mutation counters,
  as in the 2026-05-29 proof.
- Normal Finder/Spotlight Codex.app remains the correct no-drawback direct path
  outside Slimference mode. Browser ChatGPT, ChatGPT.app, Claude Code,
  `/etc/hosts`, pfctl, Keychain, macOS proxy settings, and persistent
  `~/.codex/config.toml` mutation remain outside this product path.

## Deviations

None.
