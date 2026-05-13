# TASK 139: TLS fingerprint provider-edge proof and profile maintenance

Status: DONE (local implementation 2026-05-13; live reflected provider-edge run remains operator/manual)
Priority: P1
Scope: `internal/tlsdial/`, `scripts/utils/tls_probe.go`, `internal/proxy/`, `cmd/slimference/doctor`, `cmd/slimference/proxy_cmd.go`, `docs/transparent-mode.md`, `docs/todo/t123-tls-fingerprint-mimicry.md`.

## Why

T123 reduced the obvious Go stdlib TLS fingerprint by using uTLS profiles in transparent mode. That is useful. It is not proof of being indistinguishable from Codex Desktop, Chromium, Node, Python requests, or any provider-edge view. We need provider-edge evidence, profile freshness, and honest reporting.

The goal is not a childish "undetectable" claim. The goal is: Slimference's upstream TLS and HTTP transport shape should be as close as practical to the client family it represents, and the operator should know when the profile is stale or unproven.

## Target State

1. Local loopback TLS proof remains.
2. External reflected JA3/JA4/ALPN/HTTP2 proof exists for each profile where a safe reflector is available.
3. Profile catalogue has review dates and automated staleness warnings.
4. `doctor` reports profile catalogue age, proof status, and last reflected hash. `proxy status` already reports active runtime profile mapping.
5. Codex App routes prefer `chromium_stable`; API SDK/CLI style routes can use `node_stable` only when an actual profile exists or alias is clearly labelled.
6. No docs claim provider invisibility beyond evidence.

## Work Packages

### WP1 - Proof model

- [x] Add `TLSProof` record:
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
- [x] Store under `~/.slimference/tls-proofs/`.

### WP2 - Reflector support

- [x] Extend `scripts/utils tls-probe`:
  - local loopback mode.
  - external reflector mode.
  - JSON output.
  - save mode.
  - compare mode.
- [x] Reflector is configurable and off by default.
- [x] No OpenAI/Anthropic credentials are sent to reflectors; the probe opens a direct HTTPS request to the configured reflector only.

### WP3 - HTTP2 shape

- [x] TLS ClientHello is only one layer. The reflected probe records:
  - ALPN negotiated protocol.
  - JA3 / JA3 hash when reflector returns it.
  - JA4 when reflector returns it.
  - HTTP version / H2 settings hash when reflector returns it.
- [x] If ALPN negotiates `h2`, the probe marks the proof unproven unless a matching H2 probe stack exists. This avoids a fake HTTP/1.1 proof over an H2-negotiated connection.

### WP4 - Profile catalogue

- [x] Split aliases from concrete profiles:
  - `chromium_stable` concrete.
  - `node_stable` must either become a real Node-like profile or remain labelled alias-to-chromium.
  - `python_requests` same.
- [x] Existing staleness policy is surfaced through `doctor`; strict-mode failure is not implemented because there is no `doctor --strict` flag today. This remains intentionally informational, not startup-blocking.
- [x] Alias targets are visible through catalogue metadata and probe output.

### WP5 - Runtime selection

- [x] Per-host/per-route selection:
  - `chatgpt.com` / Codex App: chromium profile.
  - `api.openai.com` via Codex CLI transparent mode: choose profile from observed client family if known.
  - unknown: conservative default.
- [x] Selected TLS profile logging is deliberately kept out of the hot-path flight schema for this task. The runtime resolver has the data, but adding per-request TLS profile fields without live T140 demand would widen the schema for low operational value.

### WP6 - Tests

- [x] Unit tests for proof JSON.
- [x] Local probe tests remain deterministic.
- [x] External tests are manual/opt-in.
- [x] Doctor/profile-age/proof-status tests.

## Acceptance

- [x] Local proof and external reflected proof commands exist.
- [x] `doctor` reports proof status and profile age.
- [x] Alias profiles are not misrepresented as exact Node/Python fingerprints.
- [x] HTTP2/ALPN limitations are measured or explicitly marked unproven.
- [x] `go run ./scripts/ci` passes.
- [x] Docs avoid "undetectable" and state exact evidence level.

## Notes

- This extends T123. It does not block functional transparent mode.
- Implemented local/non-Codex scope: `internal/tlsproof`, extended `scripts/utils tls-probe`, alias metadata, doctor proof status, docs.
- Not claimed: actual OpenAI/Anthropic edge parity. That still needs an operator-selected reflector or live provider-edge evidence.
- Verification: `go run ./scripts/ci` passed 8/8 on 2026-05-13 with 100.0% statement coverage.
