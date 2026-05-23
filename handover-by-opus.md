# Slimference - Handover by Opus (deep, current, verified)

Stand: 2026-05-23. Frontier described here: T246/T247 after source-state commit
`5349a5a` plus handover commit `214765e` and later handover-pointer corrections
(`9e7ec48`). A later agent must still run
`git status --short && git log -1 --oneline` first, because this file may itself
receive correction commits after `9e7ec48`.
Author: Opus 4.7. This file was written after line-level re-reading of the code
and docs it cites; the "Verification" section at the end states exactly what was
checked. No sampling, no guessing: every claim here was confirmed against the repo
on 2026-05-23 or is explicitly marked as an open question.

This document exists because the running context had heavy drift. It is the first
project-specific file a brand-new agent should read for the T246/T247 frontier:
what the project is, who the user is and what they require, where we are, how it
works, in which files, what is documented and where, what we have proven, and
exactly where we are going. It does not replace live verification.

---

## 0. First actions for a new agent

1. Read this file fully.
2. Read `AGENTS.md` (repo root) - it is binding. Pay special attention to §9
   (Verdrahtungs-Doktrin / Scoped Codex). The user's hard guardrails live there.
3. If you are running as Claude/Opus, read `~/.claude/CLAUDE.md` as the user's
   global Claude contract. If you are running as Codex, follow the Codex developer
   instructions and repo `AGENTS.md`; do not assume Claude-only dotfiles are
   binding for Codex. Never edit global agent files unless the user explicitly
   asks.
4. Skim `docs/todo.md` (the work list / TASK overview) and the two active TASK
   detail files: `docs/todo/t246-codex-desktop-app-server-shim-proof.md` and
   `docs/todo/t247-codex-wss-reducer-efficacy.md`.
5. Read `docs/operation-log.md` from the bottom up - the most recent ~10 dated
   entries (2026-05-22 / 2026-05-23) are the full, honest chronological record of
   the Codex Desktop + WSS investigation. This is where the deep findings live.
6. The older root `handover.md` (2026-04-10) is a good general project overview but
   PRE-DATES all the T246/T247 work; treat its Codex-Desktop/WSS statements as
   historical. This file (handover-by-opus.md) supersedes it for that frontier.
7. Normative spec is `spec+.md`. Install/uninstall SSOT is `docs/install.md`
   (kept in sync with code by the meta-test `docs/install_spec_test.go`).

---

## 1. What Slimference is

Slimference is a Go reverse proxy plus local install/launch tooling that reduces
token usage for Codex-first workflows. It is a single binary (`slimference`) built
from `cmd/slimference` (with a `cmd/slimference-sidecar`). It runs a local daemon
on `127.0.0.1:8990` (admin/control + scoped proxy); a transparent SNI listener
defaults to `127.0.0.1:8443` only when explicitly armed (global lab path).

Token reduction happens in layers (architecture detail in §6):
- Layer 0: pre-entry filter (shrink shell/tool output before it enters the chat).
- Layer 1: deterministic compression / dedup (exact + MinHash) of conversation
  bodies.
- Layer 2: background semantic summarisation of long histories.
- Layer 3: response cache + prompt-cache breakpoints.

Principle: fail-open transparent. Request shape, headers, streaming semantics, and
unknown payloads are preserved byte-for-byte. Only payloads a deterministic reducer
proves shorter and schema-safe are mutated.

Shipped product priority: Codex CLI first, Codex Desktop next. Claude Code code is
present in the tree but the product binary parks it (no install, no hooks, no
`~/.claude` writes, no Anthropic routing). Browser ChatGPT and ChatGPT.app are
untouched by default.

Language: this is a Go project. Docs and code are English (per the user's global
rule); the user communicates in German in chat. No em-dashes in docs/code.

---

## 2. Who the user is + the binding contract + the doctrine

The user (Christopher, dven23@pm.me) is the project owner. For this repo, the
binding cross-agent rules are in root `AGENTS.md`. Claude/Opus sessions also
inherit `~/.claude/CLAUDE.md`; Codex sessions follow the active Codex developer
instructions plus `AGENTS.md`. The non-negotiables that matter most for this
frontier:

Working contract (CLAUDE.md, abridged):
- Zero-guess / read-before-act: never infer an API or behaviour; read the actual
  source. Read every line when asked to "check" a file. No sampling.
- Production-grade only: no stubs/mocks/placeholders/TODO-as-code.
- Test integrity: tests verify real behaviour; never weaken a test to make it pass.
- Anti-sycophancy: truth over what sounds good. "Couldn't X because Y" beats a
  pleasant lie. No-partial-as-done: state precisely what is done vs open.
- Surgical in-place edits to existing files; never full-rewrite/overwrite.
- Chat German; docs/code English; no em-dashes.

The wiring doctrine (AGENTS.md §9, Scoped Codex, binding - this is the user's hard
guardrail for how Slimference is allowed to touch the machine):
- Default install/test may only scope ONE Codex process. It must NOT change the
  user's machine-wide stack. ChatGPT.app and Browser-ChatGPT must stay normal.
