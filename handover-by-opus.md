# Slimference - Handover by Opus (deep, current, verified)

Stand: 2026-05-23. Branch `main`, working tree clean, last commit `5349a5a`.
Author: Opus 4.7. This file was written after line-level re-reading of the code
and docs it cites; the "Verification" section at the end states exactly what was
checked. No sampling, no guessing: every claim here was confirmed against the repo
on 2026-05-23 or is explicitly marked as an open question.

This document exists because the running context had heavy drift. It is the single
place a brand-new agent should read first to know everything: what the project is,
who the user is and what they require, where we are, how it works, in which files,
what is documented and where, what we have proven, and exactly where we are going.

---

## 0. First actions for a new agent

1. Read this file fully.
2. Read `AGENTS.md` (repo root) - it is binding. Pay special attention to §9
   (Verdrahtungs-Doktrin / Scoped Codex). The user's hard guardrails live there.
3. Read `~/.claude/CLAUDE.md` is the user's global agent contract (character,
   working contract, file-ops rules). It overrides defaults.
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

The user (Christopher, dven23@pm.me) is the project owner. Working style and hard
rules come from `~/.claude/CLAUDE.md` and `AGENTS.md`. The non-negotiables that
matter most for this frontier:

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

- Branch `main`, working tree clean, last commit `5349a5a` (2026-05-23).
- This session produced 19 commits across T246 (Desktop routing) and T247 (WSS
  reducer efficacy). Key ones, newest first:
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
  - T247 Codex WSS Phase-F reducer efficacy - OPEN. The real product-value work.
    Prerequisite (session-key) fixed; the read-delta reducer for the Responses-API
    delta shape is the remaining engineering.
- Build/test health (verified this session): `go build ./...` clean,
  `go vet ./...` clean, `go test ./...` green except one timing-flaky test
  (`TestStartCodexDesktopProcessRejectsImmediateExit`, passes 5/5 in isolation,
  pre-existing, see §10). `go run ./scripts/ci` 8/8 PASS, coverage gate green.
- Daemon currently running and healthy on :8990; CLI `wss_certified=true`,
  `auto.mode=wss_phasef` (but see §9 - the cert rests on a single mutated frame).

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

## 9. WSS Phase-F savings (T247) - THE REAL OPEN WORK

Detail file: `docs/todo/t247-codex-wss-reducer-efficacy.md`. Evidence:
`docs/operation-log.md` 2026-05-23 entries.

The decisive finding: WSS Phase-F INSPECTS real Codex traffic correctly but MUTATES
almost nothing - for CLI AND Desktop. Measured 2026-05-23 on fresh daemons, read
after session close:
- CLI exec (recert-style): `compressed_messages_inspected=58`, `phasef_requests=3`,
  but `frames_reencoded=0`, `compressed_messages_mutated=0`, `byte_bridge_only=true`.
- Desktop (93KB file twice): `compressed_messages_inspected=1111`,
  `frames_reencoded=0`.
- The cert `~/.slimference/codex-wss-cert.json` shows `frames_reencoded: 1` - the
  green flag rests on a single mutated frame.

Root cause (found by an env-gated debug dump in `wsmitm_phasef.go:handleRequest`,
since reverted; request bodies captured to `/tmp/wsphasef-body-*.json`): Codex WSS
is the OpenAI Responses API with `previous_response_id` (server-side conversation
state). Each request carries only the DELTA, not the accumulated history. Observed
one-turn sequence: `input=[]` -> `input=[message,message,message]` ->
`input=[function_call_output]` -> `input=[function_call_output]`. No single request
contains repeated history. Slimference's L1/L0 dedup reducers
(`applyProxyLayer0WithSessionAndToolUses`, read-delta, MinHash dedup) are built for
the Chat-Completions shape where the FULL history (with repeated tool outputs) is in
every request. Against delta requests they find nothing to reduce.

The mutation decision path (so you know exactly where to work):
- `wsmitm/session.go:finishCompressedMessage` inflates a permessage-deflate message,
  calls the handler (`wsPhaseFAdapter.handle` in `wsmitm_phasef.go`), and only
  mutates (CompressedMessagesMutated++, FramesReencoded++) when the handler returns
  `replace=true` (i.e. the reducer produced a shorter, schema-safe body).
