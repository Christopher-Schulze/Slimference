# TASK 109: Layer 2 outbound redaction before MiniMax

Status: DONE 2026-04-30 (redactor + Layer2 wire-in + admin telemetry + doctor warning)
Priority: P0
Scope: `internal/summarization/layer2.go`, `internal/summarization/minimax.go`, `internal/security/`, `internal/config/`
Driver: Layer 2 today serialises the full conversation prefix (assistant text, user text, tool inputs, tool results) and POSTs it to `api.minimax.io` (a third-party provider hosted in PRC). The existing `security.Detector` is only wired into the inbound proxy hot path; the L2 outbound path performs zero redaction. Any session that touched a secret-bearing file, an env-var dump, an auth header, or a private filesystem path leaks that content to the third-party endpoint by default. Without this fix, Layer 2 cannot ship default-on without a data-policy violation.

---

## Problem

`Layer2.RunCompressionJobContext` -> `renderRangeForSummarization` -> `chain.Summarize` -> `MiniMaxClient.Summarize` (`internal/summarization/minimax.go:820`) builds the request body straight from the message slice. The only preprocessing today is:

- `preprocessInput` (line dedup + 2000-char-per-message truncate)
- T89 chain-of-thought stripping (12 family fixed-point regex)

Neither of these touch:

- API keys, JWTs, OAuth tokens visible in tool_results / tool_inputs
- AWS / GCP / Azure credentials in env dumps
- Private absolute paths (`/Users/<name>/...`, `/home/<name>/...`, `C:\Users\<name>\...`)
- Internal hostnames / SSH config / database URLs
- Customer PII appearing in log output

`internal/security/secrets.go` already implements the relevant patterns and the Detector returns redacted content. It is only consulted at request ingress (`proxy/handler.go:120`) and never on the path that ships data outside the user's machine.

## Target State

A mandatory `redactOutbound(messages, mode) []Message` pass runs on the message slice **before** it is rendered for `chain.Summarize`. The pass:

1. Runs the configured `security.Detector` (or a stricter L2-specific detector) over every text/tool_input/tool_result block.
2. Replaces each detected secret in-place with a stable placeholder of the form `[REDACTED:<pattern_name>:<sha8>]` so the model still sees that "something secret was here" but cannot reconstruct it.
3. Normalises absolute filesystem paths to relative form (`/Users/foo/proj/...` -> `<HOME>/proj/...`, `/home/...` -> `<HOME>/...`, drive letters -> `<DRIVE>:`).
4. Strips the `Authorization`, `Cookie`, `X-Api-Key`, `Set-Cookie` headers from any captured HTTP response in tool output.
5. Records per-session redaction counts on `/admin/status.layer2.redaction.{secrets,paths,headers}_redacted`.

Mode tiers (`[compression.summary] outbound_redaction` config key):

- `off` - no redaction (NOT recommended; warning emitted by `slimference doctor`)
- `default` - secrets + path normalisation + auth headers (DEFAULT once T121 lands)
- `strict` - default + custom `[secrets].layer2_extra_patterns` from config + drop tool_input bodies entirely (only retain tool name)

Failures must fail closed: if the redaction pass errors out on a block, that block is **omitted** from the summary input rather than shipped raw.

## Implementation Plan

### WP1 - Detector wiring
- New `internal/summarization/redact.go` exposing `RedactForOutbound(detector *security.Detector, msgs []types.Message, opts RedactOptions) ([]types.Message, RedactStats)`.
- `RedactOptions{Mode string, ExtraPatterns []security.SecretPattern, HomeDir string, ReplaceAbsPaths bool, StripAuthHeaders bool}`.
- Iterates every block; redacts in-place on a deep copy. Original slice unmodified.

### WP2 - Path normalisation
- Detect `/Users/<seg>/`, `/home/<seg>/`, `[A-Z]:\\Users\\<seg>\\`, `/var/folders/...` (macOS tmp) -> placeholder mapping with stable shas so the model can correlate references across messages.
- Skip paths inside `<code>` fences? No - same rule everywhere: model only needs the relative shape.

