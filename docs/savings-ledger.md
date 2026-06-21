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
| L1 server-state continuation | `engineered_pending_evidence` (default-on with fail-open, live proof pending) | not measured | +15 to +30 | Default flipped on (§3.4 handbrake removed). Fail-open: 4xx rejection → full body resend (tested). Live proof: run real long session, verify 0 upstream 400s, 0 context loss in shadow-verify, net-positive `S_local`. |
| L2 command-output-first | `engineered_pending_evidence` (default-on, gate-wired, sidecar tested, no live captures yet) | 0% in gate (no sidecar data yet) | +15 to +25 | Needs live Codex session with T418 active + sidecar capture. Compaction code is comprehensive (git, rg, grep, go, cargo, pytest, npm, docker, kubectl, terraform, etc.). Next: run a real tool-heavy session, collect sidecar, verify gate moves. |
| L3 WSS history mutation | `parked` | n/a | safe subset +3 to +8 | Phase 4 only after L1+L2 proven |

---

## Accepted slices (append one row per gate-moving change)

| Date | Lever | Slice | `S_local` before | `S_local` after | Provider-cache (separate) | Recovery cost (negative) | Drawdown checks | Commit |
|------|-------|-------|------------------|-----------------|---------------------------|--------------------------|-----------------|--------|
| _none yet under the new regime_ | | | | | | | | |
| 2026-06-21 | L2 infra | T418 sidecar reader wired into corpus gate; T418 shim writes per-session JSONL sidecar | 6.05% (no L2 counted) | 6.05% (no sidecar captures yet — gate ready) | n/a | 0 | No sidecar → gate unchanged (test-proven); zero-savings sidecar → gate unchanged (test-proven); sidecar with savings → counted (test-proven) | 64cba22 |
| 2026-06-21 | L1 activation | `server_state_enabled` default flipped from false → true (§3.4 handbrake removed); fail-open path already implemented (4xx → full body resend) | 6.05% (no L1 measured yet) | 6.05% (no live captures yet — gate ready) | n/a | 0 | Fail-open: `TestServeHTTP_serverStateRecoveryOnUnknownPreviousID` proves 4xx rejection → full body resend → success. Disabled path: `TestServeHTTP_serverStateDisabledByFlag` proves flag=false → no rewrite. Default-on: `TestServerStateEnabledByDefault` proves `Defaults()` returns true. Anthropic no-regression: `TestServeHTTP_serverStateAnthropicNoRegression`. Live proof pending: real long session with 0 upstream 400s + net-positive `S_local`. | (this commit) |

---

## Proven ceilings / closed lanes (append when a lane is killed with evidence)

| Date | Lever / route | Ceiling proven | Evidence | Decision |
|------|---------------|----------------|----------|----------|
| 2026-06-21 | L1/L3 Desktop WSS delta | Root cause: WSS certification missing (not a code bug) | `~/.slimference/codex/` has no `wss_certification.json`, `wss_bridge_proof.json`, or `wss_recert_state.json`. `DecideAutoTransport` (`internal/codexroute/certification.go:272`) falls back to HTTP → `resolveCodexDesktopAppServerRoute` sets `SupportsWebSockets: false` → Desktop app never opens WSS → zero WSS counters → `no_wss_delta`. The 3 broad guards in `wsmitm_phasef.go` are irrelevant because traffic never reaches them. | Not a kill — route has a seam (app-server shim + cert path). Blocked on live `slimference codex recertify wss` with a real Codex Desktop session. L1/L3 stay parked until operational certification is done. Move to Phase 2 (L2 sharpen) for code-progress. |

---

## Recording rules

1. A slice is only recorded here after the **single live gate** number moved.
2. Always record `S_local` and provider-cache discount **separately**.
3. Archive/expand recovery bytes are recorded as **negative** `S_local`.
4. Every guard/default-off entry must name its **live-proven** drawdown vector.
   Unproven handbrakes do not belong here as ceilings — they belong in the
   active queue as drawdown-safe-activation work.
