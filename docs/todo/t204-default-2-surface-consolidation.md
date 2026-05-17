# TASK 204: Default-config consolidation to 2-surface architecture

Status: PLANNED 2026-05-16
Parent: T200 (Phase H epic)
Scope: `internal/config/defaults.go`, `internal/proxy/proxy.go` (URL
       rewrite paths), `internal/config/config.go` (deprecation
       warnings), test suite, `agents.md`

## Why

User mandate (2026-05-16): **direkt aufs 2-Phasen-Endzustand bauen,
nicht noch eine Zwischenstufe**. The 2-surface architecture is:

1. Hooks (Signal IN) — irreducible
2. Transparent SNI-MITM (Traffic IN) — universal

Today's defaults still ship with URL-redirect and HTTPS_PROXY paths as
co-equal alternatives. Phase H decommissions them from the default
install path and from the test matrix. The code stays in tree as
documented advanced/legacy hooks, but **no test, no default install,
no TUI button** activates them anymore.

## Target state

### `internal/config/defaults.go`

- `cfg.Transparent.SNIPeekMode = false` — opt-in via `enable`
- `cfg.Transparent.SNIPeekPort = 8443` — dev default; 443 via pfctl rdr
  or root
- **REMOVE** any references in defaults that hint at URL-redirect or
  HTTPS_PROXY as the primary Traffic-IN path.

### Default routing in `internal/proxy/`

- HTTP requests on `8990` (the existing CONNECT/HTTP listener) keep
  working for OpenAI-style direct API clients that want to use us as a
  plain HTTPS proxy. This is the **advanced/legacy** use case.
- Transparent SNI-MITM on `8443` (or 443 via pfctl) is the **default**
  path that `slimference install + enable` arms.

### Tests

- **REMOVE** test fixtures that exercise the URL-redirect path as the
  primary entry. Codex CLI / Desktop App integration tests now go
  through `transparent.Engine` end-to-end via the SNI peek path.
- **KEEP** unit tests for the URL-redirect code paths (provider
  selection, body rewrite, etc.) — they assert the legacy code still
  works for advanced users who configure it manually.
- **ADD** integration tests that simulate Codex CLI traffic flow:
  Codex client TLS-dials port 8443 with SNI `chatgpt.com` (per hosts
  patch), engine peeks SNI, sniroute routes, dispatcher bridges (or
  in Phase C2: mutates). One golden test per app:
  - `codex_cli_conversation_end_to_end_test.go`
  - `codex_desktop_responses_end_to_end_test.go`
  - `claude_code_messages_end_to_end_test.go`

### TUI

- TUI Setup-Wizard / Apps view does NOT offer "configure
  openai_base_url" or "set HTTPS_PROXY". These options are removed
  from any visible UI.
- TUI's `EnableTransparent` adapter becomes `Enable` and calls into
  `internal/install.Enable()` which writes config + SIGHUPs daemon.

### `agents.md` updates

- §1 "Normative Dokumente" gains `docs/install.md` as the install SSOT.
- A new §9 documenting the 2-surface architecture in code-discipline
  language:
  ```
  ## 9. Verdrahtungs-Doktrin (2-Surface)
  - Signal IN: ausschließlich Hooks (~/.codex/config.toml, ~/.claude.json).
  - Traffic IN: ausschließlich Transparent SNI-MITM (/etc/hosts + CA + port 8443/443).
  - URL-Redirect (openai_base_url) und HTTPS_PROXY sind Legacy/Advanced.
    Kein Default-Install setzt sie. Keine TUI bietet sie an. Tests
    exercieren sie nur als isolierte Unit-Tests, nicht als
    Integration-Pfad.
  - Drift-Verbot: PRs, die Default-Install um eine 3. Surface erweitern,
    sind reviewable nur mit explizitem Phase-H-Override-Tag.
  ```

## Implementation plan

1. **Audit existing tests** for URL-redirect / HTTPS_PROXY as
   integration entry points. Catalog into a markdown table; convert
   each to either:
   - delete (redundant given new transparent tests), OR
   - mark as `_legacy_` and document in `agents.md`.
