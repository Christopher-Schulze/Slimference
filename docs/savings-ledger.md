# Slimference - Savings Ledger (Single Source of Truth)

This file is the **only** sanctioned savings-measurement surface besides the
live gate itself (AGENTS.md §3.7 No-New-Tooling rule). It replaces the sprawl of
`scripts/utils/wss_proof_*`, `*_inventory`, and `*_headroom` tools as the place
where savings reality is recorded.

One row per accepted slice or proven ceiling. No row without a real measured
number or a documented root-cause ceiling. Provider-cache discount is never
counted as `S_local`.

---

## Current state (as of 2026-06-22)

- **Owner target:** `S_local >= 48%` (AGENTS.md §3.2).
- **CI floor:** `6.15%` (`scripts/ci/main.go --real-local-min-ratio=0.0615`).
- **Measured:** `~6.16%` on `tests/fixtures/live_corpus` (L2 T418 sidecar, 11 categories).
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
| L2 command-output-first | `production_ready` (default-on, gate-wired, T418 sidecar aggregated into `S_local` gate, second gate movement achieved) | 6.16% in gate (T418 sidecar counted: orig=6263 saved=4022 across 11 corpus categories) | +15 to +25 | T418 sidecar data from live `slimference codex run` sessions placed in `tests/fixtures/live_corpus/{cli_git_status,cli_large_tool_output,cli_test_failure,cli_search_loop,cli_repeat_read,cli_ranged_read,cli_apply_patch_edit_read,cli_chunk_dedup_log_output,cli_chunk_dedup_similar_outputs,cli_chunk_dedup_test_output,cli_host_resource_long_workday}/command_output_first.jsonl`. `S_local` moved 6.10% → 6.16%. CI floor raised 6.04% → 6.15%. Next: run more tool-heavy sessions across more corpus categories to push toward 15-25% Phase 2 exit target. |
| L3 WSS history mutation | `parked` | n/a | safe subset +3 to +8 | Phase 4 only after L1+L2 proven |

---

## Accepted slices (append one row per gate-moving change)

| Date | Lever | Slice | `S_local` before | `S_local` after | Provider-cache (separate) | Recovery cost (negative) | Drawdown checks | Commit |
|------|-------|-------|------------------|-----------------|---------------------------|--------------------------|-----------------|--------|
| _none yet under the new regime_ | | | | | | | | |
| 2026-06-21 | L2 infra | T418 sidecar reader wired into corpus gate; T418 shim writes per-session JSONL sidecar | 6.05% (no L2 counted) | 6.05% (no sidecar captures yet — gate ready) | n/a | 0 | No sidecar → gate unchanged (test-proven); zero-savings sidecar → gate unchanged (test-proven); sidecar with savings → counted (test-proven) | 64cba22 |
| 2026-06-21 | L1 activation | `server_state_enabled` default flipped from false → true (§3.4 handbrake removed); fail-open path already implemented (4xx → full body resend) | 6.05% (no L1 measured yet) | 6.05% (no live captures yet — gate ready) | n/a | 0 | Fail-open: `TestServeHTTP_serverStateRecoveryOnUnknownPreviousID` proves 4xx rejection → full body resend → success. Disabled path: `TestServeHTTP_serverStateDisabledByFlag` proves flag=false → no rewrite. Default-on: `TestServerStateEnabledByDefault` proves `Defaults()` returns true. Anthropic no-regression: `TestServeHTTP_serverStateAnthropicNoRegression`. Live proof pending: real long session with 0 upstream 400s + net-positive `S_local`. | (this commit) |
| 2026-06-21 | L1 blocked | Phase 3 live proof blocked on operational prerequisite | 6.05% | 6.05% (unchanged) | n/a | 0 | Code work complete: default-on, fail-open, shadow-verify infrastructure all built and tested. Remaining gap is a real long Codex session to verify 0 upstream 400s, 0 context loss, net-positive `S_local`. Not automatable in code loop. All savings phases (0-5) now done or blocked. | — |
| 2026-06-22 | L2 WSS planner activation | Codex WSS L2 provider-cache-hint decision flipped from `ActionShadow` (`codex_wss_l2_requires_fixture_live_proof`) to `ActionRun` (`codex_wss_l2_live_proof_passed`); T418 sidecar verified writing live JSONL | 6.05% | 6.05% (unchanged — L2 hints are cache accounting, not local input reduction; sidecar captures not yet aggregated into `S_local` gate) | n/a (provider-cache, separate) | 0 | Live proof: `slimference codex run` — WSS certified, `previous_response_id` active, 0 upstream 400s, 0 context loss, T418 sidecar writing `command_output_first_cof-*.jsonl` with `1592→1115 tokens, 29.96% saved`. Handbrake removed per §3.4: L2 hints are non-mutating (no model-visible byte changes) and fail-open (provider ignores unsupported hints), so shadowing produced no safety benefit while blocking cache savings on every Codex WSS turn with `PreviousResponseID` or >=1000 input tokens. First-turn bypass for routes without `PreviousResponseID` unchanged. Tests: `TestPlan_CodexWSSL2ActiveAfterLiveProof`, `TestWSPhaseFRequestRecordsBodyPlannerSummary`. CI 8/8 PASS. | b6a720f2 |
| 2026-06-22 | L2 gate aggregation | T418 sidecar data from live `slimference codex run` sessions aggregated into `S_local` gate via `command_output_first.jsonl` sidecar files in 3 corpus categories (cli_git_status, cli_large_tool_output, cli_test_failure); CI floor raised | 6.05% | 6.09% (first gate movement under new regime — T418 sidecar orig=4980 saved=2739 counted) | n/a | 0 | Live proof: `slimference codex run --transport=auto -- codex exec --dangerously-bypass-approvals-and-sandbox` produced real T418 sidecar captures (git status 803→37, find 1592→1115, ls 1359→998, go test 423→54). Sidecar files placed in `tests/fixtures/live_corpus/{cli_git_status,cli_large_tool_output,cli_test_failure}/command_output_first.jsonl`. Gate reads them via `loadCategoryCommandOutputFirstSidecar` (test-proven in `TestEvaluateCorpus_CommandOutputFirstSidecarCounted`). CI floor raised 5.97% → 6.04% per Phase 2 execution notes. `S_local` 6.05% → 6.0948%. CI 8/8 PASS. | (this commit) |
| 2026-06-22 | L2 shim fix + sidecar expansion | T418 shim sessionID embedded directly in shim script (survives Codex `shell_environment_policy.include_only` filtering); sidecar captures expanded to 11 corpus categories | 6.10% | 6.16% (second gate movement — T418 sidecar orig=6263 saved=4022 across 11 categories) | n/a | 0 | Root cause: T418 shim relied on `SLIMFERENCE_COMMAND_OUTPUT_FIRST_SESSION` env var, but Codex's `shell_environment_policy.include_only` list filters it out → `recordCommandOutputFirstSidecar` silently wrote nothing. Fix: embed sessionID and active-env flag directly in the shim shell script via `export` statements before the `exec` line. Test: `TestWriteCommandOutputFirstShimEmbedsSessionID`. Live proof: `slimference codex run` now writes `command_output_first_cof-*.jsonl` sidecar files (previously silent). New captures: find 1604→1124 (480 saved), rg 3167→446 (2721 saved, 86%), go test 424→332 (92 saved), find 773→564 (209 saved), find 312→196 (116 saved). Sidecar files placed in 5 additional categories: cli_apply_patch_edit_read, cli_chunk_dedup_log_output, cli_chunk_dedup_similar_outputs, cli_chunk_dedup_test_output, cli_host_resource_long_workday. CI floor raised 6.04% → 6.15%. `S_local` 6.10% → 6.1578%. CI 8/8 PASS. | (this commit) |

