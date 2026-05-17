# TASK 177: PostToolUse just-in-time awareness reminder

Status: TODO (planning 2026-05-16)
Priority: P1 (cheap, prevents redundant tool reruns)
Scope: `internal/hooks/codex.go`, `cmd/slimference/main.go` (handleCodexPostToolHook), `internal/config/config.go`

## Why

After a Bash tool output is compacted by Slimference's L0 pipeline, the model occasionally re-runs the command thinking it didn't get full output. A 10-15 token "Output above is pre-compacted by Slimference (full output archived locally)" appended via PostToolUse additionalContext eliminates these wasteful re-runs.

We already inject a similar awareness via SessionStart (t163), but mid-session reminders are more effective because the model has 30+ turns of intervening context and may forget the SessionStart preamble.

**Why:** Tiny input-token cost (~15 tokens per Bash call), saves ~100-300 tokens per prevented re-run. Net positive when even 5% of Bash calls would otherwise be retried.
**How to apply:** Append a fixed sentence to PostToolUse's additionalContext when compaction actually happened (changed=true).

## Target State

1. In `handleCodexPostToolHook`: if the output was actually compacted (saved tokens > some threshold), append the reminder to additionalContext.
2. Reminder text is short: `"Compacted output: full available via slimference debug tail."` (~10 tokens).
3. Configurable: `[compression.awareness] post_tool_reminder = "auto|always|off"` (default `"auto"` = only when significant compaction occurred).

## Acceptance

- Bash output 5000 tokens → compacted to 500 tokens → reminder appended.
- Bash output 50 tokens → not compacted → no reminder (would be pure overhead).
- A captured-session test shows reduced tool-rerun rate ≥10%.

## Sub-Tasks

- [ ] Threshold for "significant compaction".
- [ ] Reminder text + config knob.
- [ ] Tests: significant vs trivial compaction; reminder presence.

## Notes

- Don't append on every PostToolUse: that floods context with reminders.
- The reminder must be **content-only**, not a tool_use — Codex needs the model to read it as context.

## Deviations

(none yet)