### WP3 - Header / token strippers
- Detect HTTP-style header lines in tool_result text (`Authorization: Bearer ...`, `Cookie: ...`, `Set-Cookie: ...`, `X-Api-Key: ...`).
- Replace value with `[REDACTED]` while keeping header name.
- Same for JSON shapes containing common credential keys (`api_key`, `access_token`, `client_secret`, `password`, `auth_token`).

### WP4 - Layer2 integration
- `Layer2.RunCompressionJobContext` calls `RedactForOutbound` immediately before `renderRangeForSummarization` (and before `Layer2.ApplyMidExchange`'s `chain.Summarize`).
- New struct field `Layer2.redactor` constructed from cfg in `NewLayer2`.

### WP5 - Telemetry
- `RedactStats{SecretsRedacted, PathsNormalised, HeadersStripped, BlocksDropped int}` returned by every call.
- Sum into atomic counters on `Layer2`; surface on `/admin/status.layer2.redaction`.
- `slimference doctor` prints a warning when `outbound_redaction = off`.

### WP6 - Config + defaults
- `[compression.summary] outbound_redaction = "default"` in `config/defaults.go`.
- `[secrets] layer2_extra_patterns = []` for operator-supplied additional patterns.
- `[compression.summary] outbound_drop_tool_inputs = false` (true under `strict`).

### WP7 - Tests
- Per-pattern fixture matrix (every `secrets/patterns.go` family represented).
- Path-normalisation edge cases (trailing slash, mixed separators, deep nesting).
- Failure-closed behaviour: detector returns error -> block omitted, stats reflect drop.
- Negative test: redacted body fed to `chain.Summarize` (mocked) - assert the mock's input body contains zero of the original secret values.

### WP8 - Live verification
- Add `tests/fixtures/redaction_corpus/` with synthetic but realistic samples (NOT real secrets).
- `scripts/verify` adds a redaction smoke test: feeds corpus through `RedactForOutbound`, asserts zero secret patterns leak through.

## Acceptance Criteria

- [x] `Redactor.Redact` covers every `security.Detector` family + the six header types (Authorization, Cookie, Set-Cookie, X-Api-Key, X-Auth-Token, Proxy-Authorization) + path normalisation across macOS / Linux / Windows / macOS-tmp shapes.
- [x] `Layer2.RunCompressionJobContext` and `Layer2.ApplyMidExchange` call `applyOutboundRedaction` on every call path that leads to `chain.Summarize`.
- [x] `[compression.summary] outbound_redaction` defaults to `"default"`; `slimference doctor` warns under `"off"`, surfaces `strict`, and flags unknown modes.
- [x] Telemetry counters (`Secrets`, `Paths`, `Headers`, `JSONKeys`, `Inputs`) accumulate across calls; surfaced via `/admin/status.layer2.redaction` and `Layer2.RedactionCounters()`.
- [x] Coverage 100% in summarization + proxy; race tests green.
- [x] Fixture matrix in `redact_test.go` covers per-pattern leak guards (TestRedactor_DefaultMode_RedactsKnownSecretPatterns, TestRedactor_HeaderStripping, TestRedactor_JSONCredentialKeys, TestRedactor_PathNormalisation, TestRedactor_StrictMode_JSONSweepFiresAtDepth, TestRedactor_ChainedRedactions).
- [ ] **T109b** (deferred): standalone `tests/fixtures/redaction_corpus/` + `scripts/verify` smoke gate. The in-tree unit-fixture matrix already enforces leak-zero; the disk-corpus follow-up unblocks the T118 live-corpus sweep so they share one redaction harness.

## Out of Scope

- Detecting brand-new secret shapes invented after this task lands (pattern updates are evergreen work).
- Encrypting transport (TLS already covers in-flight; this is an upstream-content concern, not a transport concern).
- Removing MiniMax as the L2 provider (T121 addresses provider trust labelling).

## Validation

```
go test ./internal/summarization/... ./internal/security/...
go run ./scripts/verify
go test -race ./internal/summarization/...
```
