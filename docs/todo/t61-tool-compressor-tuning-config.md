# T61 - Tuning-Config Durchreichen für `tool_compressor` RTK-Heuristiken

Status: todo
Priority: P2
Scope: `internal/compression/tool_compressor.go`, `internal/config/`, `docs/tuning-inventory.md`
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

T22 introduced `[compression.tuning]` as the central place for
thresholds, but `internal/compression/tool_compressor.go` still has
a handful of hardcoded RTK-inspired heuristics:

- age-based compression ("if exchange age > 5, compress aggressively")
- per-tool min-length before compression
- truncation tail-length
- preview line count

These are invisible to users, cannot be A/B tested, and block any
data-driven tuning. The audit found them flagged in
`docs/tuning-inventory.md` as "TODO exposure".

## Current State

- Hardcoded constants in `tool_compressor.go`.
- `docs/tuning-inventory.md` lists them but marks "not yet in config".
- No ENV override possible.

## Target State

All heuristic constants become fields under `[compression.tuning.
tool_compressor]`:

```toml
[compression.tuning.tool_compressor]
aggressive_after_exchanges = 5
min_tokens_per_tool        = 120
truncation_head_lines      = 40
truncation_tail_lines      = 20
preview_max_lines          = 8
preview_max_bytes          = 2048
per_tool_overrides = [
  { tool = "Read",  min_tokens = 200, preview_max_lines = 12 },
  { tool = "Grep",  min_tokens = 60,  preview_max_lines = 20 },
  { tool = "Bash",  min_tokens = 100 },
]
```

- ENV overrides for every scalar field (prefix
  `SLIMFERENCE_TUNING_TOOL_COMPRESSOR_`).
- Per-tool overrides matched by name; unknown tools use defaults.
- Validation: constants must be non-negative; overrides must reference
  at least one valid field.

## Design

### Config struct

```go
type ToolCompressorTuning struct {
    AggressiveAfterExchanges int
    MinTokensPerTool         int
    TruncationHeadLines      int
    TruncationTailLines      int
    PreviewMaxLines          int
    PreviewMaxBytes          int
    PerToolOverrides         []ToolOverride
}

type ToolOverride struct {
    Tool             string
    MinTokens        *int
    PreviewMaxLines  *int
    PreviewMaxBytes  *int
    // ... (nil = inherit)
}
```

### Resolver

```go
func (t *ToolCompressorTuning) Resolve(tool string) ResolvedTool {
    base := defaultsFrom(t)
    for _, o := range t.PerToolOverrides {
        if o.Tool == tool {
            if o.MinTokens != nil { base.MinTokens = *o.MinTokens }
            // ...
        }
    }
    return base
}
```

### Logging on load

On config load, emit one slog.Info event summarising resolved per-tool
matrix:

```
config_loaded tool_compressor_tuning=
  {"aggressive_after":5, "min_default":120,
   "per_tool":["Read:200","Grep:60","Bash:100"]}
```

### Deprecation

No deprecation needed - these constants were never public API.
Changelog note: "internal tool-compressor thresholds are now
config-driven; behaviour unchanged at defaults."

## Implementation Plan

### WP1 - Move constants to config struct.
### WP2 - Default values preserve current behaviour byte-equal.
### WP3 - Per-tool override resolver.
### WP4 - ENV override wiring.
### WP5 - Config-load summary log.
### WP6 - Tests
- Default config produces byte-identical compression output on fixture
  corpus compared to pre-refactor behaviour.
- Per-tool override takes precedence.
- Invalid values rejected at load.

---

## Subtasks

- [ ] Config struct + TOML binding.
- [ ] Resolver function.
- [ ] Replace constants in `tool_compressor.go` with resolver calls.
- [ ] ENV overrides.
- [ ] Validation (non-negative, schema check).
- [ ] Byte-equal regression test on fixture corpus.
- [ ] `docs/tuning-inventory.md` update.
- [ ] `docs/documentation.md` §11 Config reference.

## Risks

- Subtle behaviour drift on edge cases during refactor. Mitigation:
  fixture corpus byte-equal regression test mandatory before merge.
- Cognitive load of many knobs. Mitigation: per-tool overrides are
  optional; docs show recommended ranges only.

## Acceptance Criteria

- [ ] Zero hardcoded heuristic constants remain in
      `tool_compressor.go` (grep check in CI).
- [ ] Fixture-corpus regression test byte-equal at defaults.
- [ ] Per-tool overrides work.
- [ ] ENV overrides work.
- [ ] `go test -race ./internal/compression/...` green.
- [ ] `docs/tuning-inventory.md` reflects new fields.

## Out of Scope

- Auto-tuning (adaptive thresholds from observed data).
- UI for editing the config.

---

## Validation

```
go test -race ./internal/compression/...
grep -R "const " internal/compression/tool_compressor.go     # expect 0 heuristic consts
./slimference doctor
```
