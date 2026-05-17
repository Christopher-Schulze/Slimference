# TASK 189: Smart SNI + path router for transparent listener

Status: PLANNING 2026-05-16
Priority: P0 (gatekeeper for everything that flows through :443)
Scope: new `internal/proxy/sniroute/`, wires into the transparent :443
       listener built in T188, decides per-connection whether to MITM,
       per-path whether to apply Phase F, or to tunnel TLS opaquely

## Why

When the user enables Slimference's transparent mode, every TLS connection
that would have gone to `chatgpt.com` (and selected other LLM endpoints) is
redirected to our local listener. We are now responsible for **every**
TLS handshake on that domain, including:

- Codex CLI conversation traffic (`/backend-api/codex/responses`) - MITM.
- Codex Desktop App conversation traffic (same path, different UA) - MITM.
- Codex Desktop App voice/realtime calls (`/backend-api/codex/realtime/*`) -
  passthrough (audio is latency-sensitive, framing is opaque to us).
- Codex Desktop App computer-use traffic - **mostly** the same `/responses`
  endpoint with `computer_call` items + image content blocks. Conversation
  itself is MITM-eligible **but** the image bytes / screenshots travel as
  base64 inside content blocks and must not be touched by compression.
- Codex Desktop App image generation (`/backend-api/codex/images/*`) -
  passthrough.
- Codex Desktop App plugin install / model listings / memories -
  passthrough.
- Codex Desktop App microphone / transcription - over **WebRTC/UDP**, never
  hits :443 anyway (verified in T122 audit). Listed for completeness.
- Browser `chatgpt.com` traffic (Safari, Chrome on the same Mac, etc.) -
  passthrough, do not touch.
- Anthropic API `api.anthropic.com` - MITM only when Claude Code is toggled
  on (T193).
- OpenAI direct API `api.openai.com` - MITM (covers Codex CLI in API-key
  mode and other consumers).

A wrong decision = either we break a user feature (voice, computer use,
image gen, browser ChatGPT) or we miss out on token savings (untouched
conversation traffic).

## Routing decision data points

For each incoming TLS connection we have, before any application data flows:

1. **SNI** - extracted from ClientHello during our TLS handshake. Always
   present for HTTPS-modern browsers and reqwest/rustls.
2. **Client IP / source port** - localhost, always.
3. **Process owner** (optional, may be unavailable on macOS due to
   sandboxing): try `lsof -i :<sport>` to identify originating process.
   When available, gives the cleanest discriminator: `codex_cli_rs` vs
   `Codex.app` vs `Safari` vs `Google Chrome`. Cache for the connection's
   lifetime. Fail open (no process found = use SNI/UA only).
4. **ALPN** - either `h2` (HTTP/2) or `http/1.1`. WebSocket needs `http/
   1.1`; Codex desktop browser-view uses `h2`.

Once we accept the handshake (we always accept and terminate, because the
user trusted our CA), we read the first HTTP request line + headers:

5. **HTTP method + path** - `POST /backend-api/codex/responses` vs `GET
   /backend-api/codex/models` vs `Upgrade: websocket` upgrade for the WSS
   variant of the same path.
6. **User-Agent** - `codex_cli_rs/...`, `codex_desktop_app/...`,
   `Codex.app/...`, `Mozilla/5.0` (browser), `Safari/...`, `curl/...`.
7. **Subprotocol** (WebSocket only) - `responses_websockets=2026-02-06`
   identifies the conversation channel.

The router uses these in priority order to decide one of:

- `route=mitm_codex_conversation` - apply Phase F.
- `route=passthrough_tls` - re-encrypt to upstream as fast as possible,
  no inspection.
- `route=reject` - off-domain or policy violation; serve a 403. Rare.

## Routing table (declarative)

```
Domain          | Path                                | Method | Subproto | Decision
----------------+-------------------------------------+--------+----------+-----------------------
chatgpt.com     | /backend-api/codex/responses        | POST   | -        | mitm_codex_conversation
chatgpt.com     | /backend-api/codex/responses        | GET    | resp_ws  | mitm_codex_conversation
chatgpt.com     | /backend-api/codex/responses/compact| *      | *        | passthrough_tls (T194)
chatgpt.com     | /backend-api/codex/realtime/*       | *      | *        | passthrough_tls (voice)
chatgpt.com     | /backend-api/codex/images/*         | *      | *        | passthrough_tls (image-gen)
chatgpt.com     | /backend-api/codex/plugins/*        | *      | *        | passthrough_tls
chatgpt.com     | /backend-api/codex/memories/*       | *      | *        | passthrough_tls
chatgpt.com     | /backend-api/codex/models           | *      | *        | passthrough_tls (model list)
chatgpt.com     | /backend-api/codex/analytics-events*| *      | *        | passthrough_tls
chatgpt.com     | /backend-api/wham/*                 | *      | *        | passthrough_tls (remote-ctrl)
chatgpt.com     | /backend-api/* (other)              | *      | *        | passthrough_tls (sideband)
chatgpt.com     | /* (non-backend-api)                | *      | *        | passthrough_tls (web UI)
api.openai.com  | /v1/chat/completions                | POST   | -        | mitm_codex_conversation
api.openai.com  | /v1/responses                       | POST   | -        | mitm_codex_conversation
api.openai.com  | /v1/audio/*                         | *      | *        | passthrough_tls
api.openai.com  | /v1/images/*                        | *      | *        | passthrough_tls
api.openai.com  | /v1/embeddings                      | *      | *        | passthrough_tls
api.openai.com  | /* (other)                          | *      | *        | passthrough_tls
api.anthropic.com| /v1/messages                       | POST   | -        | mitm_codex_conversation (T193)
api.anthropic.com| /v1/messages/batches                | *      | *        | passthrough_tls
api.anthropic.com| /v1/messages/count_tokens          | *      | *        | passthrough_tls
api.anthropic.com| /* (other)                         | *      | *        | passthrough_tls
* (other domain) | *                                  | *      | *        | passthrough_tls
```

