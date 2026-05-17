# TASK 192: Stats Dashboard v2 (per-app, per-mechanism, historical)

Status: PLANNING 2026-05-16
Priority: P1 (the "wie viel gespart wird" piece the user explicitly named)
Scope: `internal/tui/`, `internal/analytics/`, `internal/proxy/admin.go`
       (extend), persistence in `~/.slimference/analytics/`

## Why

User wants to see savings per app, per session, per mechanism, and over
time. The current admin status surfaces some counters; the TUI v1 surfaces
a subset. v2 makes everything visible, drillable, and persistent.

## Target state

Three connected views in the TUI:

### Overview (the dashboard tile shown in T191)

Already specified in T191. Recap: real-time counters for today's session,
streamcut fires, repdet hits, stale-read aging, etc. Pulled from
`/admin/state` which aggregates `output_reduce_counters` + qualityab +
filter pipeline stats.

### Stats detail (S key)

```
╔══════════════════════════════════════════════════════════════════╗
║ Slimference — Stats Detail                                       ║
╠══════════════════════════════════════════════════════════════════╣
║ Period: [ Today ] [ This Week ] [ This Month ] [ All Time ]      ║
║                                                                  ║
║ ▸ By app                                                         ║
║   Codex CLI            412 conv, 38 % input saved, 22 % output   ║
║   Codex Desktop App     87 conv, 35 % input saved, 19 % output   ║
║   Claude Code            0 conv, n/a                             ║
║                                                                  ║
║ ▸ By mechanism (input-side)                                      ║
║   T170 Stale-read aging  124 blocks, 18 312 tokens               ║
║   T174 Obsolete-prune     29 blocks,  7 044 tokens               ║
║   L1 Sliding window       —                                      ║
║   L1 Tool-output strip   217 segments, 24 901 tokens             ║
║   L2 Summarization        2 sessions, 12 880 tokens              ║
║   L3 Response cache       4 hits, 2 219 tokens                   ║
║                                                                  ║
║ ▸ By mechanism (output-side)                                     ║
║   T165 Stop-sequence      412 reqs, est 6 311 tokens             ║
║   T166 Streamcut fires     11 fires, est 3 044 tokens            ║
║   T167/T183 Repdet         37 rewrites, 44 102 bytes             ║
║   T169 Be-terse hint        0 (default off)                      ║
║                                                                  ║
║ ▸ Quality A/B (T186)                                             ║
║   Cohort split            T 211 / C 201                          ║
║   Failure rate            T 6.6 % / C 5.5 %  (Δ +1.1 pp)         ║
║   Rolled back             no                                     ║
║                                                                  ║
║ [←] Back   [E] Export JSONL                                      ║
╚══════════════════════════════════════════════════════════════════╝
```

### History (within Stats detail, scrollable)

```
Day            In saved   Out saved   Cost USD   Sessions
2026-05-16     412 318    94 207      $7.84      23
2026-05-15     376 020    81 119      $7.11      19
...
```

## Implementation

- New admin endpoint `/admin/state` (already in T191) returns the
  `SetupState` JSON.
- New admin endpoint `/admin/savings?period=today|week|month|all`
  returns aggregated rollups.
- Persistence: existing `internal/analytics/` writes JSONL events. Add
  a daily-roll-up table written to `~/.slimference/analytics/daily.jsonl`
  on session-end + on every hour boundary.
- Cost estimation: per-provider/per-model token-price table loaded from
  `~/.config/slimference/pricing.toml` (operator-editable). Defaults
  shipped for current OpenAI / Anthropic public pricing.

## Sub-Tasks

- [ ] Aggregator: `analytics.DailyRollup(period)`.
- [ ] `/admin/state` + `/admin/savings` endpoints.
- [ ] TUI stats-detail screen.
- [ ] Pricing TOML loader + defaults.
- [ ] Export action (JSONL) for off-line analysis.
- [ ] Tests covering rollup math against synthetic event corpus.

## Acceptance

- After 100 turns over multiple apps, the stats screen shows accurate
  per-app and per-mechanism counts that match the raw JSONL log
  cross-checked by a Python script.
- Cost figures match the configured price table.
- Switching period filters re-renders in ≤ 50 ms.

## Notes

- Privacy: per-session detail does not store prompt contents - only
  aggregate counts and metadata. Already the existing analytics shape.

## Deviations

(none yet)
