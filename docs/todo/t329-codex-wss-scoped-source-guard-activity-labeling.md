# T329 Codex WSS Scoped Source Guard and Activity Labeling

Status: Done

## Why

T328 stopped recurrent Codex WSS 400 failures by full-passing source-like tool output
when Codex supplied `previous_response_id`. The guard was safe but intentionally
conservative. The product needs the same safety with less savings blast radius, and
the TUI Activity view must show the real Codex surface instead of guessing CLI/App
from the shared `codex_chatgpt` provider label.

## Acceptance

- Large source-like WSS tool results after `previous_response_id` still full-pass.
- Small source snippets are no longer full-passed by that guard.
- Debug facts expose source-tool byte totals for future proof review.
- Activity view resolves routed Codex sessions through Codex thread metadata and
  shows CLI/App, title, cwd, and model when available.
- Activity view does not expose raw `codex_chatgpt`, `websocket_phasef`, or
  `/backend-api` labels.
- Tests cover the scoped source guard and Codex CLI Activity labeling.

## Sub-Tasks

- [x] Scope the source-continuation guard to large source-like tool results.
- [x] Add source-tool byte debug facts.
- [x] Read Codex `state_5.sqlite` thread metadata by Slimference WSS session ID.
- [x] Cache metadata lookups so TUI rendering does not query SQLite every frame.
- [x] Render Activity rows from real Codex thread metadata when present.
- [x] Add focused regression tests.

## Notes

- Latest inspected Golem sessions in `~/.codex/state_5.sqlite` are `source=cli`
  with readable titles and cwd data. The previous Activity view mapped them to
  Codex App because it inferred from `provider=codex_chatgpt`.
- Daemon logs after the T328 build did not show a new Codex WSS upstream 400 in
  the inspected tail. Decision logs showed the source-continuation bypass firing
  only on the newest large source-like CLI WSS requests.
- The new source guard threshold is `4096` bytes per source-like tool result.
  Tiny source snippets avoid the quarantine; large source continuation remains
  fail-open protected.

## Deviations

- None.
