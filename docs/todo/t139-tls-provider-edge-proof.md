# TASK 139: TLS fingerprint provider-edge proof and profile maintenance

Status: PENDING (opened 2026-05-13)
Priority: P1
Scope: `internal/tlsdial/`, `scripts/utils/tls_probe.go`, `internal/proxy/`, `cmd/slimference/doctor`, `cmd/slimference/proxy_cmd.go`, `docs/transparent-mode.md`, `docs/todo/t123-tls-fingerprint-mimicry.md`.

## Why

T123 reduced the obvious Go stdlib TLS fingerprint by using uTLS profiles in transparent mode. That is useful. It is not proof of being indistinguishable from Codex Desktop, Chromium, Node, Python requests, or any provider-edge view. We need provider-edge evidence, profile freshness, and honest reporting.

The goal is not a childish "undetectable" claim. The goal is: Slimference's upstream TLS and HTTP transport shape should be as close as practical to the client family it represents, and the operator should know when the profile is stale or unproven.

## Target State

1. Local loopback TLS proof remains.
2. External reflected JA3/JA4/ALPN/HTTP2 proof exists for each profile where a safe reflector is available.
3. Profile catalogue has review dates and automated staleness warnings.
4. `proxy status` / `doctor` reports profile, age, proof status, and last reflected hash.
5. Codex App routes prefer `chromium_stable`; API SDK/CLI style routes can use `node_stable` only when an actual profile exists or alias is clearly labelled.
6. No docs claim provider invisibility beyond evidence.

## Work Packages

### WP1 - Proof model

- Add `TLSProof` record:
  - profile
  - host
  - transport
  - ja3
  - ja4
  - alpn
  - h2 settings hash if available
  - timestamp
  - reflector
  - success/failure
  - notes
- Store under `~/.slimference/tls-proofs/`.

### WP2 - Reflector support

- Extend `scripts/utils tls-probe`:
  - local loopback mode.
  - external reflector mode.
  - JSON output.
  - compare mode.
- Reflector must be configurable and off by default.
- Never send OpenAI/Anthropic credentials to reflectors.

### WP3 - HTTP2 shape

- TLS ClientHello is only one layer. Add optional probes for:
  - ALPN negotiated protocol.
  - HTTP/2 SETTINGS order/values.
  - pseudo-header order where observable.
  - connection reuse behavior.
- If Go `http.Transport` creates a distinguishable H2 profile even with uTLS, report it.

### WP4 - Profile catalogue

- Split aliases from concrete profiles:
  - `chromium_stable` concrete.
  - `node_stable` must either become a real Node-like profile or remain labelled alias-to-chromium.
  - `python_requests` same.
- Add staleness policy:
  - warn after 90 days.
  - fail `doctor --strict` after 180 days unless acknowledged.
- Add profile update checklist.

### WP5 - Runtime selection

- Per-host/per-route selection:
  - `chatgpt.com` / Codex App: chromium profile.
  - `api.openai.com` via Codex CLI transparent mode: choose profile from observed client family if known.
  - unknown: conservative default.
- Log selected profile per request in T134 flight recorder.

### WP6 - Tests

- Unit tests for proof JSON.
- Local probe tests remain deterministic.
- External tests are manual/opt-in.
- Doctor/profile-age tests.

## Acceptance

- [ ] Local proof and external reflected proof commands exist.
- [ ] `doctor` reports proof status and profile age.
- [ ] Alias profiles are not misrepresented as exact Node/Python fingerprints.
- [ ] HTTP2/ALPN limitations are measured or explicitly marked unproven.
- [ ] `go run ./scripts/ci` passes.
- [ ] Docs avoid "undetectable" and state exact evidence level.

## Notes

- This extends T123. It does not block functional transparent mode.
