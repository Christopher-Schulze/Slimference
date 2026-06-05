# Slimference - Master TODO

Normative spec: `spec+.md`
Install SSOT: `docs/install.md`
Architecture docs: `docs/documentation.md`, `docs/map.md`

The old semantic context-replacement path is removed from the product. Old task
detail files may remain as historical records, but they do not define current
work and must not be used to reintroduce MiniMax, summarization, OCRL, or
context-ledger insertion. Current Layer 2 is the response/provider cache layer.

## Active

No active task.

## Queue

No queued tasks.

## Blocked

- [!] Live production savings claims beyond checked-in corpus evidence require
  fresh Codex CLI/Desktop captures and resource bundles. This is proof work, not
  a product drawdown.

## Done

- [x] T284 Response cache layer renumbering -> docs/todo/t284-response-cache-layer-renumbering.md
- [x] T260 Layer 0 parser frontier max-out -> docs/todo/t260-layer0-parser-frontier-maxx.md
- [x] T261 Layer 1 reversible safety tiers -> docs/todo/t261-layer1-reversible-safety-tiers.md
- [x] T263 Layer 2 provider/cache max-out -> docs/todo/t263-layer2-provider-cache-maxx.md
- [x] T264 Codex read/ranged/repeated-output cache-hit maximizer -> docs/todo/t264-codex-read-cache-hit-maximizer.md
- [x] T265 Repo-safe search keying max-out -> docs/todo/t265-repo-safe-search-keying-maxx.md
- [x] T266 Chunk dedup guarded auto hardening -> docs/todo/t266-chunk-dedup-always-auto-hardening.md
- [x] T267 Output-reduce quality governor -> docs/todo/t267-output-reduce-quality-governor.md
- [x] T268 Tool-schema pruning recovery max-out -> docs/todo/t268-tool-schema-pruning-recovery-maxx.md
- [x] T271 Product TUI and live-corpus proof -> docs/todo/t271-product-tui-and-live-corpus-proof.md
- [x] T272 Host resource budget max-out -> docs/todo/t272-host-resource-budget-maxx.md
- [x] T279 Remove retired Layer 2 from code and docs -> docs/todo/t279-remove-retired-layer2.md
- [x] T280 Final post-removal proof refresh -> docs/todo/t280-final-post-removal-proof-refresh.md
- [x] T281 RTK Codex maxx parity and safe deltas -> docs/todo/t281-rtk-codex-maxx-parity.md
- [x] T282 Search output-shape hardening -> docs/todo/t282-search-output-shape-hardening.md
- [x] T283 RTK Codex audit closure -> docs/todo/t283-rtk-codex-audit-closure.md

## Current Product Rules

- Active product layers: Layer 0, Layer 1, Layer 2, Layer 4.
- Removed product path: semantic context replacement.
- No product path may summarize old model context or replace it with capsules.
- Default-safe savings must be deterministic, recoverable, fail-open, or
  live-proof gated.
- `go run ./scripts/ci` is the final local truth gate.
