# T267 - Output-reduce quality governor

## Why

Output tokens are expensive, but output reduction can directly hurt user
workflow if it cuts explanation, code, patch content, or final answers. The
right goal is not "shorter always"; it is task-aware reduction with automatic
rollback when the user or model needs more.

## Current reality check

- Stop sequences, streamcut, repetition detection, terse hints, and output
  profile work exist in some routes.
- WSS streamcut has known terminal-safety risk and must stay off until proven.
- Output-side savings are not the same as billable input savings and must be
  reported separately.

## Product target

Output reduction becomes a runtime-governed layer:

- never damages exact code/patch/final-answer workflows
- separates output-wire savings from billable input savings
- downgrades automatically after repair turns, "you skipped" feedback, malformed
  patches, or user re-asks
- only applies aggressive profiles to task shapes where proof says quality holds

## Technical work packages

1. Define task shapes:
   - short status
   - code review
   - patch generation
   - explanation
   - deep analysis
   - command output relay
   - final summary
2. Define per-task profile rules:
   - no aggressive output cut for patch/code/final exact content
   - conservative cut for status/boilerplate
   - repetition detector only where replacement is protocol-safe
   - WSS streamcut disabled until terminal-safe proof exists
3. Add quality signals:
   - repair-turn detection
   - user re-ask
   - malformed patch
   - missing requested detail
   - "too short" / "you skipped" patterns
4. Add automatic policy response:
   - downgrade profile for session/bucket
   - cooldown period
   - full output after negative signal
   - record reason in audit
5. Add accounting split:
   - output-wire bytes saved
   - output tokens estimated
   - billable input untouched
   - no mixed headline total

## Zero product-drawdown gates

- No output reducer may alter code blocks, patches, JSON payloads, or protocol
  terminal frames unless exact safety is proven.
- WSS streamcut remains disabled until a valid terminal sequence is captured and
  live-proven.
- Any repair signal disables aggressive output reduction for that workload
  bucket.
- User-visible truncation must not be hidden as "success".

## Savings targets

- Status/boilerplate-heavy outputs: measurable output-wire savings.
- No increase in repair turns or user re-asks in live corpus.
- No malformed patch regressions.

## Verification

- Unit tests for profile selection.
- Golden tests for code/patch preservation.
- Repair-turn feedback tests.
- HTTP streamcut tests stay green.
- WSS terminal-safe proof before WSS streamcut is enabled.
- Live corpus A/B for aggressive profiles.

## Done

Output reduce is maxxed when it saves where safe, backs off automatically where
quality signals degrade, and never hides output savings as billable input
savings.