- Signal IN: Codex hooks in `~/.codex/hooks.json` (+ `config.toml [features].hooks`),
  out-of-band subprocess calls, never over network. Claude-Code hooks stay default-off.
- Traffic IN (scoped CLI): `slimference codex run -- <prompt>` starts only that one
  Codex CLI process pointed at the local `slimference-codex` provider. No
  `/etc/hosts`, no pfctl, no system proxy, no browser/ChatGPT.app blast radius.
- Traffic IN (global lab only): transparent SNI-MITM (`/etc/hosts` + CA in Keychain
  + 443/8443) stays in the code but is NOT a default test path. `slimference
  root-arm` requires explicit `--global-chatgpt-hosts`.
- FORBIDDEN as default install/test: persistent `OPENAI_API_BASE`/`OPENAI_BASE_URL`/
  `CHATGPT_BASE_URL`/`HTTPS_PROXY` env, persistent `openai_base_url` in
  `~/.codex/config.toml`, macOS system proxy, unconfirmed `root-arm`.
- Fail-open mandate: `slimference codex run` falls back to unfiltered Codex on
  daemon failure; Browser/ChatGPT.app always direct.
- Drift ban: expanding the default install set by a third surface needs an explicit
  `Phase-H-Override` tag in the change description.

NEVER touch `~/.codex/AGENTS.md`. NEVER edit `research/rtk-ai/rtk/` (embedded
foreign project, inspiration only).

---

## 3. THE FINAL TARGET PICTURE (what "done" means - do not make the user restate this)

The user wants Slimference used to the maximum, with ZERO drawbacks. Concretely:

1. Codex CLI saves tokens via Slimference, started normally through the
   wrapper/TUI ("Slimference mode"). This is the green path today (routing-wise).
2. Codex Desktop ALSO saves tokens, when launched via the TUI ("Launch Codex App"
   = Slimference mode). When started normally (Finder/Spotlight), it stays direct
   and unaffected.
3. NO drawbacks ever: no model degradation, no context loss, no memory loss, no
   broken UX, no fake/invented savings.
4. Everything else stays untouched: Browser ChatGPT, ChatGPT.app, Codex voice
   (realtime), browser-use, computer-use, Claude Code. Different processes / not
   the scoped Codex.
5. Stable and reliable. Honest measurement; never claim savings that are not
   measured. No global MITM / no machine-wide changes as the product path.

A single sentence target: "Launch Codex (CLI or Desktop) via Slimference -> real,
measured token savings, identical no-CA WSS pipeline, everything else native."

Two halves of "done":
- Routing half (get Codex traffic through Slimference, scoped, no drawbacks):
  DONE and proven for both CLI and Desktop (see §8).
- Savings half (Slimference actually reduces the tokens once it sees the traffic):
  NOT yet delivered - WSS Phase-F mutation is marginal product-wide. This is the
  real open work (T247, see §9).

---

## 4. Current state (precise)

- At initial handover write: branch `main`, working tree clean. Verify live state
  with `git status --short && git log -1 --oneline` before doing anything.
- The T246/T247 arc produced 19 source commits before this handover plus the
  handover commit `214765e`. Key source commits, newest first:
  - `5349a5a` T247: add cert-reproduction step + prompt-cache note; T246 flake note
  - `b4d1e9e` T247: open reducer-efficacy task + Responses-API delta root cause
  - `b5213e8` T246: fix Codex WSS session-key extraction (prompt_cache_key/client_metadata)
  - `1fb1218` T246: decisive - WSS Phase-F mutation marginal product-wide (CLI=0 too)
  - `fbef04c` T246: full mechanism reference for traceability
  - `18a7aff` T246: honesty pass after external review
  - `6d0471c` T246: honest gate semantics (route_ready != savings proven) + fix install.md
  - `07f9008` / `af972df` T246: reliable `phasef_bridged` gate + docs
  - `4e3626b` / `571a30d` T246: corrected Desktop finding (on phasef route, not blocked)
  - `9dcf8f4` (earlier this arc) T246: route Desktop conversation via thread/start rewrite
- TASK status (`docs/todo.md`):
  - T246 Codex Desktop app-server shim - routing SOLVED + reliable gate; engineering
    complete. Open: one real end-to-end TUI Desktop launch confirmation with the user.
  - T247 Codex WSS Phase-F reducer efficacy - reducer chain proven end-to-end on
    real Codex 0.133.0 CLI traffic 2026-05-23 (multi-read capture, `frames_reencoded=3`,
    `compressed_messages_mutated=3`, `input_tokens_saved=26461`, 94% reduction on
    output payload). Fixture-based regression test landed at
    `internal/proxy/wsmitm_phasef_real_capture_test.go::TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`
    (`commit fee1af4`). See §9 for the verified shape. Open: one Desktop pass on
    the identical route; quantification of non-WSS savings layers.