2. **Write the 3 golden integration tests** listed above. Each test:
   - Builds a fake TLS server impersonating chatgpt.com / api.openai.com
     / api.anthropic.com with a generated cert.
   - Configures a real `transparent.Engine` with a Resolver pointed at
     the fake server (via dispatcher UpstreamDial injection).
   - Sends a realistic request body (captured live trace from real
     Codex / Claude Code).
   - Asserts:
     - Engine accepted handshake
     - sniroute decision matches expected (MITMConversation /
       PassthroughTLS)
     - For MITM: Phase F mutations were applied (post-C2)
     - For Passthrough: bytes are byte-equal end-to-end
     - Counters tick correctly in `/admin/state`
3. **TUI cleanup** (folded into T197).
4. **agents.md update**.
5. **Defaults audit**: ensure no default config field hints at
   URL-redirect being the primary path. Add code comments at each
   legacy field saying "// Legacy: not auto-armed by `slimference install`."

## Failure semantics

| Scenario | Behavior |
|---|---|
| User manually sets `OPENAI_API_BASE` after Phase H install | Still works (legacy code path). `slimference status` flags it as "legacy URL-redirect detected — supersedes transparent mode for this app". Not auto-removed. |
| Default `transparent.enabled = true` legacy field on disk | Honored for backward compat (existing CONNECT-MITM path still runs). New users won't see this default. |
| User runs `slimference install` on a system with `HTTPS_PROXY=http://...` already set | We don't unset it. We document in `slimference status` that an external proxy is detected. Transparent layer works on top of it (hosts patch supersedes the env var for chatgpt.com hostnames). |

## Acceptance

- `internal/config/defaults.go` diff shows the 2-surface bias: SNI-peek
  is the default-ready Traffic-IN; legacy fields stay but are
  commented as "Legacy".
- Three golden integration tests pass:
  `codex_cli_conversation_end_to_end_test.go`,
  `codex_desktop_responses_end_to_end_test.go`,
  `claude_code_messages_end_to_end_test.go`. Each exercises the FULL
  pipeline: TLS handshake on 8443 → SNI peek → sniroute → dispatcher
  → (post-C2: Phase F) → upstream.
- `slimference install + enable + run-codex` produces a successful
  conversation under transparent MITM in a fresh test env.
- No test in the repository exercises URL-redirect or HTTPS_PROXY as
  the primary integration path. Audit verified by grep + manual
  review.
- `agents.md` §9 exists and is one section.
- `docs/install.md` (T203) is consistent with this doctrine — no
  mention of OPENAI_API_BASE / HTTPS_PROXY in the install flow.

## Sub-Tasks

- [ ] Audit existing integration tests; catalog legacy-pathed ones.
- [ ] Write `codex_cli_conversation_end_to_end_test.go`.
- [ ] Write `codex_desktop_responses_end_to_end_test.go`.
- [ ] Write `claude_code_messages_end_to_end_test.go`.
- [ ] Capture realistic golden bodies (live trace or recorded
      fixtures) for each test.
- [ ] Comment legacy fields in `defaults.go` as "Legacy".
- [ ] `agents.md` §9 "Verdrahtungs-Doktrin (2-Surface)".
- [ ] Cross-check no test relies on `OPENAI_API_BASE` as integration
      entry.

## Notes

- **Why Codex Desktop tests separately from Codex CLI**: they use
  different request paths (`/backend-api/codex/responses` for Desktop
  realtime vs same path for CLI 0.130 WSS), and different ALPNs
  (`h2` for Desktop, `http/1.1` upgrade-to-WSS for CLI). sniroute must
  distinguish them by UserAgent and Subprotocol (T194 already does).
- **Golden body source**: ideally captured via the T198 tshark probe
  once it's live. Until then, hand-crafted fixtures matching the
  schema in `internal/proxy/wsmitm/frames.go`.

## Deviations

- The user explicitly forbade an intermediate test stage. Originally
  Phase H planned a "demote legacy" step. Per the 2026-05-16
  guidance, the test matrix flips ALL integration coverage to the
  2-surface model in one sweep. Legacy paths remain in code only for
  manually-configured advanced setups.