Per-app toggle (T193) gates which `mitm_codex_conversation` decisions are
active. With "Codex CLI off, Codex App on, Claude Code off", only requests
from the Codex App UA get MITM'd; CLI requests still flow through but use
the passthrough route.

## Implementation plan

1. **TLS Snoop** (`sniroute/snoop.go`): a minimal TLS ClientHello parser
   that extracts SNI + ALPN without doing a full TLS handshake. Used for
   the early-exit decision "is this a domain we care about at all?". For
   non-LLM domains we don't even bother loading the signer; we just
   transparently TCP-proxy to upstream (resolved via our DoH client).

2. **Pre-handshake decision**: if SNI is not in {chatgpt.com,
   api.openai.com, api.anthropic.com}, take the **fast transparent
   passthrough** path (no decryption, just TCP-bridge to the real upstream
   resolved via DoH). Resource-cheap: no CA signing, no TLS overhead.

3. **Post-handshake decision**: for the three LLM domains, complete the
   TLS handshake using our CA-signed leaf, then read the first HTTP
   request line + Host + Method + Path + UA + Subprotocol. Look up the
   decision in the routing table. Apply per-app toggles.

4. **Route dispatch**:
   - `mitm_codex_conversation` → if HTTPS POST: dispatch to existing
     `handler.go` path. If WebSocket upgrade: dispatch to T188's
     `wsmitm.Session`.
   - `passthrough_tls` → bridge the decrypted bytes to a freshly-opened
     TLS connection to the real upstream. We have already terminated the
     client-side TLS so we now re-encrypt to upstream. Latency-budget
     ≤ 5 ms p50.
   - `reject` → close cleanly.

5. **DoH-backed upstream resolver**: `/etc/hosts` redirects the domain
   to 127.0.0.1, so we cannot use system resolution. We resolve via
   DoH (Cloudflare `1.1.1.1` or Google `8.8.8.8`), cache TTL-respecting,
   share the resolver across the process.

6. **Per-app toggle** (`sniroute/app_policy.go`): config-driven map of
   `app_id → on/off`. App identification via UA prefix or process owner.
   TUI surfaces the toggle; daemon hot-reloads on SIGHUP.

7. **Route decision telemetry**: every connection logs its routing
   decision once at first-byte. `/admin/status.sniroute` exposes per-
   route counts (mitm vs passthrough vs reject) plus the routing table
   in effect.

## Acceptance

- Unit tests covering every row of the routing table (~30 rows).
- Negative test: an unknown subdomain of chatgpt.com (e.g.
  `m.chatgpt.com`) routes to passthrough, not MITM.
- Negative test: a request to chatgpt.com with no SNI (rare but
  legitimate for some clients) falls back to passthrough.
- Integration test: with our CA installed and hosts redirect active,
  `curl https://chatgpt.com/api/auth/session` returns a successful
  passthrough (no MITM artifact in headers).
- Integration test: Codex CLI 0.130 conversation triggers MITM; voice
  call setup goes passthrough.
- Per-app toggle test: Codex CLI off + Codex Desktop App on → CLI
  requests route to passthrough, App requests route to MITM.
- 100% statement coverage on the routing table evaluator.

## Sub-Tasks

- [ ] TLS ClientHello SNI/ALPN extractor (zero-copy where possible).
- [ ] Routing table data structure + evaluator + tests.
- [ ] DoH resolver with cache.
- [ ] Transparent TCP-bridge passthrough path.
- [ ] HTTPS-MITM dispatch reusing existing handler.
- [ ] WSS-MITM dispatch dispatching to T188 wsmitm.
- [ ] Per-app toggle config + reload.
- [ ] Telemetry surface in admin status.
- [ ] Decision audit log (debug-mode).

## Notes

- We could in principle MITM-then-bridge for non-LLM domains too, but
  that incurs latency and TLS-handshake CPU we don't need. SNI-pre-route
  is significantly cheaper for the browser-ChatGPT case.
- Process-owner identification on macOS is unreliable in the general
  case (sandboxed apps, sip-protected lookups). We fall back to UA for
  most decisions. Documented as "best effort".
- The `passthrough_tls` route still requires our CA-signed leaf to be
  served for the client's TLS handshake (because /etc/hosts redirected
  the connection to us). After the handshake we bridge bytes - we
  decrypt and re-encrypt to upstream but don't inspect. This is still
  TLS-MITM in the strict sense (we have plaintext in memory) but we
  don't store, log, or modify it. Documented for transparency.

## Deviations

(none yet)