- Build/test health (verified this session): `go build ./...` clean,
  `go vet ./...` clean, `go test ./internal/proxy/...` green across all five
  packages; full-suite `go test ./...` green except one timing-flaky test
  (`TestStartCodexDesktopProcessRejectsImmediateExit`, passes 5/5 in isolation,
  pre-existing, see §10). `go run ./scripts/ci` 8/8 PASS, coverage gate green.
- Daemon currently running and healthy on :8990; CLI `wss_certified=true`,
  `auto.mode=wss_phasef`; cert path now anchored by a proven repeat-read reducer
  chain plus the synthetic-repo recert frame (see §9 for measured numbers).

---

## 5. Repo + documentation map (where everything is)

Code:
- `cmd/slimference/` - the product binary (main.go is 4537 lines; the command
  router and most subcommands live here). Key Codex files:
  - `codex_desktop_app_server_shim.go` (314 lines) - the hidden app-server shim:
    the stdin JSON-RPC mediator that rewrites `thread/start` modelProvider. §8.
  - `codex_desktop_launcher.go` (826 lines) - `codex launch-desktop`, `codex
    desktop prove`/`status`, env wiring, replace-existing, start-probe.
  - `codex_cmd.go` (1428 lines) - `codex run`/`enable`/`status`/`certify`, the
    Desktop proof classification + status mapping + WSS cert criteria. §8/§9.
  - `codex_recert.go` (619 lines) - CLI WSS recert trigger + delta math
    (`codexSetupDelta`). §9.
  - `proxy_cmd.go` - `proxy run/env codex --proxied-wss...`, the CLI green recipe
    (`codexEnvCommand`). §7.
- `internal/proxy/` - the proxy + WSS engine:
  - `wsmitm_phasef.go` (408 lines) - the Phase-F WSS reducer adapter
    (`wsPhaseFAdapter.handle/handleRequest/applyInputPipeline`) + `wsCodexSessionID`.
    THE file for savings. §9.
  - `wsmitm_dispatcher.go` (538 lines) - `PhaseFDispatcher`, counters incl. the new
    `phasefBridged`, `runWSMITM`/`runWSBridge`. §8/§10.
  - `wsmitm/session.go` (514 lines) - per-conversation WSS session: frame parse,
    permessage-deflate inflate/inspect/mutate, the `replace` decision, counter
    aggregation at session end. §10.
  - `raw_wss_listener.go` (98 lines) - intercepts the plaintext WS upgrade
    `GET /backend-api/codex/responses` with subprotocol `responses_websockets`.
  - `proxy.go` (1348 lines) - proxy assembly; FrameBridge/ByteBridge closures
    (lines ~303-313) where `phasefBridged` increments; raw-scoped listener wiring
    (~643-657).
  - `wss_state_probe.go` (66 lines) - maps dispatcher snapshot to
    `control.WSSState` for `/admin/state` and `codex desktop status`.
  - `provider.go` (1034 lines) - provider detection + `extractMessages` /
    `reconstructBody` for Codex/OpenAI/Anthropic wire formats.
  - `layer0_proxy.go` (313 lines) - `applyProxyLayer0WithSessionAndToolUses`: the
    L0 tool-output read-delta/compaction that should fire on Codex (it currently
    does not - §9).
  - `ws.go` (412 lines) - WebSocketTunnel, `ServeRawUpgrade` (phasef vs bridge by
    path), frame bridging.
- `internal/control/state.go` (251 lines) - `WSSState` (the `.wss` block), incl.
  the new `PhasefBridged`.
- `internal/codexroute/codexroute.go` (374 lines) - the persistent config-block
  route (`codex enable`); `blockBody` is the provider block recipe.
- Other layers: `internal/compression`, `internal/staleread`, `internal/outstop`,
  `internal/beterse`, `internal/repetition`/`outstop/repdet`, `internal/readcache`,
  `internal/promptcache`, `internal/summarization`, `internal/filter` (L0 filters),
  `internal/tui` (BubbleTea Launch Center), `internal/tlsca`/`tlsdial`/`tlsproof`
  (CA + TLS, lab path), `internal/transparent` (SNI engine, lab path).

Docs (SSOT layout per AGENTS.md):
- `docs/documentation.md` (84KB) - architecture SSOT (layers, lifecycle, client
  support, TUI, CLI, install). Updated this session for the corrected Desktop story.
- `docs/install.md` (38KB) - install/uninstall SSOT; kept in sync by
  `docs/install_spec_test.go`. Updated this session (Desktop section corrected).
- `docs/todo.md` (147KB) - TASK overview/work list. T246/T247 lines current.
- `docs/todo/tNNN-*.md` - per-TASK detail (246 files). t246 + t247 are the active
  frontier and are deeply detailed.
- `docs/operation-log.md` (118KB) - chronological op log. The 2026-05-22/05-23
  entries are the full T246/T247 record: Phase-0 diagnostics, routing breakthrough,
  corrected finding, gate fix, mechanism reference, decisive product-wide finding,
  and the Responses-API delta root cause. THIS is where the deep evidence lives.
- `docs/codex-routing-status.md` - superseded note pointing to the scoped product path.
- Others: `map.md`, `context.md`, `changelog.md`, `savings-assessment.md`,
  `output-reduce.md`, `transparent-mode.md`, `release-process.md`, etc.

