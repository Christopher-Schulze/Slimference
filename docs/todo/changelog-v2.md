# changelog.md - v2.0.0 Entry

**Status:** done  
**Priority:** medium  
**File:** `docs/changelog.md`

## Task

Write the v2.0.0 changelog entry covering all work done in the v2.0.0-draft milestone.

## Scope of v2.0.0

### Major Features
- Layer 0 pre-entry filtering (24 built-in filters, TOML DSL, 200+ TryCompact* functions)
- Hook system for Claude Code + Codex CLI (install, verify, remove)
- SQLite tracking for filter savings (`modernc.org/sqlite`)
- Tee recovery system
- Permission model (deny/ask/exclude)
- All 14 Layer 1 sub-layers (L1.1-L1.14) including:
  - ANSI strip, tool classifier/compressor, success short-circuit
  - Image base64 replacement, repeated tool collapse, graph pruning
  - Pre-filtered content tagging, MinHash/LSH near-dedup (k=128, Jaccard 0.85)
  - Structure extraction for 10 languages (regex-based, replacing tree-sitter)
  - Delta encoding (LCS unified diff)
- Layer 2: Adaptive sliding window, tool result priority classification
- Layer 3: True LRU response cache (fixed from FIFO), fsnotify file watcher with interface abstraction
- Layer 3: recoverMiddleware (panic recovery + passthrough), rate limit retry (429), context overflow retry
- Multi-provider: Anthropic + OpenAI, OAuth passthrough, format normalization
- Secret detection: 12+ patterns, redact/warn/block modes
- BubbleTea TUI: 3 views (main/stats/debug), keyboard controls, provider/layer toggles
- Analytics: JSONL persistence, `slimference gain` command
- Debug system: decision chain JSONL, session replay
- Config: full TOML + env + CLI override system
- CLI: all subcommands (filter, hook, rewrite, gain, debug, doctor, stats, test, version)

### Bug Fixes
- Response cache promoted from FIFO to true LRU (Get + Set promote key to MRU)
- recoverMiddleware: panic recovery attempts passthrough when original body is available

### Breaking Changes from v1.x
- Config renamed: `tree_sitter_*` -> `structure_*`
- Section numbers in spec+.md updated (§4->§5, §5->§6, etc.)

## Completion Criteria
- [x] v2.0.0 entry written in docs/changelog.md following existing format
- [x] Entry covers all major features listed above
- [x] File edited surgically (not rewritten)
