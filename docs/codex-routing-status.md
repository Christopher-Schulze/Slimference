# Codex CLI Routing Status (2026-05-16)

Legacy investigation note. This file records why the old
`openai_base_url` / `chatgpt_base_url` config-patch route stopped catching
Codex ChatGPT-subscription conversation traffic. It is not the install
guide. Current scoped Codex setup is documented in `docs/install.md` and
uses Codex hooks plus `slimference codex run` for one-shot CLI traffic.
`slimference codex enable` writes the reversible advanced shared Codex
CLI/App provider route. Transparent SNI-MITM remains global lab-only because it
routes `chatgpt.com` machine-wide.

## TL;DR

With Codex CLI **0.130 + ChatGPT subscription auth**, the historical wiring
recipe (`openai_base_url` / `chatgpt_base_url` pointing at `127.0.0.1:8990`)
**does not intercept conversation traffic.** Codex 2026 hardcodes the
ChatGPT-auth conversation endpoint and routes it over WebSocket transport.

This file documents what I verified, why it bypasses, and what works today.

## What I tested

1. Built the proxy fresh with all today's changes (T165/166/167/170/174/
   169/183/184/185/186 + audit fixes + Codex-path-tightening).
2. Started it on `127.0.0.1:8990` (confirmed listening via lsof).
3. Set in `~/.codex/config.toml`:
   ```toml
   openai_base_url = "http://127.0.0.1:8990/backend-api/codex"
   chatgpt_base_url = "http://127.0.0.1:8990/backend-api/"
   ```
4. Ran `codex exec --skip-git-repo-check "say only OK"`. Response was
   successful, 27 977 tokens used.
5. Checked the proxy log: **0 bytes**. No TCP connection from Codex.
6. Checked `/admin/status.output_reduce_counters`: **all zero**. No
   request hit the handler.

## Why it bypasses (source-verified)

From `openai/codex` on GitHub (read 2026-05-16):

- `codex-rs/model-provider-info/src/lib.rs` defines
  `pub const CHATGPT_CODEX_BASE_URL: &str = "https://chatgpt.com/backend-api/codex";`
  This constant is the ChatGPT-mode OpenAI provider's base URL. It is
  **not** built from `chatgpt_base_url` — it's a literal.
- `codex-rs/core/src/client.rs` sets
  `RESPONSES_WEBSOCKETS_V2_BETA_HEADER_VALUE = "responses_websockets=2026-02-06"`
  and defaults `disable_websockets = false`. Codex 0.130 opens a
  WebSocket to the constant base URL above.
- `chatgpt_base_url` is consulted by several **sideband** endpoints
  (`chatgpt/src/chatgpt_client.rs`, `memories/*`, `core-plugins/*`,
  `cli/login.rs`, `mcp_openai_file.rs`, agent-identity JWKS fetch).
  Those go through the proxy correctly. But the conversation does not.
- `openai_base_url` overrides the **API-key** OpenAI provider only
  (used when `OPENAI_API_KEY` is set or `--profile` selects an API-key
  provider). Not used in ChatGPT-subscription flows.

## What still works (with the proxy idle)

- **Hooks**: PreToolUse / PostToolUse / SessionStart / Stop are wired in
  `~/.codex/hooks.json` and call into `slimference`. These fire locally
  on every Codex tool invocation. The Layer-0 filter / rewrite path
  still operates on local tool output without needing the proxy.
- **Sideband traffic**: if you re-enable `chatgpt_base_url` and the
  proxy is running, plugin manifests / memories / login redirects do
  flow through us. None of this is compression-worthy traffic.

## Paths that would actually work

1. **API-key auth + model-provider profile** (Codex's official proxy
   pattern — see `codex-rs/responses-api-proxy/README.md`):
   ```toml
   [model_providers.slimference]
   name = "slimference"
   base_url = "http://127.0.0.1:8990/v1"
   wire_api = "responses"

   [profiles.slimference]
   model_provider = "slimference"
   ```
   Then `codex -p slimference`. Caveat: bills via API key, not via
   ChatGPT subscription. Works only over HTTP (no WebSocket).

2. **TLS MITM via our CA cert** (`internal/tlsca`): install the
   Slimference CA in macOS trust, then a network-level redirect
   (PAC, pfctl, or per-process DNS) sends `chatgpt.com` traffic to
   `127.0.0.1:8990`. Heavy setup, security-sensitive.

3. **Wait for OpenAI** to expose `experimental_disable_responses_websocket`
   plus a config knob for the ChatGPT-auth conversation base URL. No
   such knob exists in 0.130.

## Today's state

- Daemon stopped.
- `~/.codex/config.toml` config keys commented out with explanation.
- Hooks remain installed and operational (Layer-0 / rewrite path).
- All 49+ test packages still green; binary works.
- All today's code changes (T165-T186, audit fixes, Codex-path-
  tightening) are in the tree and would work the moment we have a
  routing solution.

## Superseded decision

This 2026-05-16 recommendation is superseded by the scoped Codex WSS product
path documented in `docs/install.md`.

Current truth:

- Codex CLI no longer relies on the old top-level config patch route. The green
  path is `slimference codex run --transport=auto -- ...`, which prefers
  certified WSS Phase-F savings, then WSS byte-equal bridge, then HTTP, then
  direct fail-open.
- Codex CLI auto-recert keeps the strict version tuple guard but repairs WSS
  Phase-F after Codex updates when live proof is clean.
- Codex Desktop no longer relies on the old proxy/CA branch. The current green
  Desktop product path is the process-local app-server shim launched by
  Slimference. It rewrites only default/null `thread/start.modelProvider` to
  `slimference-codex`, keeps Finder/Spotlight launches direct, and routes the
  prompted Desktop conversation onto the same no-CA WSS Phase-F route as CLI.
- On 2026-05-29 a real Codex.app repeat-read proof on Codex 0.135.0 returned
  `desktop_app_server_phasef_proven`, `desktop_savings=true`,
  `frames_reencoded=3`, `compressed_messages_mutated=3`, and
  `phasef_mutations=3` with zero parse/degrade/compression errors.
- TUI Launch Codex App may use the app-server shim path when the proof gate is
  green. Normal Finder/Spotlight Codex.app remains the direct no-drawback
  Desktop path, and Browser ChatGPT / ChatGPT.app remain untouched.
