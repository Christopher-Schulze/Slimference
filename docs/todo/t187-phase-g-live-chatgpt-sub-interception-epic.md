# TASK 187: Phase G epic - live ChatGPT-Sub WebSocket conversation interception

Status: PLANNING 2026-05-16
Priority: P0 (blocks "max von slimference nutzen" goal with ChatGPT subscription auth)
Scope: epic that pulls T188-T195 into one shippable user-visible target

## Why (the actual user goal)

The user wants to:

1. Use Codex CLI **and** Codex Desktop App with ChatGPT subscription auth (no API key).
2. Get the full Slimference benefit (all Phase F mechanisms + L1/L2/L3) on every
   model-conversation turn.
3. Manage everything from the Slimference TUI: install, status, per-app toggle,
   uninstall, statistics.
4. Have all the **other** Codex Desktop traffic - voice / realtime, computer
   use, browser use, microphone / transcription, image generation, plugin
   installs, model listings - pass through **byte-equal**.
5. Outbound traffic to OpenAI must be **indistinguishable** from normal Codex
   traffic (TLS fingerprint, headers, HTTP/2 frame ordering, WebSocket
   subprotocol, ALPN, timing).
6. Light resource footprint.

## What Codex 0.130 changed (vs the existing T122/T131/T133 plans)

The existing transparent-mode plans were written before Codex 0.130 shipped its
new wire transport. Live re-audit (2026-05-16):

- ChatGPT-auth conversation traffic now defaults to **WebSocket transport**
  carrying the Responses API. Header:
  `responses_websockets=2026-02-06`.
- The conversation base URL is **hardcoded** in
  `codex-rs/model-provider-info/src/lib.rs` as
  `pub const CHATGPT_CODEX_BASE_URL: &str = "https://chatgpt.com/backend-api/codex";`.
  Neither `chatgpt_base_url` nor `openai_base_url` config keys redirect it.
- WebSocket connect path in `codex-rs/codex-api/src/endpoint/responses_websocket.rs`
  uses `tokio_tungstenite::connect_async_tls_with_config` directly. **It does
  NOT honor `HTTPS_PROXY`/`HTTP_PROXY` env vars.**
- TLS stack is rustls + `rustls-native-certs` for HTTP and
  `rustls-tls-native-roots` for WebSocket - both **honor the macOS system
  trust store**, so a CA installed in Keychain works.
- `built_in_model_providers` ChatGPT entry has `supports_websockets: true` and
  is **not overridable** by user config (`merge_configured_model_providers()`
  uses `.or_insert()` for non-Bedrock providers).
- The only way to disable WebSocket transport is internal session-scoped
  fallback after retry budget exhaustion - no config knob, no env var.

Net effect: **HTTPS_PROXY-based interception is not enough**. The path must
be TLS-MITM at the network level (CA-trusted + DNS or pfctl redirect).

## Target user experience

```
$ slimference                # TUI launches
  ┌──────────────────────────────────────────────────────────────┐
  │ Slimference — Token Optimizer for LLM CLIs/Apps             │
  ├──────────────────────────────────────────────────────────────┤
  │ Setup status                                                 │
  │   CA installed in Keychain        ✓  (expires 2027-05-16)   │
  │   Daemon (launchd)                ✓  running, pid 40860     │
  │   Transparent listener            ✓  127.0.0.1:443          │
  │   Network redirect                ✓  hosts + chatgpt.com    │
  │                                                              │
  │ Per-app integration                                          │
  │   [x] Codex CLI                   ✓  intercepting            │
  │   [x] Codex Desktop App           ✓  intercepting            │
  │   [ ] Claude Code                 -  not yet enabled         │
  │                                                              │
  │ Today's savings                                              │
  │   Input tokens saved              412 318  (-31 %)           │
  │   Output tokens saved              94 207  (-22 %)           │
  │   Total estimated cost saved         $7.84                   │
  │   Sessions intercepted                    23                 │
  │                                                              │
  │ [I] Install/repair    [U] Uninstall    [S] Stats             │
  │ [A] Per-app config    [R] Reload       [Q] Quit              │
  └──────────────────────────────────────────────────────────────┘
```

Every state above is testable, reversible, and visible.

## Target state

A new `Phase G` block of work delivers, end-to-end:

1. **CA lifecycle** (`internal/tlsca`): generate root, sign per-SNI leafs,
   install in macOS Keychain via `security add-trusted-cert`, uninstall
   removes the cert. Track install state, expiry, fingerprint in the TUI.

2. **Transparent :443 listener** (`internal/transparent` + new
   `internal/proxy/transparent_listener.go`): bind 443 either with
   privileged-port helper or via `pfctl` `rdr` from 443 to 8990; terminate
   TLS using on-the-fly SNI-signed leaf; identify the wire shape (HTTPS POST,
   WebSocket upgrade, raw passthrough) and dispatch.

3. **Smart SNI / path router** (T189): per-domain + per-path dispatch.
   - `chatgpt.com/backend-api/codex/responses` (HTTPS POST or WSS) → MITM.
   - `chatgpt.com/backend-api/codex/{realtime,images,plugins,memories,models}`
     → transparent TLS passthrough.
   - `chatgpt.com/*` (browser web app, anything else) → transparent
     passthrough.
   - `api.openai.com/v1/chat/completions` / `/v1/responses` → MITM (Codex
     CLI API-key flow + other OpenAI-API consumers).
   - `api.anthropic.com/v1/messages` → MITM (only when Claude Code app
     toggled on).
   - Everything else → transparent passthrough.

