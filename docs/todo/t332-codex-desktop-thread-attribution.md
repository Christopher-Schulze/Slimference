# T332: Codex Desktop thread attribution and savings labels

Status: done

## Why

Launch Codex CLI is intentionally rooted at the TUI cwd, but Launch Codex App is
not. Codex Desktop can switch projects and run multiple threads inside the app,
so Slimference must attribute routed traffic by Codex thread/session metadata
instead of implying that the TUI cwd controls Desktop state. The daily Activity
and Savings surfaces also must not label raw `codex_chatgpt` provider traffic as
Codex App without thread evidence.

## Acceptance

- Codex thread metadata lookup is shared outside the TUI and reads
  `~/.codex/state_5.sqlite` by thread/session id.
- WSS request summaries carry a neutral `codex` client family and parse
  `x-codex-turn-metadata` client/source hints when present.
- Activity and Savings resolve thread title, cwd, model/source where Codex has
  persisted them.
- `slimference savings` text and JSON expose per-session display name, project
  path, and client family without raw provider labels as the user-facing source
  of truth.
- No Desktop cwd binding, Desktop app patching, model-list mutation, or context
  mutation is introduced.

## Subtasks

- [x] Extract Codex thread metadata lookup to `internal/codexthreads`.
- [x] Reuse the shared lookup from TUI Activity/Savings.
- [x] Enrich `slimference savings` per-session rows from Codex thread metadata.
- [x] Stop treating `codex_chatgpt` provider fallback as Codex App proof.
- [x] Add focused regression tests for WSS client metadata, Activity labels, and
  Savings thread enrichment.
- [x] Add direct `internal/codexthreads` tests for current Codex schema, older
  schema fallback, missing DB/table behavior, and session id normalization.
- [x] Update product documentation.

## Notes

- Desktop route behavior stays process-local and cwd-agnostic. The app owns
  project/thread selection; Slimference only observes routed WSS frames and maps
  their session ids back to Codex's persisted thread metadata when available.
- This is display/accounting hardening only. It does not change token mutation,
  prompt cache steering, provider routing, or Desktop model metadata.
- The Codex thread lookup now introspects the local `threads` table and treats
  optional columns as optional, so a missing `thread_source`, `model`, or
  `updated_at_ms` column degrades to empty labels/timestamp fallback instead of
  breaking Activity/Savings rendering.

## Verification

- `go test ./internal/codexthreads -count=1`
- `go test ./internal/codexthreads ./internal/proxy ./internal/tui ./cmd/slimference -run 'TestWSSRequestMetaFromRawMatchesBodyHelpers|TestWSCodexSessionIDFromCodexResponsesShape|TestView_ActivityRenderShowsSessionsAndTraffic|TestUserClientLabelDoesNotTreatCodexProviderAsDesktopApp|TestSavingsSessionsUseCodexThreadMetadata|TestComputeSavingsDecisionMechanismBreakdown|TestFormatSavingsTextDecisionCacheAndSigned' -count=1`
