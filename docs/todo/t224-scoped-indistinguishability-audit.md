# TASK 224: Scoped indistinguishability audit

Status: PREPARED PRE-LIVE
Priority: P0 before any "stealth" claim
Scope: Evidence harness for scoped Codex HTTP and WSS modes

## Why

No "invisible" claim is valid without capture evidence. T198 can parse and diff
traffic captures, but the product target changed after T220: the audit must now
compare native Codex CLI against scoped Slimference Codex routes, not global
hosts/pfctl mode.

This task creates the proof loop: baseline native Codex -> scoped HTTP -> scoped
WSS/raw -> diff TLS, HTTP, WSS, timing, and mutation counters.

T224 is the promotion gate. Until it passes, docs must say "scoped WSS power
mode" or "minimized drift", not "invisible". After it passes, `transport=auto`
may prefer WSS for Codex CLI.

## Acceptance

- A repeatable operator runbook captures:
  - Native Codex CLI direct baseline.
  - `slimference codex run` HTTP scoped mode.
  - `slimference codex run --transport=wss` scoped WSS mode after T221.
  - `slimference codex run --transport=auto` after T222/T223, proving auto
    selects WSS only when preflight says it is safe.
  - Optional `slimference codex enable` Desktop route after T225.
- The audit records:
  - Destination host/IP and SNI.
  - JA3/JA4 or equivalent TLS features.
  - ALPN and HTTP version.
  - HTTP method/path and Upgrade headers.
  - WebSocket subprotocol and extension list.
  - Header order/casing where observable.
  - Timing envelope and connection reuse.
  - Slimference mutation counters and degradation counters.
- The report distinguishes three outcomes:
  - `match`: no material drift in the captured dimension.
  - `explained`: drift is expected and documented, e.g. body shorter because
    savings happened.
  - `fail`: unexplained provider-visible drift.
- The docs forbid "undetectable" language unless every required dimension is
  `match` or `explained` with an accepted rationale.
- The report decides whether `auto` may be promoted from HTTP-first to
  WSS-first for Codex CLI.

## Sub-Tasks

- [x] Adapt T198 scripts/docs for scoped Codex capture modes.
- [x] Define golden capture naming under ignored `research/indist/` or another
  approved non-committed evidence location.
- [x] Add report templates for baseline/scoped diff.
- [x] Add a local smoke that proves the audit tool can parse a synthetic WSS
  capture without tshark installed.
- [x] Add operator checklist for real tshark captures from an external terminal.
- [x] Add a transport-promotion section: exact evidence required before WSS is
  default-preferred.
- [x] Update `docs/install.md` with honest claim language: "scoped and
  minimized drift", not "undetectable".
- [ ] Run the real tshark native/scoped HTTP/scoped WSS captures during T209.
- [ ] Attach the live diff result and promotion decision.

## Notes

Benefit:

- Converts architecture belief into measurable proof.
- Prevents future agents from overstating invisibility.
- Gives concrete engineering targets to T221/T222/T223.
- Prevents WSS from becoming default on vibes alone.

Known limit:

- If request bodies are compressed, body diff is expected. The audit must judge
  body changes semantically and by safety policy, not byte equality.

Pre-live runbook:

1. Keep global lab mode off: no `lab cert-trust`, no
   `lab root-arm --global-chatgpt-hosts`, no `lab enable`.
2. Capture native Codex direct baseline:
   `go run ./scripts/utils/indist_probe capture --label codex-native-direct --out research/indist/codex-native-direct.json --iface en0 --host chatgpt.com --port 443`
3. Trigger one small native Codex CLI prompt while capture is listening.
4. Capture scoped HTTP:
   `go run ./scripts/utils/indist_probe capture --label slimference-scoped-http --out research/indist/slimference-scoped-http.json --iface en0 --host chatgpt.com --port 443`
5. Trigger `slimference codex run --transport=http -- "say hi"` from an
   external terminal.
6. Capture scoped raw WSS:
   `go run ./scripts/utils/indist_probe capture --label slimference-scoped-wss --out research/indist/slimference-scoped-wss.json --iface en0 --host chatgpt.com --port 443`
7. Trigger `slimference codex run --transport=wss -- "say hi"` from an
   external terminal.
8. Diff:
   `go run ./scripts/utils/indist_probe diff research/indist/codex-native-direct.json research/indist/slimference-scoped-wss.json`
9. Record `/admin/state.wss`: `frames_reencoded`, `frames_forwarded`,
   `degraded_sessions`, `parse_failures`, `bytes_c2s`, `bytes_s2c`.

Promotion criteria:

- WSS may become the `transport=auto` preferred path only when scoped raw WSS
  succeeds with `frames_reencoded>0`, `degraded_sessions=0`,
  `parse_failures=0`, no unexplained TLS/ALPN/WSS drift, and no Browser
  ChatGPT/ChatGPT.app traffic entering Slimference.
- Explained body drift is allowed only when it is the intended Phase-F savings
  mutation.
- Any unexplained provider-visible drift keeps `auto` on HTTP.

Verification:

- Synthetic parser smoke is covered by
  `TestParseTSharkJSONSyntheticWSSCapture`.
- `research/indist/` is intentionally ignored; live capture artifacts stay out
  of git unless the operator explicitly requests a scrubbed evidence fixture.
