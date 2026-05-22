# TASK 246: Codex Desktop app-server shim proof

Status: IMPLEMENTED INFRASTRUCTURE - LIVE BLOCKED FOR CURRENT CODEX.APP
Priority: P0 before any Desktop savings claim or T240 release certification
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

Live proof disproved that candidate for current Codex.app. The infrastructure
is still the cleanest hook if a future Codex build changes endpoint handling,
but current Codex Desktop does not produce usable Slimference Desktop savings
through it.

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
- `slimference codex desktop prove --finish --json` returns green only as
  `desktop_app_server_phasef_proven`: bytes in both directions, WSS frames,
  `frames_reencoded>0`, `compressed_messages_mutated>0`, and zero
  parse/degrade/compression errors in the Desktop-specific delta window.
- If bytes and frames flow but mutation does not, classify as
  `desktop_app_server_wss_bridge`, not as Desktop savings.
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
- [x] Retarget TUI Launch Codex App to require
  `desktop_app_server_proven`.
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

- This is not a Desktop savings claim.
- The app-server shim is implemented infrastructure and useful diagnostic
  leverage, but it is blocked by current Codex Desktop behavior.
- TUI Launch Codex App must remain blocked for Slimference mode and show
  `connect_only_no_app_server_bytes` until a future
  `desktop_app_server_phasef_proven` result exists.
- Normal Finder/Spotlight Codex.app is the correct no-drawback Desktop path
  today. Browser ChatGPT, ChatGPT.app, Claude Code, `/etc/hosts`, pfctl,
  Keychain, macOS proxy settings, and `~/.codex/config.toml` remain outside
  this product path.

## Deviations

None.
