# TASK 160: Parser tier system (full / degraded / passthrough)

Status: TODO (planning 2026-05-15)
Priority: P2
Scope: `internal/parser/` (new), `internal/filter/`, `internal/debug/`, `docs/documentation.md`

## Why

Our `TryCompact*` functions are ad-hoc Go regexes. When a parser fails, we either silently pass through raw output or compact incorrectly. There is no structured fallback ladder, no telemetry on which tier kicked in, and adding a new parser requires re-inventing the fallback pattern each time. RTK formalized this as Tier-1 (structured: JSON/TAP/SARIF), Tier-2 (regex degrade), Tier-3 (truncate). The pattern is clean, testable, and gives debug-grade observability. Porting the pattern eliminates the silent-failure class entirely.

**Why:** Silent fallthrough is the worst failure mode for a compactor: the user sees less savings, has no idea why, and we can not iterate. A tier-aware framework makes every decision auditable.
**How to apply:** Every new `TryCompact*` or embedded TOML filter registers under the tier framework. Decision-log emits `tier=N reason=<why>` per call.

## Target State

1. New `internal/parser/` package with:
   - `type Tier int` (`TierFull = 1`, `TierDegraded = 2`, `TierPassthrough = 3`).
   - `type Parser interface { Name() string; Tier() Tier; CanHandle(cmd, stdout, exit) bool; Parse(in []byte) (out []byte, ok bool, err error) }`.
   - `type Chain []Parser` with `Run(in []byte) (out []byte, tier Tier, parserName string)` that walks Tier 1 -> 2 -> 3 in order.
2. Refactor existing `TryCompact*` functions in `internal/filter/` into `parser.Parser` implementations, declaring their tier. Most remain Tier-2 (regex-based) initially.
3. New strong Tier-1 parsers as we go:
   - `go test -json` (already partial in `TryCompactGoTestJSON` — promote to Tier-1).
   - `vitest --reporter=json`, `jest --json`, `pytest --json-report`.
   - SARIF-emitting linters (clippy, golangci-lint, eslint with sarif formatter).
   - `cargo test --message-format=json`.
4. Truncate fallback (`TruncateStdoutWithHint`) becomes the explicit Tier-3 sink.
5. `internal/debug/decisions.go` `DecisionEntry` gains `Tier int`, `Parser string`, `FallbackReason string` fields. JSONL output includes them.
6. CLI: `slimference debug parsers` lists every registered parser with name + tier + handled commands. `slimference filter --trace -- <cmd>` shows which tier ran and why.
7. Embedded TOML filters from t159 declare `tier = 1|2|3` in their TOML metadata (default Tier-2 for regex-driven, Tier-3 if pure truncate, Tier-1 if structured).

## Acceptance

- All current `TryCompact*` functions wrapped as `parser.Parser` implementations without behavior change (snapshot tests confirm).
- New Tier-1 parsers for `go test -json` / `vitest-json` / `jest-json` ship green.
- `slimference debug parsers` lists >= 30 registered parsers.
- Every filter-run SQLite row records `tier` + `parser_name`.
- `slimference filter --trace` output includes a tier-decision line per stage.
- 100% statement coverage on `internal/parser/`.

## Sub-Tasks

- [ ] Define `Tier`, `Parser`, `Chain` in `internal/parser/tier.go`.
- [ ] Registry: `internal/parser/registry.go` with `Register(parser Parser)` + `Lookup(cmd) Chain`.
- [ ] Migrate existing `TryCompact*` functions in `internal/filter/try_compact_*.go` to `parser.Parser`; keep public thin-wrapper signatures for backwards compat in tests.
- [ ] Promote `TryCompactGoTestJSON` to a full Tier-1 parser; add `vitest_json.go`, `jest_json.go`, `pytest_json.go`.
- [ ] Add SARIF-aware Tier-1 parsers for the major linters (`clippy`, `golangci-lint`, `eslint`).
- [ ] Wire `DecisionEntry` extensions in `internal/debug/decisions.go`; propagate from `internal/filter/pipeline.go`.
- [ ] Add `slimference debug parsers` subcommand (`cmd/slimference/debug_cmd.go`).
- [ ] Add `--trace` flag to `slimference filter` (debug stream to stderr; quiet by default).
- [ ] Extend embedded-TOML loader (from t159) to read `tier` field.
- [ ] Snapshot tests: every Tier-1 parser has both happy-path JSON fixture and corrupt/partial JSON fixture (must fall back to Tier-2).
- [ ] `docs/documentation.md`: new section "Parser tiers" with architecture diagram and authoring guide.

## Notes

- Tier-1 parsers must be strict: if the input is not the expected structured format, return `ok=false, err=nil` so the chain moves to Tier-2.
- Never panic from a parser; defer-recover at chain level to convert panics into Tier-3 passthrough + alarm log.
- A single command can have multiple Tier-2 parsers (e.g., `git log` graph vs. `git log` oneline) — `CanHandle` disambiguates.

## Deviations

(none yet)