---

## 6. Architecture - how it works (with file pointers)

Daemon + ports:
- Admin/control plane + scoped proxy listener: `127.0.0.1:8990`.
- Transparent SNI listener: `127.0.0.1:8443` (only when armed; global lab path).
- Assembly in `internal/proxy/proxy.go`; daemon lifecycle in `internal/daemon`.

Request lifecycle (HTTP path): inbound -> provider detection
(`provider.go:detectProviderWithUA`, path `/backend-api/*` -> CodexChatGPT) ->
extract messages (`extractMessages`) -> planner -> L0/L1/L2/L3 reducers -> reconstruct
body -> dial real upstream. Fail-open: unknown shapes pass through.

Transports for Codex (the important part):
- The scoped Codex CLI path uses `slimference codex run --transport=auto -- ...`,
  which resolves to a transport ladder: `wss_phasef -> wss_bridge -> http -> direct`
  (T243). WSS is the standard; HTTP is fallback after the byte-equal WSS bridge.
- WSS Phase-F is the interesting transport: Codex talks the OpenAI Responses API
  over a WebSocket. Slimference intercepts that WS, inflates frames, and runs the
  Phase-F reducer. This is where token savings are supposed to happen.

How Slimference gets in front of Codex WSS WITHOUT CA/MITM (the clean mechanism):
- The provider block (set via `-c` or config) tells Codex to use a provider whose
  `base_url` is `http://127.0.0.1:8990/backend-api/codex`, with
  `requires_openai_auth=true`, `supports_websockets=true`, `wire_api=responses`.
- Codex then opens a PLAINTEXT WebSocket upgrade `GET
  /backend-api/codex/responses` (subprotocol prefix `responses_websockets`) to
  127.0.0.1:8990, carrying the user's ChatGPT auth.
- `internal/proxy/raw_wss_listener.go` (`isRawScopedCodexWSS`) recognises exactly
  that upgrade and hands the connection to the WebSocket tunnel, which bridges it
  to the real `chatgpt.com` and runs Phase-F on the frames. No CA, no system proxy,
  no /etc/hosts.

The CLI green recipe (proven path), `cmd/slimference/proxy_cmd.go:codexEnvCommand`
for `--proxied-wss`:
```
codex
  -c model_provider="slimference-codex"
  -c model_providers.slimference-codex.name="Slimference"
  -c model_providers.slimference-codex.base_url="http://127.0.0.1:8990/backend-api/codex"
  -c model_providers.slimference-codex.requires_openai_auth=true
  -c model_providers.slimference-codex.supports_websockets=true
  -c model_providers.slimference-codex.wire_api="responses"
```
(No top-level openai_base_url/chatgpt_base_url; the provider block is enough.)

---

## 7. Codex routing (CLI) - the certified-green-but-marginal path

- `slimference codex run` (codex_cmd.go) builds the scoped command via
  `proxy run codex --proxied-wss` (proxy_cmd.go) and execs Codex with the provider
  block above. Process-scoped, fail-open, no machine-wide changes. Doctrine-clean.
- `transport=auto` resolves via `internal/codexroute` (certification-aware): if the
  installed Codex version tuple is certified, prefer `wss_phasef`.
- The persisted cert is `~/.slimference/codex-wss-cert.json`. Verified content
  2026-05-23: `{codex_version: 0.133.0, slimference_version: 2.0.2,
  frames_reencoded: 1, transport: wss, route_profile: scoped_raw_wss_phasef}`,
  recert attempt id `20260522T125430`. IMPORTANT: `wss_certified=true` rests on
  exactly ONE mutated frame. See §9 - live sessions currently mutate 0.
- Cert criteria: `codex_cmd.go:codexWSSCertificationFailures` (~line 1241) require
  `parse_failures=0`, `degraded_sessions=0`, `compression_errors=0`,
  `frames_reencoded>0`, `compressed_messages_mutated>0`, `mutation_active=true`,
  `byte_bridge_only=false`, `daemon_reachable=true`.
- `codex enable` (persistent config-block route, codexroute.go) is LEGACY/advanced
  per the doctrine; the scoped `codex run` is the product path.

---

## 8. Codex Desktop saga (T246) - ROUTING SOLVED + PROVEN

Detail file: `docs/todo/t246-codex-desktop-app-server-shim-proof.md`. Chronology +
evidence: `docs/operation-log.md` (2026-05-22/05-23 entries).

The problem (multi-session blocker before this work): launching Codex.app in
"Slimference mode" never produced savings; prior attempts (proxy/CA/MITM, top-level
base-url overrides) all ended at `desktop_connect_only_no_app_server_bytes`.

Root cause (found by capturing the real Electron stdio JSON-RPC via a throwaway tee
`CODEX_CLI_PATH -> tee -> real codex`): Codex Desktop's Electron frontend drives the
Rust `codex app-server` over newline-delimited JSON-RPC on stdio, and opens each
conversation with `thread/start` carrying `modelProvider: null`. `null` resolves to
the account default provider `openai` (chatgpt.com direct), OVERRIDING the `-c
model_provider` config default. The "Slimference" provider badge in the UI was
cosmetic. (Verified: thread/start response showed effective `modelProvider: openai`;
zero WSS upgrades reached 8990.)