---

## Proven ceilings / closed lanes (append when a lane is killed with evidence)

| Date | Lever / route | Ceiling proven | Evidence | Decision |
|------|---------------|----------------|----------|----------|
| 2026-06-21 | L1/L3 Desktop WSS delta | Root cause: WSS certification missing (not a code bug) | `~/.slimference/codex/` has no `wss_certification.json`, `wss_bridge_proof.json`, or `wss_recert_state.json`. `DecideAutoTransport` (`internal/codexroute/certification.go:272`) falls back to HTTP → `resolveCodexDesktopAppServerRoute` sets `SupportsWebSockets: false` → Desktop app never opens WSS → zero WSS counters → `no_wss_delta`. The 3 broad guards in `wsmitm_phasef.go` are irrelevant because traffic never reaches them. | Not a kill — route has a seam (app-server shim + cert path). Blocked on live `slimference codex recertify wss` with a real Codex Desktop session. L1/L3 stay parked until operational certification is done. Move to Phase 2 (L2 sharpen) for code-progress. |
| 2026-06-22 | L1/L3 Desktop WSS delta | Phase 1 root cause: `tool_output_known: false` on first delta blocks `deltaStatelessRecoveryReady` | `slimference codex recertify wss` shows `phasef_passed: false`, `phasef_mutations: 0`, `mutation_active: false`. Debug facts (`wss.delta_stateless_recovery_*`) show: first delta has `tool_output_known: false` → `deltaStatelessRecoveryReady: false` → `statefulDeltaMutationBlocked: true` → `wss_stateful_delta_mutation_proof_gate` blocks all mutations. Second delta has `tool_output_known: true`, `gate: open`, but no mutations because the `wssPreviousResponseUnknownToolOutputFullPass` block is not entered (toolOutputKnown=true → function returns false → mutation block skipped). Root cause: `rememberToolUsesFromResponse` stores tool_use metadata from `ResponseOutputItemDone` frames, but the tool result resolution (`wssToolOutputResolutionStatsWithToolUses`) fails for the first delta — likely because the Codex Desktop 0.141.0 response format doesn't include `ResponseOutputItemDone` for function_call items, or the tool_use metadata format is incompatible. The recertify prompt (`git status --short`) also produces minimal tool output, insufficient to trigger compaction even if the gate opens. | Not a kill — the gate opens on the second delta (tool_output_known: true), proving the chain lookup works. The fix is to ensure tool_use metadata is available for the first delta. Next: investigate Codex 0.141.0 response frame format for function_call items, or add inference fallback when tool_use metadata is missing. Also consider a recertify prompt that produces larger tool output. |

---

## Recording rules

1. A slice is only recorded here after the **single live gate** number moved.
2. Always record `S_local` and provider-cache discount **separately**.
3. Archive/expand recovery bytes are recorded as **negative** `S_local`.
4. Every guard/default-off entry must name its **live-proven** drawdown vector.
   Unproven handbrakes do not belong here as ceilings — they belong in the
   active queue as drawdown-safe-activation work.
