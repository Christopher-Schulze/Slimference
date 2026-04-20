# T45 - Multi-Breakpoint Prompt-Cache Injection

Status: todo
Priority: P1
Scope: `internal/compression/prompt_cache.go`, `internal/compression/layer1.go`, `internal/analytics/`, `internal/tui/`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

Anthropic's prompt-cache allows **up to 4 cache_control breakpoints** per
request (system prompt + 3 message-level). Slimference currently injects
exactly one breakpoint, placed at `CompressiblePrefixEnd`. That leaves
2-3 cache slots unused per request, which is the single biggest
unrealised savings lever documented in `docs/savings-assessment.md`.

Hit-rate impact estimate (based on `docs/benchmarks.md`):

| Breakpoints | Typical hit-rate | Token rebill reduction |
|-------------|------------------|------------------------|
| 1 (today)   | 45-60 %          | baseline               |
| 4 (target)  | 70-85 %          | +25-35 pp              |

This is the highest-ROI L1 improvement remaining.

## Current State

- `internal/compression/prompt_cache.go::InjectBreakpoints` walks messages
  and appends `cache_control: {type: "ephemeral"}` on the single tail of
  the compressible prefix.
- No per-message length heuristic, no system-prompt awareness, no tool
  definitions awareness.
- T23 already measures `usage.cache_creation_input_tokens` and
  `usage.cache_read_input_tokens`; infrastructure to A/B compare is in
  place.

## Target State

Breakpoint placement strategy (in priority order, up to 4):

1. **System prompt** - always cache if > `system_min_tokens` (default 256).
2. **Tools array** - cache if tools exist and combined length > 128 t.
3. **Early history boundary** - first stable turn boundary after system
   prompt, before the first user turn likely to vary.
4. **Late history boundary** - the existing `CompressiblePrefixEnd`.

Fallback: if fewer than 4 positions qualify, leave slots empty (Anthropic
accepts). Never inject on the last `keep_last_n` turns (already guarded).

## Design

### Config

New `[compression.prompt_cache]` section:

| Field | Type | Default | Semantic |
|-------|------|---------|----------|
| `max_breakpoints`      | int  | 4    | anthropic cap |
| `system_min_tokens`    | int  | 256  | min size to warrant cache slot |
| `tools_min_tokens`     | int  | 128  | min tools size |
| `early_boundary_after` | int  | 3    | turn index for early breakpoint |
| `enable`               | bool | true | master toggle |

### Placement algorithm

```
candidates := []{}

if system.len >= system_min_tokens: candidates += SYSTEM
if tools.len >= tools_min_tokens:   candidates += TOOLS
if turns >= early_boundary_after:   candidates += turn[early_boundary_after]
if compressiblePrefixEnd > 0:       candidates += COMPRESSIBLE_END

// Sort by position (stable), dedup adjacent, cap at max_breakpoints
```

### Stability rule

Breakpoints must be **content-stable** across requests to hit the cache.
For SYSTEM and TOOLS this is guaranteed; for early/late boundaries we must
ensure the byte-hash of the text up to the breakpoint is stable across
requests - otherwise cache miss. Add a unit test that runs the same prompt
twice and asserts breakpoint hashes match.

### Metrics

Extend analytics snapshot:

- `prompt_cache_breakpoints_injected_total`
- `prompt_cache_breakpoints_per_request_p50/p95`
- `prompt_cache_hit_tokens` (from `usage.cache_read_input_tokens`)
- `prompt_cache_create_tokens` (from `usage.cache_creation_input_tokens`)
- `prompt_cache_hit_rate` = read / (read + create)

TUI Stats: new row "Prompt-Cache Hit: 72.4% (4 breakpoints avg)".

## Implementation Plan

### WP1 - Candidate extraction
- Function `candidatePositions(req)` returning `[]Position`.

### WP2 - Placement + dedup
- Sort, dedup, cap at `max_breakpoints`.

### WP3 - Integration into layer1 pipeline
- Replace single-breakpoint injection with multi.

### WP4 - Stability test
- Fixture: identical request twice, assert identical breakpoint byte
  positions and identical hash-up-to-breakpoint.

### WP5 - Metrics plumb
- Wire usage fields through analytics collector.
- Dashboard render.

### WP6 - A/B benchmark
- Add `scripts/benchmarks/prompt-cache-ab.ts` (Bun/TS per repo convention)
  running N representative requests with `max_breakpoints=1` vs `=4`,
  reporting delta hit-rate and saved tokens.

---

## Subtasks

- [ ] `candidatePositions` implementation.
- [ ] System-prompt + tools-array detection.
- [ ] Early-boundary heuristic (turn index).
- [ ] Multi-breakpoint injection in Layer 1 pipeline.
- [ ] Config `[compression.prompt_cache]` + defaults + ENV overrides.
- [ ] Stability test with identical-request fixture.
- [ ] Analytics fields + TUI Stats row.
- [ ] A/B benchmark script under `scripts/benchmarks/`.
- [ ] Spec update: `spec+.md` §Prompt-Cache.
- [ ] `docs/documentation.md` §5 Prompt-Cache sub-layer.

## Risks

- Over-fragmentation: too many breakpoints on small prompts waste the
  feature. Thresholds above guard that.
- Cache-key instability if system prompt contains dynamic content (date,
  model version). Detect with stability test, document in spec.
- Anthropic-side quota/billing changes. Monitor via T63 and rollback via
  `max_breakpoints=1` toggle.

## Acceptance Criteria

- [ ] Stability test: two identical requests produce identical breakpoint
      offsets and identical up-to-breakpoint hash.
- [ ] `max_breakpoints=4` default enabled; opt-out via config works.
- [ ] Analytics snapshot exposes hit-rate field.
- [ ] Benchmark script produces a table showing delta hit-rate and tokens.
- [ ] `go test -race ./...` green.

## Out of Scope

- Auto-detection of provider support (Anthropic-only for now; OpenAI has
  different caching surface).
- Dynamic per-request breakpoint count adjustment.

---

## Validation

```
go test -race ./internal/compression/...
bun run scripts/benchmarks/prompt-cache-ab.ts
curl -s 127.0.0.1:8990/admin/status | jq .prompt_cache
```