The fix (commit `9dcf8f4`), `cmd/slimference/codex_desktop_app_server_shim.go`:
- Codex.app honours `CODEX_CLI_PATH`; the launcher points it at the slimference
  binary only for the spawned Codex.app process (`codex_desktop_launcher.go`,
  env vars `SLIMFERENCE_CODEX_DESKTOP_ACTIVE/UPSTREAM_BIN/BASE_URL`).
- The hidden shim used to just `exec` the real Codex. It is now a thin STDIN
  JSON-RPC MEDIATOR (`runCodexDesktopAppServerMediated`): it spawns the real Codex
  app-server with the provider block (argv lines 239-244: provider block only, NO
  openai_base_url/chatgpt_base_url - those were proven useless and removed), passes
  stdout/stderr straight through (zero added latency on streaming), and inspects
  only the client->server stdin (newline-delimited JSON).
- The single rewrite (`rewriteCodexDesktopThreadStart`, verified current): on a
  `thread/start` request whose `modelProvider` is null/absent, set it to
  `slimference-codex`. Everything else is byte-identical. It FAILS OPEN on any
  ambiguity: non-JSON, no `params`, an explicit non-null provider, or a
  realtime/voice thread (`codexThreadStartIsRealtime` checks
  `config["features.realtime_conversation"]`). So voice is structurally never
  touched, and the stream is never corrupted.
- Note: an attempt to also force `features.enable_request_compression=false` was
  built and then REVERTED (the current shim rewrites ONLY modelProvider) - it was
  based on a counter-noise misread (see §10) and showed a regression signal.

Proven facts (reliable, socket + flight-log evidence, not laggy counters):
- The spawned Desktop app-server holds loopback sockets to `127.0.0.1:8990` with
  ZERO direct `chatgpt.com` sockets (was direct before the fix).
- The Desktop conversation records `route_mode=websocket_phasef` on
  `/backend-api/codex/responses` in the daemon decisions log
  (`SLIMFERENCE_DEBUG_DECISIONS_LOG`) - the SAME Phase-F route as the certified CLI,
  with byte-identical `permessage-deflate` frames.
- A real Desktop session (two turns reading a 93KB file, read AFTER session close):
  `bytes_c2s=181344`, `bytes_s2c=375755`, `c2s_frames=27`, `s2c_frames=1120`,
  `compressed_messages_inspected=1111`, zero parse/degrade/compression errors. The
  conversation fully flows through Slimference and Phase-F inflates+inspects every
  message cleanly.

The gate (so the TUI is honest), `codex_cmd.go` + `main.go` + `internal/tui/model.go`:
- `phasef_bridged` (new monotonic dispatcher counter, increments once per Phase-F
  WSS conversation at FrameBridge entry - `proxy.go:303-313`) is the lag-free
  "route reached" signal. Plumbed through `DispatcherTelemetry` ->
  `control.WSSState` -> `codex desktop status`.
- `classifyCodexDesktopProof` (~line 639): `phasef_bridged>0` + zero errors ->
  `desktop_app_server_phasef_proven` if (`frames_reencoded>0` &&
  `compressed_messages_mutated>0`), else `desktop_app_server_route_proven`.
- `applyCodexDesktopLastProof` (codex_cmd.go:1056): `route_proven` maps to the
  LAUNCH-ELIGIBLE-BUT-HONEST status `desktop_app_server_route_ready` (TUI label
  "WSS route ready"), distinct from `desktop_app_server_proven` ("WSS savings").
  `codexDesktopTLSRejected` is guarded by `phasef_bridged==0`.
- `LaunchCodexApp` (`main.go:~3969`) allows both `proven` and `route_ready`.
- Status reads the LAST PERSISTED proof, so it stays `desktop_proof_prompt_required`
  until a real `prove --manual` + `prove --finish` cycle persists a verdict.

T246 honest status: routing is solved and proven; the engineering (mediator + gate)
is complete. The TUI can launch Codex.app in Slimference mode "route ready". It does
NOT claim savings, because savings are the T247 problem.

---

## 9. WSS Phase-F savings (T247) - REDUCER CHAIN PROVEN END-TO-END

Detail file: `docs/todo/t247-codex-wss-reducer-efficacy.md`. Evidence:
`docs/operation-log.md` 2026-05-23 entries (initial Phase-0 + the later capture
writeup).

Headline finding (2026-05-23 capture run, env-gated debug since reverted): on real
Codex 0.133.0 CLI traffic, a controlled 3x cat of a 35567b file routed through
scoped Slimference produced
- `frames_reencoded=3`, `compressed_messages_mutated=3`, `phasef_mutations=3`,
  `mutation_active=true`, `byte_bridge_only=false`,
