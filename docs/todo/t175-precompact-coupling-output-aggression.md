# TASK 175: PreCompact signal coupling — extend to output aggression

Status: TODO (planning 2026-05-16)
Priority: P1
Scope: `internal/proxy/handler.go`, `internal/proxy/precompact_signal.go`, `internal/outstop/`, `internal/config/config.go`

## Why

When the Codex PreCompact hook fires we know the session is near its auto-compaction window. We already use this to halve the sliding-window for L1 aggression (t164). Output-side aggression (terse-hint, stricter stop sequences, aggressive markdown-normalize) should also activate in the same window — that's the moment the user most needs us to save tokens.

**Why:** Symmetric: when context is tight, BOTH input compression AND output reduction should escalate. Doing only one is leaving leverage on the table.
**How to apply:** When `hasRecentPreCompactSignal` returns true, also enable: t169 terse-hint, t165 expanded stop-sequence list, t168 markdown-normalize, t167 stricter repetition threshold.

## Target State

1. Add `Proxy.aggressionMode()` returning `normal|hot` based on the PreCompact signal age.
2. In `injectOpenAIPromptCache` neighbour code: when hot, mutate the request via:
   - Expanded stop-sequence list (t165's full registry vs reduced default)
   - Optional terse-hint inject (t169) — gated still
3. In SSE pipeline: when hot, lower thresholds for t167 repetition cuts.
4. Telemetry: per-mode counter so analytics shows hot-mode firing rate.

## Acceptance

- Without PreCompact marker: normal behaviour.
- With recent PreCompact marker: stop-sequence list expanded; t169 honoured if enabled; t167 thresholds tightened.
- Marker expires (60s TTL) → returns to normal.
- Live e2e test confirms the aggression escalation fires.

## Sub-Tasks

- [ ] Centralised `aggressionMode()` helper.
- [ ] Thread the mode through downstream layers.
- [ ] Tests: mode transitions on marker write/expire.

## Notes

- Builds directly on t164 (signal infrastructure) and t165-t169 (output reductions).
- No new external API surface; pure internal coupling.

## Deviations

(none yet)
