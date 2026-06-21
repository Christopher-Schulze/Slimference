# Slimference - Savings Ledger (Single Source of Truth)

This file is the **only** sanctioned savings-measurement surface besides the
live gate itself (AGENTS.md §3.7 No-New-Tooling rule). It replaces the sprawl of
`scripts/utils/wss_proof_*`, `*_inventory`, and `*_headroom` tools as the place
where savings reality is recorded.

One row per accepted slice or proven ceiling. No row without a real measured
number or a documented root-cause ceiling. Provider-cache discount is never
counted as `S_local`.

---

## Current state (as of 2026-06-21)

- **Owner target:** `S_local >= 48%` (AGENTS.md §3.2).
- **CI floor:** `5.97%` (`scripts/ci/main.go --real-local-min-ratio=0.0597`).
- **Measured:** `~6.05%` on `tests/fixtures/live_corpus` (synthetic, 332 KB).
- **Historical real-session peak:** `46.1%` on a 48M-token day (2026-06-08),
  `75.9%` (2026-06-02), from `~/.slimference/analytics/*.jsonl`
  (`saved_input_tokens`). Collapsed to ~0% from ~2026-06-13 when broad WSS
  guards landed.
- **Command-output-first (L2) all-time:** `~87%` on touched output
  (`slimference gain` all-time = 1.4M saved / 1.6M input) — but **not counted by
  the production gate yet**.

---

## Lever status

| Lever | Status | Measured `S_local` (live, gate) | candidate_potential_if_completed | Next move |
|-------|--------|---------------------------------|----------------------------------|-----------|
| L1 server-state continuation | `engineered_pending_evidence` (default-off, unproven handbrake) | not measured | +15 to +30 | Phase 3: prove `previous_response_id` acceptance + fail-open |
| L2 command-output-first | `engineered_pending_evidence` (default-on, gate-wired, no sidecar captures yet) | 0% in gate (no sidecar data yet) | +15 to +25 | Phase 2: sharpen compaction + capture real sidecar data |
| L3 WSS history mutation | `parked` | n/a | safe subset +3 to +8 | Phase 4 only after L1+L2 proven |

---

## Accepted slices (append one row per gate-moving change)

| Date | Lever | Slice | `S_local` before | `S_local` after | Provider-cache (separate) | Recovery cost (negative) | Drawdown checks | Commit |
|------|-------|-------|------------------|-----------------|---------------------------|--------------------------|-----------------|--------|
| _none yet under the new regime_ | | | | | | | | |
| 2026-06-21 | L2 infra | T418 sidecar reader wired into corpus gate; T418 shim writes per-session JSONL sidecar | 6.05% (no L2 counted) | 6.05% (no sidecar captures yet — gate ready) | n/a | 0 | No sidecar → gate unchanged (test-proven); zero-savings sidecar → gate unchanged (test-proven); sidecar with savings → counted (test-proven) | 64cba22 |

---

## Proven ceilings / closed lanes (append when a lane is killed with evidence)

| Date | Lever / route | Ceiling proven | Evidence | Decision |
|------|---------------|----------------|----------|----------|
| _none yet_ | | | | |

---

## Recording rules

1. A slice is only recorded here after the **single live gate** number moved.
2. Always record `S_local` and provider-cache discount **separately**.
3. Archive/expand recovery bytes are recorded as **negative** `S_local`.
4. Every guard/default-off entry must name its **live-proven** drawdown vector.
   Unproven handbrakes do not belong here as ceilings — they belong in the
   active queue as drawdown-safe-activation work.
