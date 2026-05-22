# TASK 242: Codex Desktop custom-CA env and proxy compatibility matrix

Status: LIVE PROOF NEGATIVE - automated proof gate implemented; current Desktop
path still blocks Slimference savings after user prompt
Priority: P0 after T238 zero-byte CONNECT classification
Scope: Codex Desktop App conversation routing only; no global lab product path

## Why

T238 proved the clean scoped Desktop architecture up to CONNECT:
process-local proxy env reaches the Codex.app Rust app-server, CONNECT reaches
Slimference, and upstream dial succeeds. The failure is after Slimference
presents its local CA-signed leaf: Codex.app closes before any application
bytes flow.

The current best hypothesis is a Rust TLS root-store mismatch: Codex.app may use
rustls/webpki roots or another root store that does not see the macOS Keychain
trust entry. This is functionally as blocking as pinning, but it is more precise
and may have a solvable hook.

The newest high-value hook is Codex's own custom CA environment support:
current upstream Codex has `CODEX_CA_CERTIFICATE`, falls back to
`SSL_CERT_FILE`, and its secure WebSocket path calls the same custom-CA rustls
builder before opening `wss://.../responses`. The installed Codex.app binary
also contains `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`, rustls/webpki, proxy
env, and managed-proxy strings. That does not prove Desktop conversation routing
works, but it changes the next proof: try process-local custom CA env before
asking the user to trust a CA in Keychain.

This task must not confuse Desktop TLS work with the scoped Codex CLI WSS
product path. CLI WSS Phase-F, WSS bridge, auto-recert, and the T243 ladder do
not require macOS Keychain trust. CA/root-store work here is only for Desktop
process-local proxy or global lab TLS-MITM branches. It is still CA/proxy/MITM:
without an official Desktop endpoint hook or local-provider route, Slimference
cannot see encrypted Desktop WSS frames and therefore cannot produce Desktop
Phase-F savings.

## Acceptance

- Run the explicit `--with-ca-env` Desktop launcher probe with
  `CODEX_CA_CERTIFICATE` as the primary Codex-specific CA hook and
  `SSL_CERT_FILE`, `CURL_CA_BUNDLE`, `REQUESTS_CA_BUNDLE`, and
  `NODE_EXTRA_CA_CERTS` as compatibility hints. Every variable points at the
  Slimference root only for the spawned Codex.app process.
- Add and use `slimference codex desktop prove`: snapshot daemon WSS state,
  launch one scoped Codex.app process, observe a bounded pre/post delta, clean
  up hard failures, and support a prompt-driven manual mode. `--manual` exits
  zero for `desktop_ready_for_prompt` so the operator can send a prompt in the
  launched app; `--finish` is the savings gate.
- Refuse Desktop proof/launch if a normal Codex.app main process is already
  running, because that instance may be foregrounded by macOS without inheriting
  the scoped Slimference env.
- Scrub inherited `CODEX_*` session variables from every Desktop launch/proof
  env before adding intentional Slimference variables. A launcher started from
  inside Codex must never pass an old `CODEX_THREAD_ID` into the new Codex.app
  process tree.
- Do not require macOS Keychain trust for this first Desktop proof. Keychain
  trust is a fallback/lab branch from T245, not the default Desktop UX.
- Verify app-server env via `ps eww` and route via `lsof`.
- Send one Desktop prompt and capture `/_slimference/admin/state` `.wss`
  before/after.
- If bytes and WSS frames flow, continue to mutation proof before claiming
  Desktop savings.
- If bytes remain zero even with `CODEX_CA_CERTIFICATE`, classify Desktop
  Slimference as `desktop_ca_env_rejected` / blocked until upstream changes its
  root-store behavior or exposes a supported route hook.
- If Desktop bytes flow but Phase-F mutation is not yet proven, classify
  Desktop as WSS byte-equal bridge, not as a savings path. The T243 ladder
  applies to Desktop only after T242 proves process-local Desktop routing can
  carry real conversation bytes through Slimference.
- If CA env or Keychain trust is absent, do not mark CLI WSS degraded. Report
  it only as a Desktop/Lab readiness detail. The TUI wording must say
  "not needed for CLI WSS".
- Investigate Codex's own managed `network.proxy_url` / permission-profile
  proxy path from the OpenAI Codex source tree and prove whether it applies only
  to command sandbox traffic or can ever affect Desktop conversation routing.
- Investigate provider/base-URL route hooks one last time, but treat them as
  positive only if Desktop conversation WSS bytes, not sideband traffic, enter
  Slimference.
- No `/etc/hosts`, pfctl, macOS system proxy, persistent shell env, or
  `~/.codex/config.toml` mutation is allowed in the product proof.

## Sub-Tasks

- [x] Update or verify the Desktop launcher so `--with-ca-env` injects
  `CODEX_CA_CERTIFICATE=<slimference-root.crt>` before generic CA env hints.
- [x] Harden `codex launch-desktop` so it detaches from the caller, starts from
  the Codex bundle executable directory, and refuses with the concrete exit or
  signal if the spawned process dies during startup.