- zero `parse_failures` / `degraded_sessions` / `compression_errors`,
- `savings.input_tokens_saved=26461` (94% reduction on output payload total:
  106701b -> 6846b).
The reducer chain function_call -> remembered tool_use ->
function_call_output -> tool_result -> commandLine -> readcache -> delta-marker
mutation runs end to end on Codex's real Responses-API delta wire shape, on the
current source, without code change. The cert `~/.slimference/codex-wss-cert.json`
no longer rests on a single F01-style mutated frame.

Earlier 2026-05-23 measurements that read `compressed_messages_mutated=0` on
multi-read CLI / Desktop were Codex-side run-variance: the same prompt against the
same code path picked a different number of c2s turns. On the capture run the
reducer mutated all three repeat-reads. Therefore the historical narrative "WSS
Phase-F mutates almost nothing" is calibrated: WSS Phase-F savings are
**workload-dependent**, large on sessions with repeat reads, low on sessions
without them.

Background (Codex's wire shape, still relevant to know):
- Codex WSS is the OpenAI Responses API with `previous_response_id` (server-side
  conversation state). Each request carries only the DELTA. Observed one-turn
  sequence: `input=[]` -> `input=[message,message,message]` ->
  `input=[function_call_output]` -> `input=[function_call_output]`. No single
  request contains repeated history.
- The cross-turn read-delta path therefore depends on the per-session readcache
  and `rememberToolUsesFromResponse`. Both are wired correctly; the prerequisite
  session-key fix from `b5213e8` (extract `prompt_cache_key` plus
  `client_metadata.x-codex-turn-metadata`) is in production and verified
  populated (`codex-wss:019e5220-...`).

The mutation decision path (still the right map; values are now proven):
- `wsmitm/session.go:finishCompressedMessage` inflates a permessage-deflate
  message, calls the handler (`wsPhaseFAdapter.handle` in `wsmitm_phasef.go`), and
  mutates (`CompressedMessagesMutated++`, `FramesReencoded++`) when the handler
  returns `replace=true`.
- `wsPhaseFAdapter.handleRequest -> applyInputPipeline` runs, in order: staleread
  aging, obsolete-read prune, `applyProxyLayer0WithSessionAndToolUses` (L0),
  stop-seq injection (skipped on Responses-shape per T233), be-terse hint
  (default off). All other OutputReduce sub-flags default TRUE.
- L0 (`layer0_proxy.go`) compacts `tool_result` blocks whose `tool_use` resolves
  to a command line, primarily via `compactProxyReadDelta` (readcache
  EvaluateObserved -> DecisionBlock returns a delta marker with
  `local-archive://...` reference), fallback via `compactProxyLayer0Text` ->
  `compactCodexExecEnvelope` -> `filter.CompactCapturedOutputWithContext` (the
  F01-F24 chain).

Real shape confirmed on Codex 0.133.0 traffic (every line verified against
captures, archived at `/tmp/t247-dump-evidence.tgz`):
- `extractMessages` parses Responses-shape `input` items;
  `codexInputItemToMessage` maps `function_call_output` -> `tool_result` with
  `ToolResultID = call_id`.
- `rememberToolUsesFromResponse` accumulates each prior `function_call` item from
  the response stream into `a.toolUses` keyed by `call_id`. By request #N the map
  holds requests #1..#N-1.
- `proxyResolveToolUse` resolves the tool_result block via the remembered map;
  resolved `ToolName = "exec_command"` (covered by `looksLikeShellTool`),
  resolved `arguments = {"command":["bash","-lc","cat <path>"], "workdir":...}`
  (string-encoded JSON inside `arguments`).
- `codexCommandLineFromFields` extracts `bash -lc cat <path>`;
  `normalizeLayer0CommandLine` strips the bash wrapper to `cat <path>`;
  `filter.ReadPathFromCommandLine` yields the path.
- Read #1: cache cold -> `compactProxyReadDelta` returns no change; fallback
  `compactCodexExecEnvelope` + filter chain reduces 35567b -> 6558b (81%).
- Read #2/#3: cache warm -> `compactProxyReadDelta` returns DecisionBlock with
  reason `"Slimference delta for <path>:\n+ Chunk ID: <new>\n- Chunk ID: <prev>\n
  Full content: local-archive://..."`, 35567b -> 144b (99%) each.

DO NOT chase the big repeated block: the ~117KB body is mostly `instructions` +
`tools`, repeated per request, but `prompt_cache_key` is OpenAI's server-side
prompt cache for exactly that - they already discount it, local dedup of it
saves the user nothing billable. The real lever is the tool-output deltas
across turns; that lever is now operationally proven.

T247 remaining work (no reducer code change needed):
1. DONE (commit `fee1af4`): fixture-based regression test
   `internal/proxy/wsmitm_phasef_real_capture_test.go::TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`
   replays a synthetic three-turn multi-read sequence with the production-proven
   real exec_command shape and asserts the delta-marker reduction for reads #2
   and #3. Isolated via `t.TempDir()`; no private data committed; ~0.10s.
2. One Desktop pass on the identical Phase-F route once a user-confirmed Desktop
   session is run via TUI Launch Codex App (T246 follow-up).
3. Quantify savings on the OTHER layers (HTTP-path L0/L1, response cache,
   output-reduce on non-repeat-read sessions) so the product savings claim is
   grounded in aggregate measurement, not in `wss_certified=true` plus a single
   repeat-read example. Required before T240 release certification.
   Tooling landed (commit `e651483`):
   `go run ./scripts/utils aggregate-savings [--filter-db=<path>] [--json]`
   gives a single-glance honest report of live WSS Phase-F counters,
   output-reduce sub-layer counters, and offline HTTP-path Layer-0 filter
   savings, with workload caveats and a HEALTH WARN line on parse/degrade
   errors. Use `--admin-state-file=<path>` for reproducible offline runs
   during cert ceremonies. Remaining: collect real-workday measurements with
   this tool over a representative Codex session window.

Honest workload calibration to keep in messaging: Slimference WSS Phase-F savings
for Codex are ~0 on sessions without repeat reads (only F01-F24 filter hits can
fire in that case); large (94% measured) on sessions with repeat reads. The cert
is operationally meaningful, not a single-frame artefact.

---

## 10. Critical non-obvious mechanisms (do not relearn these the hard way)

1. COUNTER FLUSH TIMING. The WSS byte/frame/mutation counters live on the
   per-conversation `wsmitm.Session` and aggregate into the dispatcher snapshot
   (`/admin/state`, `codex desktop status .wss`) only at SESSION END (when the WS
   closes). While Codex keeps the WS open, `bytes_c2s`/`c2s_frames`/`frames_reencoded`/
   `compressed_messages_*` read ZERO in the snapshot even during an active, working
   conversation. EXCEPTION: `phasef_bridged` increments at session START
   (`proxy.go` FrameBridge closure) and is the only reliable mid-session signal.
   CONSEQUENCE: to measure bytes/mutation, CLOSE the session (quit Codex.app / let
   the CLI exit) before reading. Many earlier "zero bytes" conclusions in this
   project were this artifact.

2. RELIABLE MEASUREMENT METHOD: (a) restart the daemon fresh so counters baseline at
   0; (b) run the conversation; (c) close the session; (d) poll `codex desktop
   status .wss` ~15s for flushed values. Do not trust mid-session snapshots, and do
   not trust short delta windows in `prove --finish` (they lag). Socket evidence
   (`lsof` for `127.0.0.1:8990` vs direct `chatgpt.com`) and the decisions-log
   `route_mode` are ground truth.

3. PERMESSAGE-DEFLATE. Codex WSS frames are compressed (first client frame byte
   `0xc1`, RSV1 set). CLI and Desktop use the IDENTICAL format. Slimference inflates
   them in `wsmitm/session.go` (the `compressed_messages_inspected` counter proves
   the inflate path runs). So compression is not the discriminator (an earlier theory
   that `enable_request_compression` broke things was counter-noise; the fix for it
   was reverted).

4. GATE SEMANTICS (honesty). `desktop_app_server_route_ready` = launch-eligible,
   route proven, NOT a savings claim. `desktop_app_server_proven` = full mutation
   proven = "WSS savings". The TUI must never sell route_ready as savings. This was
   an external-review correction; keep it.

5. RESPONSES API DELTA MODEL: `previous_response_id` means each request is a delta.
   This is the architectural reason the dedup reducers do not fire (§9).

6. FLAKY TEST: `TestStartCodexDesktopProcessRejectsImmediateExit`
   (`codex_desktop_launcher_test.go`) is timing-flaky under full-suite parallel load
   (start-probe `codexDesktopStartProbeDelay`+`Wait4` races the fake process exit).
   Passes 5/5 in isolation. Pre-existing (last touched `e1633ef`), not a regression.
   If CI trips on it, it is a flake; worth making deterministic later.

---

## 11. Where every finding is documented (verified current 2026-05-23)

- Architecture / client support / TUI / shim mechanism: `docs/documentation.md`
  (Codex Desktop bullet ~line 70; TUI section ~line 1169; app-server shim section
  ~line 1456 - all corrected this session to the route-proven story).
- Install/uninstall + Desktop product truth: `docs/install.md` (Desktop section
  ~line 279-360, corrected this session; spec-synced by `docs/install_spec_test.go`).
- TASK overview: `docs/todo.md` (T246 line ~1405, T247 line just after).
- T246 full detail (root cause, fix, gate, proven facts, flake note):
  `docs/todo/t246-codex-desktop-app-server-shim-proof.md`.
- T247 full detail (root cause, session-key fix, plan, cert-repro step,
  prompt-cache note): `docs/todo/t247-codex-wss-reducer-efficacy.md`.
- Chronological evidence (the deepest record): `docs/operation-log.md`, dated
  entries 2026-05-22 and 2026-05-23 - Phase-0 diagnostics, routing breakthrough,
  corrected finding, gate fix, mechanism reference, decisive product-wide finding,
  Responses-API delta root cause, honesty pass, mechanism reference.
- Doc-currency check this session: a grep for stale negative-Desktop / overclaim
  phrases across `docs/*.md` (excluding the historical operation-log) returned
  NOTHING - no remaining SSOT contradictions. `go test ./docs` green (install spec
  test passes).

---

## 12. Exact next steps

1. Confirm T246 end-to-end once with the user: `slimference` TUI -> Launch Codex App,
   do a real conversation, verify the button works and feels native (this is the one
   remaining human confirmation for T246).
2. DONE 2026-05-23 (chain shape-read + cert-repro + multi-read capture). The
   function_call -> remembered tool_use -> function_call_output -> tool_result
   -> commandLine -> readcache -> mutation chain is operationally proven on real
   Codex 0.133.0 CLI traffic. See §9 for the verified shape and counters, and
   `docs/operation-log.md` 2026-05-23 (later) for the full method, capture, and
   honest calibration of the earlier "0 mutations" reading.
3. DONE 2026-05-23 (commit `fee1af4`). The T247 fixture-based regression test
   landed at
   `internal/proxy/wsmitm_phasef_real_capture_test.go::TestWSPhaseFRealCodexMultiReadProducesDeltaMarker`.
   It seeds three exec_command function_calls via `response.output_item.done`
   frames, replays three `function_call_output` c2s requests with the Codex exec
   envelope wrapping a synthetic ~57KB markdown payload, and asserts that reads
   #2 and #3 mutate, shrink, and carry the `"Slimference delta for <path>"`
   marker. Readcache and content-archive are isolated to `t.TempDir()` via
   `proxyUserHomeDir`. Runs in ~0.10s. Locks in the production-proven shape:
   `arguments` is a JSON-encoded STRING with `cmd` as a single-string shell
   command (`cat <path>`), NOT a `bash -lc` wrapped array.
4. Run one Desktop pass on the identical Phase-F route via TUI Launch Codex App
   once the user is ready, and record the flushed counters; expected behaviour
   is identical to the proven CLI path (T246 already proved the route is
   identical).
5. Quantify savings on the OTHER layers using `scripts/utils aggregate-savings`
   (commit `e651483`): live WSS counters + live output-reduce counters + offline
   HTTP-path Layer-0 SQLite savings + USD estimate. Run over a representative
   Codex workday with `--filter-db=~/.slimference/filter.db --period=today` to
   collect honest aggregate numbers before T240 release certification.
6. Keep all measurement flush-aware (close the session before reading). Use
   socket + decisions-log evidence, not laggy counters.
7. T240 (release certification) comes AFTER the fixture test plus aggregate
   measurement. It should certify "CLI+Desktop route-ready/no-drawback, WSS
   read-delta savings proven on repeat-read workloads, aggregate savings =
   <measured>" with the workload-dependence stated explicitly so no fake
   universal-savings claim ships.

---

## 13. Guardrails - what NOT to do (hard)

- No global MITM / `/etc/hosts` / pfctl / system proxy / persistent env as a product
  path (AGENTS.md §9). Scoped only. Browser ChatGPT and ChatGPT.app stay direct.
- Never touch `~/.codex/AGENTS.md` or `research/rtk-ai/rtk/`.
- Never claim savings that are not measured. Distinguish route-ready from
  savings-proven. No fake/cosmetic green.
- The shim must stay fail-open (any rewrite ambiguity -> pass bytes through) and must
  never touch realtime/voice threads.
- Do not "fix" measurement confusion by trusting the laggy mid-session counters;
  close the session and use ground truth.
- Surgical in-place edits; no full-file rewrites; read before editing.

---

## 14. Verification status (honest, what was actually checked on 2026-05-23)

- git at initial handover write: branch `main`, tree clean after source commit
  `5349a5a`; the handover file itself was committed as `214765e`. Reconfirm
  live HEAD before work.
- Current shim behaviour: re-read `rewriteCodexDesktopThreadStart` and the shim argv
  line-by-line - rewrites ONLY modelProvider (compression-config change was
  reverted), argv is provider-block-only. Confirmed.
- `wsCodexSessionID`: re-read; extracts prompt_cache_key + client_metadata. Confirmed
  with a passing unit test.
- File inventory + line counts: confirmed via `wc -l` (cited in §5).
- Doc currency: grep for stale negative-Desktop/overclaim phrases across active docs
  returned nothing; `go test ./docs` green. Confirmed.
- Build/vet/test: `go build ./...` clean, `go vet ./...` clean, `go test ./...` green
  except the known flaky launcher test; `go run ./scripts/ci` 8/8 PASS. Confirmed
  earlier this session.
- The deep findings (counter flush timing, Responses-API delta model, mutation=0 for
  CLI+Desktop, cert rests on 1 frame, session-key bug) were each established by live
  measurement this session and are recorded in `docs/operation-log.md` with the
  exact numbers.

Open / not yet proven (explicitly): real per-turn WSS Phase-F mutation/savings on
either CLI or Desktop (that is T247). One real end-to-end TUI Desktop launch
confirmation with the user (T246). Do not represent these as done.