4. **WebSocket conversation MITM** (T188): terminate `wss://chatgpt.com/
   backend-api/codex/responses`, decode frames, route messages through Phase F
   pipeline (T165 stop-seq inject, T166 streamcut delay-buffer, T167/T183
   repdet rewrite, T170 stale-read aging, T174 obsolete-prune, T169 be-terse
   gated), re-encode and forward to real upstream over fresh WSS.

5. **Indistinguishability** (T190): outbound TLS ClientHello matches what
   Codex 0.130 itself would send (uTLS profile per platform/version,
   updated catalog), HTTP/2 SETTINGS frame ordering matches, WebSocket
   `Sec-WebSocket-Extensions` / `Sec-WebSocket-Protocol` / ALPN match,
   header order matches, timing fingerprint preserved.

6. **TUI v2** (T191): install / status / uninstall, per-app toggle (Codex
   CLI / Codex Desktop App / Claude Code), live stats dashboard, repair
   diagnostic.

7. **Codex Desktop traffic discrimination** (T194): explicit test suite
   that voice / realtime / computer-use / browser / image-gen / plugin
   traffic never enters our compression pipeline; runtime guard if
   misrouted.

8. **Resource footprint budget** (T195): RSS ≤ 200 MB steady-state, p50
   added latency ≤ 5 ms, p95 ≤ 25 ms vs direct chatgpt.com baseline.

9. **Reversibility** (T196): every install step has a clean uninstall
   that restores prior state byte-equal. Snapshot-diff verified.

## Constraint: indistinguishability (the load-bearing requirement)

OpenAI must not be able to distinguish our outbound traffic from a direct
Codex/Desktop-App request. This is a multi-layer requirement:

- **TLS-layer**: ClientHello must match Codex's actual fingerprint (uTLS
  profile). Our existing `internal/tlsdial` + uTLS profile catalog handles
  this but the catalog needs refresh for Codex 0.130's rustls fingerprint.
  Cross-validate via T123 `cat-tls-fingerprint` + an external probe.
- **HTTP/2-layer**: SETTINGS frame fields, ordering, header table size,
  push-enabled bit must match. `internal/tlsdial` provides hooks.
- **WebSocket-layer**: subprotocol negotiation, extension list order
  (`permessage-deflate; client_max_window_bits`), Sec-WebSocket-Version
  must match Codex.
- **Header-layer**: order of `user-agent`, `x-codex-*`, `accept-encoding`,
  `accept-language` etc. must match Codex byte-for-byte. Easiest way: pass
  Codex's headers through untouched, only add/remove what we strictly need.
- **Timing-layer**: don't add noticeable delay. Outbound dial latency budget
  ≤ 5 ms. Use connection reuse.
- **Body-layer**: when we mutate the request body (stop_sequences, be-terse
  hint), the JSON must be re-marshalled in a way that matches Codex's own
  serialization style (key order, whitespace). Verify with byte-diff against
  recorded Codex traffic.

This is why T190 is its own task: live audit against a recorded Codex baseline.

## Work packages (cross-references)

- **T188** WebSocket conversation MITM wire
- **T189** Smart SNI / path router
- **T190** Indistinguishability live audit
- **T191** TUI Setup Wizard v2
- **T192** Stats Dashboard v2
- **T193** Per-app activation state machine
- **T194** Codex Desktop sideband bypass certification
- **T195** Resource footprint budget
- **T196** Full reversibility audit

Existing related (CODE-COMPLETE / live-cert pending):
- T122 (transparent mode foundations)
- T123 (TLS fingerprint mimicry)
- T131 (transparent runtime closure)
- T133 (TUI daemon control plane v1)
- T139 (TLS provider-edge proof)
- T140 (open: live Codex/App proof)

Phase G **builds on** those - it does not re-do the foundations, it closes the
gap between "the foundations exist" and "Codex 0.130 + ChatGPT-Sub + Codex
Desktop App + full Phase F gain works end-to-end on a real Mac".

## Acceptance (epic-level)

- A clean install on a fresh Mac with no Slimference history takes ≤ 3 minutes
  start to finish, requires one sudo prompt for keychain trust.
- After install, `codex exec "echo hi"` flows through the proxy. Admin
  status shows non-zero stop_seq_requests_modified counter after one turn.
- After install, `codex` (Codex Desktop App) opens a session, sends a
  conversation; the proxy intercepts. Microphone-based dictation in the
  same app continues to work (passthrough verified by zero packets routed
  through MITM for `/realtime/*`).
- After install, the user opens `https://chatgpt.com` in Safari: web UI
  loads and works (transparent passthrough, no MITM).
- OpenAI cannot distinguish our outbound from a direct Codex request: TLS
  fingerprint matches, headers byte-equal where Codex sets them, request
  body marshaling differs only in fields we deliberately added.
- Resource footprint: RSS ≤ 200 MB after 100 conversation turns; p95 added
  latency ≤ 25 ms; idle CPU ≤ 0.5 %.
- Uninstall via TUI restores prior network/keychain/hosts state byte-equal.

## Notes

- Until Phase G ships, the current ChatGPT-Sub Codex 0.130 path is
  **NOT actually intercepted** by Slimference (the path-level config keys do
  not redirect the WebSocket conversation transport, source-verified).
- Phase F mechanisms (T165-T186) are landed in the proxy code and
  unit-tested. Phase G is the wire that lets them fire in production for
  ChatGPT-Sub users.
- Hooks (PreToolUse / PostToolUse / Read / SessionStart) work independently
  of Phase G and give a partial Layer-0 win today. Phase G adds everything
  else.

## Deviations

(none yet)
