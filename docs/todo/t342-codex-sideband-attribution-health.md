# T342 Codex Sideband Attribution Health

## Why

After T341, live post-install Codex `/responses` rows no longer collapsed into
`empty`, but the decision log can still contain content-free Codex sideband
requests such as `/backend-api/codex/models`. Those are not conversations and do
not have a meaningful thread identity. Counting them as un-attributed Codex
sessions makes attribution health look worse than the actual conversation path.

## Acceptance

- Codex attribution health counts conversation-bearing Codex endpoints only.
- Codex `/responses` and chat-completion style rows still count and still warn
  when missing strong `codex-http:<thread>` or `codex-wss:<thread>` attribution.
- Codex sideband endpoints such as `/models` remain in total decision accounting
  but do not count as unattributed sessions.
- No savings totals, request payloads, cache keys, prompts, or model-facing data
  change.

## Sub-Tasks

- [x] Add a Codex attribution-candidate filter around attribution health only.
- [x] Keep sideband rows in total decision accounting.
- [x] Add regression coverage for `/backend-api/codex/models`.

## Notes

- Product impact: cleaner status signal. `attention` now means a conversation
  row could not be assigned, not that a harmless sideband lacked a thread.
- Drawdown risk: none. Report-only classification change.

## Verification

- `go test ./cmd/slimference -run TestSavingsCodexAttributionHealth -count=1`
