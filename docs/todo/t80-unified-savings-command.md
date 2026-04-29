# TASK 80: Unified `slimference savings` command

Status: todo
Priority: P1
Scope: `cmd/slimference/`, `internal/analytics/`
Driver: The natural user question is "wieviel hab ich heute gespart?". Today that requires `slimference gain today` + `slimference stats today` + manual addition, plus the operator has to know that prompt-cache savings live elsewhere. One canonical command kills the whole confusion.

---

## Problem

Three subcommands answer overlapping questions:

- `slimference gain` -> Layer 0 filter savings (filter.db)
- `slimference stats` -> proxy-level compression savings (analytics.db)
- prompt-cache savings -> read separately from `/admin/status` or per-request analytics

Each has different output formats, different time windows, different units. The operator has to know which is which, and add the numbers in their head.

## Target State

A single `slimference savings [today|week|month|all]` command that:

- Aggregates Layer 0 (filter.db) + Layer 1/2 (proxy analytics) + Layer 3 (response cache) + prompt-cache savings into one canonical view.
- Shows tokens saved, EUR/USD saved (using existing `gain_usd_per_million` knob), savings ratio, and a per-layer breakdown.
- Defaults to text; `--json` and `--csv` for scripting; `--by-provider`, `--by-project` for slicing.
- Existing `gain` and `stats` keep working but `slimference gain` and `slimference stats` print a deprecation hint redirecting to `slimference savings`.

## Implementation Plan

### WP1 - Aggregator
- New `internal/analytics/savings_aggregator.go` that reads filter.db + analytics.db + response-cache stats and merges per-window.
- Single struct `SavingsSummary` with named layer fields.

### WP2 - CLI
- `cmd/slimference/savings_cmd.go` with subcommands matching `gain`/`stats` style.
- Supports `--json`, `--csv`, `--by-provider`, `--by-project`, `--since=<duration>`.

### WP3 - Net-savings (T77 hookup)
- When `[quality] enabled`, also emit `net_saved_tokens` and a quality caveat line.

### WP4 - Deprecation path
- `gain` and `stats` continue to work.
- They print a one-line hint pointing at `slimference savings`.

### WP5 - Docs
- Update `docs/documentation.md` and `docs/integration.md` to lead with `slimference savings`.

## Acceptance Criteria

- [ ] `slimference savings today` prints a per-layer + total view in tokens and currency.
- [ ] `slimference savings today --json` is documented and stable.
- [ ] `slimference gain` and `slimference stats` print the deprecation hint.
- [ ] Integration test compares aggregated savings against summed legacy commands.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Per-user breakdown (single-operator product).
- Public dashboards.

## Validation

```
slimference savings today
slimference savings week --by-provider --json
go test ./internal/analytics/... ./cmd/slimference/...
```