- `wsPhaseFAdapter.handleRequest -> applyInputPipeline` runs, in order: staleread
  aging, obsolete-read prune, `applyProxyLayer0WithSessionAndToolUses` (L0), stop-seq
  injection, be-terse hint. All OutputReduce sub-flags default TRUE except
  be-terse (`internal/config/defaults.go`: stop_seq, repdet, stale_read_aging,
  obsolete_read_prune = true; be_terse = false). So config is NOT the blocker.
- L0 (`layer0_proxy.go`) compacts `tool_result` blocks whose `tool_use` resolves to a
  command line, via read-delta against a per-session read context keyed by sessionID.

Prerequisite bug found + fixed (commit `b5213e8`): `wsCodexSessionID`
(`wsmitm_phasef.go:351`) returned "" for every Codex WSS request - it looked for
`conversation_id`/`session_id`/`user_id`, but Codex's stable per-thread key is
`prompt_cache_key` (mirrored in `client_metadata.x-codex-turn-metadata`). With an
empty key the per-session read context could not accumulate across the delta
requests. Now it extracts `prompt_cache_key` then the client_metadata thread/session
id (with a unit test `TestWSCodexSessionIDFromCodexResponsesShape`). VERIFIED the
session key is now populated (`codex-wss:019e51d6-...`). NECESSARY but NOT yet
SUFFICIENT - mutation is still 0, because the read-delta still does not match the
`function_call_output` deltas.

Why mutation still 0 after the session-key fix (the remaining engineering):
- The `function_call_output` request has `tool_uses=0` (the originating
  `function_call` is in a prior response, referenced by `previous_response_id`, not
  in this request). The adapter DOES `rememberToolUsesFromResponse`, so the tool_use
  can in principle be resolved from remembered responses - but the read-delta across
  delta requests needs that resolution plus a remembered prior tool-output to delta
  against, keyed by the now-correct session id.

T247 plan (in the detail file; step 1 first, it is cheapest and most informative):
1. Reproduce the cert's single mutation: re-run the exact recert/cert path, fresh
   daemon, close cleanly, check if `frames_reencoded:1` reproduces, and trace WHICH
   frame mutated. That one path is the only thing that works today; it anchors the fix.
2. Make the read-delta reducer compact a `function_call_output` delta against the
   remembered prior tool-output of the same command/file (resolve tool_use from
   remembered responses; key the read context by session id).
3. Fixture-test against a REAL captured Codex WSS delta sequence (not the
   Chat-Completions shape).
4. Re-measure live (CLI then Desktop - route identical) with the flush-aware method.
5. Quantify the OTHER layers (HTTP-path L0/L1, response cache, output-reduce) so the
   savings claim is grounded in measurement, not in `wss_certified=true`.

DO NOT chase the big repeated block: the ~117KB body is mostly `instructions` +
`tools`, repeated per request, but `prompt_cache_key` is OpenAI's server-side prompt
cache for exactly that - they already discount it, local dedup of it saves the user
nothing billable. The real lever is the tool-output deltas across turns.

Higher-order honest question to keep in mind: if even content-rich sessions never
mutate after the reducer work, then WSS Phase-F is the wrong lever for Codex and
savings must come from other layers; decide and document that for T240 release cert.

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
2. Start T247 (the real value work) with step 1 = cert-reproduction (cheapest, most
   informative). Then the read-delta reducer for the Responses-API delta shape.
3. Keep all measurement flush-aware (close the session before reading). Use socket +
   decisions-log evidence, not laggy counters.
4. Separately measure non-WSS layers so the product savings claim is honest.
5. T240 (release certification) comes AFTER T247 resolves the savings reality: it
   should certify either "CLI+Desktop savings proven" or, honestly, "CLI+Desktop
   route-ready/no-drawback, WSS savings = <measured value>".

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

- git: branch `main`, tree clean, last commit `5349a5a`. Confirmed.
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
