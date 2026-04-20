# T66 - Codex Upstream Routing

Status: todo
Priority: P0
Scope: `internal/proxy/provider.go`, `internal/proxy/handler.go`, `internal/proxy/upstream.go` (new), `internal/config/`
Driver: T65 wires Codex to point at Slimference - Slimference must then route both Codex request shapes back to the real upstream correctly.

---

## Problem

Current proxy assumes two providers:

- **Anthropic**: path contains `/messages`, upstream = `https://api.anthropic.com`.
- **OpenAI (API)**: path contains `/chat/completions`, upstream = `https://api.openai.com`.

With `openai_base_url = http://127.0.0.1:8990` in `~/.codex/config.toml` (T65),
Codex starts sending to Slimference. But Codex is a ChatGPT-subscription product,
not an OpenAI-API product. Its real upstream is `https://chatgpt.com` with path
prefixes like `/backend-api/codex/responses`, not `api.openai.com/v1/...`.

Left unhandled:

1. `detectProvider` mis-classifies Codex traffic.
2. `upstreamURL` points at `api.openai.com` and will return 401/404.
3. Codex's bearer token (OAuth2 JWT from `~/.codex/auth.json`) must flow through
   the Authorization header unchanged so Cloudflare and OpenAI see the same
   identity they always see from this user.

## Target State

- New `types.CodexChatGPT` provider constant (or extend the enum).
- `detectProvider` recognises Codex traffic by path prefix `/backend-api/codex/`
  or by header `OpenAI-Beta: codex` (whichever Codex actually sends).
- `upstreamURL` routes Codex to `https://chatgpt.com` (or the configured
  `chatgpt_base_url` / `openai_base_url` from the upstream config block).
- `Authorization: Bearer <jwt>` header is forwarded verbatim.
- Every Codex request shape known in the wild is classified and passes through
  Layer 1 (dedup, structure, delta) without mutation failures.
- Cloudflare-observable attributes (User-Agent, TLS fingerprint, request
  ordering) are preserved so the existing OpenAI account is not flagged.

## Design

### Provider detection

`detectProvider(path, body, headers)` signature extended to take headers. New
classification order:

1. Path starts with `/backend-api/codex/` → `types.CodexChatGPT`.
2. Path contains `/messages` → `types.Anthropic`.
3. Path contains `/chat/completions` → `types.OpenAI`.
4. Header `anthropic-version` present → `types.Anthropic`.
5. Fallback body-probe as today.

### Upstream URL

```go
func (p *Proxy) upstreamURL(provider types.Provider, path, query string) string {
    var base string
    switch provider {
    case types.Anthropic:
        base = p.config.Upstream.Anthropic.BaseURL
    case types.OpenAI:
        base = p.config.Upstream.OpenAI.BaseURL
    case types.CodexChatGPT:
        base = p.config.Upstream.CodexChatGPT.BaseURL
    }
    // ...
}
```

Config gets a third `ProviderUpstream` block:

```toml
[upstream.codex_chatgpt]
base_url = "https://chatgpt.com"
```

Default `https://chatgpt.com`.

### Header preservation

Slimference already forwards every header from the client request except the
ones it strips for its own purposes. Extend the strip-list to include:

