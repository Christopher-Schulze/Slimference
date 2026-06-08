# T336 Codex HTTP Transport Attribution

## Why

T334 made Codex HTTP traffic resolvable by strong thread metadata, but the shared extractor still labeled HTTP-derived thread IDs with the WSS prefix. Savings remained mergeable through normalized Codex thread IDs, but raw flight rows were not transport-precise.

## Acceptance

- HTTP Codex traffic with strong thread metadata is recorded as `codex-http:<thread>`.
- WSS Codex traffic continues to use `codex-wss:<thread>` and keeps its `prompt_cache_key` fallback only when stronger metadata is absent.
- Codex HTTP client family is extracted from `metadata`, `client_metadata`, nested `x-codex-turn-metadata`, or User-Agent fallback.
- Proxy flight summaries include direct `client_family` for Codex HTTP local-cache and upstream rows.
- Savings enrichment still normalizes both `codex-http:` and `codex-wss:` IDs to the same Codex thread metadata store.
- No prompt/body mutation is introduced; this is accounting-only.

## Notes

- Product drawdown: none. The change only improves labels and report attribution.
- Historical logs retain their historical prefixes.

## Verification

- `go test ./internal/proxy -run 'TestExtractSessionIDCodexHTTPUses|TestExtractClientFamilyCodexHTTPFallbacks|TestServeHTTP_CodexResponsesCompressionAndHeaders|TestWsCodexSessionID|TestWSSRequestMeta' -count=1`