- [x] Add `slimference codex desktop prove --json` as the automated Desktop
  savings gate: launch, observe WSS delta, classify, and clean up.
- [x] Gate TUI Launch Codex App on the automated Desktop proof result. Current
  live result blocks the daily TUI action because the TUI item means
  "Slimference mode"; Desktop savings remain unavailable until `--finish`
  proves Phase-F mutation.
- [x] Refuse scoped Desktop launch/proof while Codex.app is already running,
  forcing a clean env-injected process tree instead of relying on macOS app
  foregrounding.
- [x] Run `slimference codex launch-desktop --transport=proxy --with-ca-env
  --probe` and verify all process-local proxy and CA env hints are present.
- [x] Run the installed `codex desktop prove` command against the live daemon and
  record final mode, WSS delta, cleanup result, config hash, and normal
  Finder/Spotlight/direct controls.
- [x] Launch with `--with-ca-env`, send a prompt, collect lsof, WSS counters,
  and direct-control evidence for the current Codex.app build. Result:
  app response succeeded, but Slimference saw only CONNECT sessions with zero
  application bytes and zero Phase-F mutation.
- [ ] Test a direct Finder/Spotlight relaunch after cleanup and prove it is
  direct again.
- [ ] Read current installed Codex.app strings and upstream Codex source for
  `CODEX_CA_CERTIFICATE`, `SSL_CERT_FILE`, `native-certs`, `webpki-roots`,
  websocket TLS config, and managed-proxy behavior.
- [ ] Prove or reject the no-CA/no-proxy alternatives explicitly:
  official endpoint/base-URL hook, managed `network.proxy_url`, remote-control
  or app-server launch path. Any positive claim requires Desktop conversation
  WSS bytes through Slimference, not merely process env, provider badge, or
  sideband requests.
- [ ] If managed `network.proxy_url` is plausible for conversation traffic,
  design a non-persistent probe; otherwise document why it is command-sandbox
  only.
- [x] Cross-check T245 CA state wording: missing CA must block only Desktop
  proxy/lab probes, never scoped CLI WSS.
- [x] Update T239 Launch Codex App menu-state vocabulary with the final result:
  current Codex.app is `blocked` in the Slimference TUI; proof/proxy commands
  remain diagnostic until a future green `desktop_proxy_phasef_proven` result.

## Notes

The menu item is not pointless. It is the user-facing steering surface. What is
forbidden is presenting a blocked route as active savings. The item should stay
visible and capability-gated.

T243's WSS-first ladder is CLI-first. Desktop adopts it only after this task
proves the TLS/root-store barrier is gone. Until then, normal Finder/Spotlight
Codex.app remains the correct direct path and the TUI Desktop launch remains
blocked or diagnostic.

The Desktop menu item remains useful as the steering surface, but success means
real bytes and WSS frames through Slimference. CA env present, Keychain trusted,
provider badge, or CONNECT accepted are only precondition signals, not proof of
savings.

2026-05-20 non-live update:

- TUI Launch Codex App no longer opens the Desktop proxy path for daily use
  unless a future proof is green. The proxy launcher remains the diagnostic
  branch:
  `slimference codex launch-desktop --transport=proxy --with-ca-env`.
- Missing CA material still blocks Desktop diagnostics, but missing Keychain
  trust does not block the preferred Desktop probe and never affects CLI WSS.
- Historical `tls_trust_rejected` counters no longer permanently block the TUI
  launch path; they are shown as a process-local CA-env retry/proof state. A
  future live run must still prove bytes, frames, and zero errors before Desktop
  savings can be claimed.

2026-05-22 partial live update:

- `codex launch-desktop` is now a real detached launcher: it sets the child
  working directory to `Contents/MacOS`, starts a new session, releases the
  child after a startup probe, and refuses early if the child exits before the
  probe window closes. This closes the previous flaky state where the command
  could print "launched" even for a short-lived process.
- Live `--transport=proxy --with-ca-env --probe` emitted all expected
  process-local proxy and CA hints. Live launch produced a stable Codex.app main
  process plus app-server; lsof showed the app-server connecting to
  `127.0.0.1:8990`. Chromium's NetworkService still kept unrelated direct TLS
  sockets, so final proof must remain tied to app-server conversation deltas,
  not generic helper sockets.
- `codex desktop status` no longer treats daemon-wide WSS counters as Desktop
  conversation proof. It now reports `wss_counters_scope=
  daemon_cumulative_not_desktop_proof` and keeps `conversation_observed=false`
  unless a future Desktop-specific proof surface records a spawned-process
  pre/post delta. This prevents CLI recertification traffic from making Desktop
  look green.
- This partial blocker was closed by the prompt-driven live verdict below.
  Desktop remains diagnostic, not a savings claim.

2026-05-22 automated proof-gate update:

- Added `slimference codex desktop prove`. It launches a scoped Desktop process
  with proxy plus process-local CA env, observes daemon WSS delta for a bounded
  duration, classifies the result, and cleans up the spawned process unless the
  launch is a viable manual proof session.