- `Host` (must be rewritten to the upstream's authority).
- Any `slim-*` prefixed internal headers.

Preserve:

- `Authorization` (bearer JWT).
- `User-Agent` (Codex-native, do NOT replace with Slimference UA).
- `OpenAI-Beta` / `OpenAI-Organization` / `X-Correlation-ID` (whatever Codex sends).
- `X-OpenAI-Client-Version`.
- `Cookie` (in case Codex uses cookie-based session refresh).
- `Accept-Encoding` / `Accept`.

### Request-body handling

`/backend-api/codex/responses` uses OpenAI's chat-completions-like body shape
but with Codex-specific fields (tool-use schema, system-prompt layout). The
existing `extractOpenAIMessages` works as a starting point. The L1 pipeline
handles unknown content blocks via the tolerant union decoder already in
`provider.go`.

Add a regression fixture: a captured Codex request (scrubbed of auth token) in
`testdata/codex_responses_request.json` that the L1 pipeline round-trips
byte-equal when all layers are disabled.

### TLS fingerprint concern

Slimference's Go `http.Client` produces a different TLS fingerprint than
reqwest's hyper-rustls. If Cloudflare WAF learns that `chatgpt.com/backend-api/codex/...`
is only ever hit from rustls-signature clients, a sudden switch to Go might
trip bot-detection.

**Mitigation plan (staged)**:

- Phase 1 (this task): Ship without TLS-fingerprint manipulation. Risk accepted
  by the user. Observe real-world behaviour for 1 week.
- Phase 2 (separate TASK if Phase 1 trips): Integrate `refraction-networking/utls`
  behind a feature flag that reproduces a reqwest ClientHello. Only activate
  on the Codex upstream, not Anthropic.

### Streaming

Codex uses Server-Sent-Events same as Anthropic and OpenAI. The existing
`streamingRelay` path handles this; no change needed beyond the routing fix.

### Admin surface

`/admin/status.providers` already lists enabled providers. Add `codex_chatgpt`
to the map. The `/admin/provider` POST endpoint gains `codex_chatgpt` as a
valid provider name for toggling.

## Implementation Plan

### WP1 - Provider enum extension
- Add `types.CodexChatGPT`.
- Update every switch on `types.Provider` (grep first to catch all sites).
- Ensure no default-panic path is missed.

### WP2 - detectProvider extension
- Path + header checks as specified.
- Unit tests for each branch with fixtures.

### WP3 - Upstream URL + config
- New `ProviderUpstream` block in config.
- Defaults + ENV override (`SLIMFERENCE_UPSTREAM_CODEX_CHATGPT_BASE_URL`).
- Validation (http/https only, no trailing slash).

### WP4 - Header preservation audit
- List every site that sets outgoing headers.
- Verify Codex-critical headers pass through.

### WP5 - Regression fixture
- Capture a synthetic Codex request body via `codex debug prompt-input` (the
  hidden subcommand the research found).
- Store scrubbed fixture under `testdata/`.
- Test: pipeline round-trips byte-equal with all layers off.

### WP6 - Admin surface
- Extend admin JSON + provider-toggle endpoint.

### WP7 - Documentation
- `docs/integration.md` notes that Codex traffic flows through Slimference
  and what Cloudflare risk looks like.

## Risks

- **Cloudflare WAF trips the TLS-fingerprint change** → OpenAI account could be
  temporarily rate-limited. Detection: user sees 403 with cf-ray. Response:
  `slimference integrate remove` + `exec $SHELL -l` reverts everything in
  under 30 seconds. Phase 2 adds uTLS.
- **Codex updates its request shape** and our provider detection mis-routes.
  Mitigation: path-prefix match is resilient; the header fallback catches edge
  cases; the T62 version-negotiation machinery applies the conservative
  pipeline if a completely unfamiliar shape arrives.
- **OAuth token refresh** hits `https://auth.openai.com/oauth/token` (not
  through the proxy because the refresh endpoint isn't covered by `openai_base_url`).
  That's correct behaviour - we don't want to touch the auth flow.

## Acceptance Criteria

- [ ] `types.CodexChatGPT` provider exists and every switch handles it.
- [ ] `detectProvider` classifies a Codex-style path correctly.
- [ ] `upstreamURL` routes to `chatgpt.com` (configurable).
- [ ] `Authorization: Bearer ...` passes through verbatim.
- [ ] Regression fixture round-trips byte-equal with all layers off.
- [ ] `/admin/status` exposes Codex enabled/disabled.
- [ ] `go test -race ./internal/proxy/...` green.
- [ ] Manual: `openai_base_url` in codex config points at Slimference, running
      `codex` hits `/backend-api/codex/responses`, Slimference forwards, Codex
      gets its normal response.

## Out of Scope

- TLS fingerprint mimicry (Phase 2).
- Capturing Codex conversation state across sessions.
- Codex marketplace / plugin traffic (different endpoint, not needed for token
  savings).

---

## Validation

```
go test -race ./internal/proxy/...
./slimference --no-tui &
# in another shell:
codex  # should show normal "who's there?" response, Slimference logs show routing
curl -s 127.0.0.1:8990/admin/status | jq '.providers, .pipeline'
```
