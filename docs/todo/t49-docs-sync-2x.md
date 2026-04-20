# T49 - `docs/documentation.md` + `docs/map.md` Sync auf 2.x

Status: todo
Priority: P1
Scope: `docs/documentation.md`, `docs/map.md`, `docs/context.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

`docs/documentation.md` is a v1.3.5 snapshot (2026-04-13). Since then:

- T37 (Read-hook cache + delta) shipped
- T38 (Structure-aware tool-result preview) shipped
- T39 (Smart compaction / checkpoints) shipped
- T40 (Tool-result archive + `expand`) shipped
- Daemon console polish and optimisation hardening landed
- `readcache`, `toolarchive`, `checkpoints` packages were added
- `[compression.tuning]` central config was introduced (T22)
- Prompt-cache metrics (T23) live in analytics snapshot

`docs/map.md` still lists packages as of pre-T37; it is missing
`internal/readcache`, `internal/toolarchive`, `internal/checkpoints`, and
the admin-surface expansion in `internal/proxy/admin.go`.

`docs/context.md` is partial but has no references to the recent features.

Result: an agent or new contributor reading the docs builds a wrong mental
model. Per `~/.claude/CLAUDE.md` doc-disziplin this must not stand.

## Current State

- `docs/documentation.md`: ~11 sections, v1.3.5, missing T37-T40 and
  tuning refactor.
- `docs/map.md`: stale package graph.
- `docs/context.md`: brief, missing pointers to the above.
- `docs/changelog.md`: current (no action needed here).

## Target State

- `docs/documentation.md` at version `2.0.x`, full coverage of:
  - L1 sub-layers including Structure-Preview (T38) and
    Read-hook-delta (T37)
  - L2 staircase modes (T36)
  - L3 response cache (unchanged)
  - Checkpoints subsystem (T39) with lifecycle diagram
  - Tool-archive subsystem (T40) with `expand` flow
  - Prompt-cache metrics (T23) and future multi-breakpoint (T45)
  - `[compression.tuning]` config reference (T22)
  - Extraction error semantics (T41)
  - Analytics queue telemetry (T42)
  - Headless mode (T44)
  - `--config` flag and XDG resolution (T46)
- `docs/map.md`: complete package graph with arrows (depends-on) and
  role tags (hot-path | admin | ops | tests).
- `docs/context.md`: 20-line executive pointer sheet covering all
  post-2.0 additions.

## Design

### documentation.md structure

```
# Slimference Documentation (v2.0.x)

## 1. Overview
## 2. Architecture
## 3. Request Lifecycle (hot path, <5 ms budget)
## 4. Layer 0 - Pre-Entry Filter
## 5. Layer 1 - Deterministic Compression
     5.1 ANSI + JSON Minify
     5.2 Comment Strip
     5.3 Exact Dedup + MinHash/LSH
     5.4 Structure Extraction
     5.5 Delta Encoding
     5.6 Structure-Aware Preview (T38)
     5.7 Read-Hook Cache + Delta (T37)
     5.8 Tool-Archive Reference (T40)
     5.9 Prompt-Cache Breakpoints (T23, T45)
## 6. Layer 2 - MiniMax Summarization
     6.1 Operating Modes (strict | balanced | fast, T36)
     6.2 Adaptive Window + Incremental Staircase (T27)
     6.3 Tool-Priority Staircase (T26)
## 7. Layer 3 - Response Cache
## 8. Analytics + Observability
     8.1 Snapshot fields
     8.2 Queue telemetry (T42)
     8.3 Prompt-cache metrics (T23)
## 9. Checkpoints (T39)
## 10. Extraction error semantics (T41)
## 11. Config Reference
     11.1 File precedence (flag > env > XDG > legacy)
     11.2 `[compression.tuning]`
     11.3 `[proxy.extraction]`
     11.4 `[compression.prompt_cache]`
     11.5 ENV overrides
## 12. Operability
     12.1 `slimference doctor`
     12.2 Headless mode (T44)
     12.3 macOS launchd
     12.4 Linux systemd (T48)
     12.5 Homebrew + tar.gz (T47)
## 13. Security
## 14. Tuning Guide
## 15. Troubleshooting
```

### map.md structure

```
# Package Map (v2.0.x)

## Hot path
internal/proxy         - handler, streaming, routing
internal/compression   - L1 pipeline, 14 sub-layers
internal/summarization - L2 MiniMax client + cache
internal/caching       - L3 response LRU

## Feature
internal/readcache     - Read-hook delta cache  [T37]
internal/toolarchive   - Large tool-result archive  [T40]
internal/checkpoints   - Smart compaction checkpoints  [T39]
internal/filter        - L0 command filter
internal/hooks         - Claude + Codex hook install

## Ops
internal/daemon        - launchd (macOS) + systemd (T48)
internal/admin         - /admin/* HTTP handlers
internal/analytics     - JSONL + snapshot
internal/debug         - Decision chain + replay
internal/tui           - BubbleTea UI

## Support
internal/config        - TOML + ENV + XDG resolution
internal/tokens        - Tiktoken tokeniser
internal/security      - Secret redaction
internal/slogutil      - JSON log handler + rotator
internal/resilience    - Retry + circuit breakers
internal/sessions      - Session bookkeeping
internal/buildinfo     - Version + commit (-ldflags)
internal/util          - Generic helpers
internal/types         - Shared types (DecisionEntry, etc.)
```

Add a Mermaid block showing the depends-on graph.

### context.md

Keep as 200-line executive overview. Section headings:

- What Slimference is
- Where code lives (map.md pointer)
- Where docs live (documentation.md pointer)
- Where tasks live (todo.md, todo/ folder)
- How to run it (doctor, --no-tui, service install)
- Known risks / open tasks pointers

## Implementation Plan

### WP1 - Content audit
- Read every TASK file T17-T64 + `docs/changelog.md` 2.0.0+.
- Extract canonical facts per subsystem.

### WP2 - documentation.md rewrite
- Section-by-section rewrite keeping format consistent.
- Cross-link to spec+.md anchors where relevant.

### WP3 - map.md rewrite
- Fresh package graph.
- Add Mermaid diagram block.

### WP4 - context.md refresh
- Executive pointer sheet.

### WP5 - Link-check
- Grep for dead anchors, broken file paths, stale section numbers.

### WP6 - Doc-lint CI gate
- Script under `scripts/ci/doc-lint.ts` runs:
  - Markdown-link-check on `docs/*.md`
  - Ensure every TASK `.md` under `docs/todo/` is referenced from
    `docs/todo.md`.
  - Ensure every internal package has a one-line entry in `docs/map.md`.

---

## Subtasks

- [ ] Content audit (T17-T64 + changelog).
- [ ] Rewrite `docs/documentation.md` on the new structure.
- [ ] Rewrite `docs/map.md` with current packages + Mermaid graph.
- [ ] Refresh `docs/context.md`.
- [ ] Cross-check spec+.md anchors.
- [ ] Add doc-lint CI gate.
- [ ] Verify every package reference is live.

## Risks

- Drift resumes immediately if doc-lint gate is not enforced. Ship gate
  together with the rewrite.

## Acceptance Criteria

- [ ] `docs/documentation.md` version header = 2.0.x, TOC matches current
      code.
- [ ] `docs/map.md` lists every `internal/*` package that exists today.
- [ ] `scripts/ci/doc-lint.ts` passes.
- [ ] No broken markdown links.

## Out of Scope

- English-German dual translation.
- HTML rendering / static site.

---

## Validation

```
bun run scripts/ci/doc-lint.ts
grep -c "^## " docs/documentation.md
```