- Added prompt-driven proof semantics: `--manual` keeps Codex.app open when
  mode is `desktop_ready_for_prompt`; after the user sends a prompt,
  `--finish` compares current WSS counters against the saved session baseline.
- Success requires `desktop_proxy_phasef_proven`: bytes in both directions,
  `frames_reencoded>0`, `compressed_messages_mutated>0`, and zero parser,
  degradation, and compression errors. WSS byte-equal bridge is a compatibility
  signal but not Desktop savings.
- Zero-byte CONNECT/TLS-close results classify as `desktop_ca_env_rejected` /
  `tls_trust_rejected`, matching the current Desktop blocker without overstating
  it as cryptographic pinning.
- The daily TUI Launch Codex App action now blocks because the finish proof is
  not green. Direct Codex.app remains available only by launching normally
  outside Slimference. Manual proof remains available as an explicit diagnostic
  command.
- `codex launch-desktop` now refuses when the Codex.app main binary is already
  running, because a reused macOS app instance would not prove scoped env
  injection.

The preferred branch is now:

1. TUI launches Codex.app with process-local proxy env and
   `CODEX_CA_CERTIFICATE`.
2. Slimference terminates only this Codex.app process tree's `chatgpt.com`
   WSS tunnel.
3. Browser ChatGPT, ChatGPT.app, normal Finder-launched Codex.app, Claude Code,
   system proxy, hosts, and `~/.codex/config.toml` remain untouched.
4. Desktop is called green only after bytes, WSS frames, Phase-F mutation, and
   clean error counters are live-proven.

If this branch works, Desktop can reach the same Phase-F savings class as the
CLI. If it fails, Desktop remains direct and honest until OpenAI exposes a
supported endpoint/root-store hook.

2026-05-22 prompt-driven live verdict:

- `slimference codex desktop prove --manual --duration=8s --json` launched a
  scoped Codex.app process and reported `desktop_ready_for_prompt`.
- The operator sent `Slimference Desktop Probe OK` in a no-folder Codex.app
  chat. The app answered successfully, proving native Desktop UX still works.
- `slimference codex desktop prove --finish --json` returned
  `desktop_ca_env_rejected` / `tls_trust_rejected` after roughly three minutes:
  `mitm_bridged=14`, `bytes_c2s=0`, `bytes_s2c=0`, `frames_reencoded=0`,
  `compressed_messages_mutated=0`, `parse_failures=0`,
  `degraded_sessions=0`, and `compression_errors=0`.
- lsof/env evidence showed the spawned app-server had process-local proxy and
  CA env plus a loopback socket to `127.0.0.1:8990`. Chromium NetworkService
  also inherited env but still held direct ChatGPT TLS sockets. The visible
  Desktop answer therefore cannot be counted as Slimference Phase-F traffic.
- Product decision at this point: TUI Launch Codex App must not claim Desktop
  savings until a future Codex.app build or supported hook proves real Desktop
  bytes plus Phase-F mutation through Slimference.

2026-05-22 Electron proxy-argument follow-up:

- `codex launch-desktop --transport=proxy --with-ca-env` now passes Electron
  `--proxy-server=http://127.0.0.1:8990` and
  `--proxy-bypass-list=localhost;127.0.0.1;::1` in addition to proxy and CA
  env.
- Live lsof showed the launched Chromium NetworkService using loopback proxy
  sockets and no non-loopback ChatGPT sockets. This closes the previous
  renderer/Chromium bypass branch.
- A visible prompt sent in that launched app still produced only one
  CONNECT/MITM session in Slimference with `bytes_c2s=0`, `bytes_s2c=0`,
  `c2s_frames=0`, `s2c_frames=0`, `frames_reencoded=0`, and
  `compressed_messages_mutated=0`. Parser/degrade/compression counters stayed
  zero.
- Final current blocker: scoped Desktop routing is correct up to CONNECT, but
  current Codex.app still does not accept the local Slimference CA/root-store
  path for conversation TLS. The TUI `Launch Codex App` item therefore blocks
  rather than opening direct or a known-bad proxy session.

2026-05-22 stale workspace restore fix:

- Codex Desktop's global state can persist deleted workspace roots in
  `~/.codex/.codex-global-state.json`. A stale
  `/Users/christopher/CODE/ClankWork-main` active root caused Codex.app to
  reopen a non-existing project even after `~/.codex/config.toml` was clean.
- The dead roots were removed from `active-workspace-roots`,
  `electron-saved-workspace-roots`, `project-order`, and
  `sidebar-collapsed-groups`; active root is now
  `/Users/christopher/CODE/Slimference`.
- The launcher now strips inherited `CODEX_*` environment variables and pins
  direct Desktop `PWD` to the selected folder. This closes a second restore
  path where launching Codex.app from an existing Codex session could pass
  `CODEX_THREAD_ID` into the new app process.

## Deviations

None yet.
