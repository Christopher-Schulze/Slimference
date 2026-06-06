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

No blocked tasks.

## Done

- [x] T303 Desktop scoped app status signal -> docs/todo/t303-desktop-scoped-app-status.md
- [x] T302 TUI live log window -> docs/todo/t302-tui-live-log-window.md
- [x] T301 TUI status and logs separation -> docs/todo/t301-tui-status-logs-separation.md
- [x] T300 Simple TUI home menu -> docs/todo/t300-simple-tui-home-menu.md
- [x] T299 Strict TUI tab separation -> docs/todo/t299-strict-tui-tab-separation.md
- [x] T298 TUI launch view declutter -> docs/todo/t298-tui-launch-view-declutter.md
- [x] T297 Lean diagnostics bundle for field sessions -> docs/todo/t297-lean-diagnostics-bundle.md
- [x] T296 Release proof refresh and docs alignment -> docs/todo/t296-release-proof-refresh-and-docs-alignment.md
- [x] T295 TUI mass-market wording cleanup -> docs/todo/t295-tui-mass-market-wording-cleanup.md
- [x] T294 Mass-market scoped launch polish -> docs/todo/t294-mass-market-scoped-launch-polish.md
- [x] T293 Conversation savings layer breakdown -> docs/todo/t293-conversation-savings-layer-breakdown.md
- [x] T292 Advanced shared Codex route wording -> docs/todo/t292-advanced-shared-codex-route-wording.md
- [x] T291 Mass-market TUI UX simplification -> docs/todo/t291-mass-market-tui-ux-simplification.md
- [x] T290 Documentation anchor drift gate -> docs/todo/t290-documentation-anchor-drift-gate.md
- [x] T289 RTK safe extra tool breadth -> docs/todo/t289-rtk-safe-extra-tool-breadth.md
- [x] T288 RTK breadth and Layer 3 renumbering -> docs/todo/t288-rtk-breadth-and-layer3-renumbering.md
- [x] T287 Prevent persistent Codex route tests -> docs/todo/t287-prevent-persistent-codex-route-tests.md
- [x] T286 Always-on safe product readiness rule -> docs/todo/t286-always-on-safe-product-readiness.md
- [x] T285 OpenAI cache steering max-out -> docs/todo/t285-openai-cache-steering-maxx.md
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

- Active product layers: Layer 0, Layer 1, Layer 2, Layer 3.
- Removed product path: semantic context replacement.
- No product path may summarize old model context or replace it with capsules.
- Default-safe savings must be deterministic, recoverable, fail-open, or
  live-proof gated.
- New product mechanisms must be default-on-safe or automatically safe-enabled;
  do not add new permanent manual experiment toggles without explicit project
  override.
- `docs/todo.md` is the active task queue. Unlisted detail files under
  `docs/todo/` are historical records unless this file links them from Active,
  Queue, or Blocked.
- `go run ./scripts/ci` is the final local truth gate.
