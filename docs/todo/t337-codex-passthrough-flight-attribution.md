# T337 Codex Passthrough Flight Attribution

## Why

Codex HTTP traffic can be route-valid but compression-ineligible: Desktop sideband endpoints, unknown shapes, empty response payloads, provider-disabled traffic, route bypasses, and tool bypasses must pass through byte-equal. Before this task, those successful passthrough flights could be invisible in Activity/Savings, so a routed Codex session could look untracked or collapse into historical anonymous `no-session:proxy` accounting.

## Acceptance

- Codex passthrough requests record a content-free `RequestSummary` after successful upstream handoff.
- Strong Codex HTTP thread metadata is still used when present, producing `codex-http:<thread>` session IDs instead of anonymous proxy buckets.
- Client family is resolved from Codex metadata or User-Agent fallback.
- Passthrough summaries record zero saved tokens, no applied layers, and a precise bypass reason.
- Generic OpenAI passthrough remains unlogged to avoid noisy accounting.
- No request body mutation, model-facing insertion, prompt change, or compression behavior change.
- Tests cover non-compressible Codex sideband passthrough and empty Codex responses passthrough.

## Notes

- Product impact: better attribution and debug truth only. This task intentionally does not create savings.
- Drawdown risk: none in the model path; payloads are forwarded byte-equal and only content-free metadata is recorded after upstream handoff.
- Savings report enrichment now deduplicates and sorts Codex thread metadata lookup IDs so parallel CLI/Desktop sessions resolve deterministically.

## Verification

- `go test ./internal/proxy -run 'TestServeHTTP_CodexUnknownShapePassthrough|TestServeHTTP_CodexEmptyResponsesPassthrough|TestServeHTTP_GenericOpenAIResponsesPassthrough|TestHandlePassthrough' -count=1`
- `go test ./cmd/slimference -run 'TestSavingsSessionsKeepParallelCodexThreadsSeparate|TestSavingsSessionEnrichmentFromCodexHTTPThread' -count=1`
- `go test ./internal/proxy ./cmd/slimference ./internal/analytics -count=1`
- `go run ./scripts/ci`
