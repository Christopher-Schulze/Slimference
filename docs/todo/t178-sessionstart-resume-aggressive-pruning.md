# TASK 178: SessionStart resume — aggressive history pruning

Status: TODO (planning 2026-05-16)
Priority: P2
Scope: `internal/proxy/handler.go`, `internal/hooks/codex.go` SessionStart handler, `internal/sessions/`

## Why

When Codex resumes a session (`SessionStart source=resume`) the entire prior conversation is restored. The user almost never wants the full history — they want to continue from where they left off. The first few requests after a resume are the highest-value moment to aggressively prune old context: the model lost continuity anyway (process restart), so we can compress more without further loss.

**Why:** Resumed sessions are a unique moment where the model's prior internal state is gone, so we lose nothing by compacting hard.
**How to apply:** When SessionStart hook fires with `source=resume`, set a session-scoped flag that the proxy reads on subsequent requests for N turns and applies aggressive L1/L2 to older history.

## Target State

1. SessionStart handler writes `~/.slimference/run/resume/<session_id>.json` marker (similar shape to compactsignal).
2. Proxy reads this marker on next N (=3) requests; while active, treat the entire pre-resume history as eligible for aggressive compaction (skip our default sliding-window protection).
3. Marker auto-clears after N turns or 5 minutes.

## Acceptance

- Codex resumes session → marker written.
- Next request: history pre-resume is compressed at 0.15 ratio (vs default 0.25).
- After 3 requests: marker cleared, back to normal mode.
- Live e2e test on a resumed session shows ≥20% input-token reduction in the first 3 post-resume requests.

## Sub-Tasks

- [ ] Marker writer in SessionStart hook handler.
- [ ] Proxy reader.
- [ ] Aggression-multiplier wiring.
- [ ] Marker-expiry janitor.
- [ ] Tests + integration test.

## Notes

- Combines naturally with t164 fail-open store: similar file-based marker pattern.
- Risk: model may legitimately need the pre-resume history. The 3-turn ramp-back-to-normal mitigates this.

## Deviations

(none yet)
