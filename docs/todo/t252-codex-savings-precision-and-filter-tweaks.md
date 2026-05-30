# TASK 252: Codex savings precision + filter/marker tweaks (quick wins)

Status: [ ] QUEUED
Priority: P2 - small, low-risk, high-certainty improvements across all layers
Scope: Codex-only. Token-accounting precision, delta formatting, filter cap quality,
more Tier-1 parsers, stderr compaction, marker notation.

## Why

Several small, verified issues each cost a few percent or add drawdown:

- Token guards use `cl100k_base` (`internal/tokens/counter.go:23`), but GPT-5-codex /
  gpt-4o bill in `o200k_base` (`ModelEncoder("gpt-4o")` already returns `o200k_base`).
  Every WSS guard (`afterTokens < beforeTokens`) counts with the wrong encoding, so
  decisions are slightly off and occasionally net-negative.
- `readcache/delta.go` emits doubled newlines: `splitDeltaLines` uses
  `strings.SplitAfter` (lines retain `\n`, `delta.go:74`) and the hunk builder appends
  another `\n` -> output is `"-x\n\n+y\n\n"` (reproduced). Lossless but inflates the
  delta and is permanent in server state.
- Filter caps are positional (search 20/file 30 files, lint head-60, terraform 30, log
  100); a needed match/error past the cap is dropped, and on the delta wire that loss
  is permanent.
- Tier-1 structured parsers are missing for common faithful cases (eslint `--format
  json`, `tsc`, `kubectl -o json`, `cargo metadata`, `terraform show -json`).
- The CLI path strips ANSI from stdout only; stderr (where most failures go) is
  uncompacted.
- Markers are prose; a compact structured notation is cleaner and more parseable.

## Acceptance

- Codex token guards use `o200k_base` (correct encoding for the active Codex model).
- Delta output has exactly one newline per diff line (no `\n\n`); tests assert it;
  existing readcache + layer0_proxy delta tests stay green.
- Filter caps are token-budget-aware and preferentially retain error/failure/match
  lines (reuse existing failure-detection helpers); tests place the needed line past
  the old positional cap and prove it survives.
- New Tier-1 parsers land for eslint-json, tsc, `kubectl -o json`, `cargo metadata`,
  `terraform show -json`, each keeping failures/values faithfully.
- stderr is compacted on the CLI path.
- Markers use a compact, voice-neutral, reinject-compatible structured notation.
- Coverage gate green; doctrine clean.

## Sub-Tasks

- [ ] Use `o200k_base` for Codex token counting in the WSS/Layer-0 guards.
- [ ] Fix `delta.go` doubled-newline (one newline per diff line); add a test asserting
      no `\n\n` inside a single-line-change delta.
- [ ] Make filter caps token-budget-aware + error/match-priority (search/lint/
      terraform/log); tests.
- [ ] Add Tier-1 parsers: eslint-json, tsc, `kubectl -o json`, `cargo metadata`,
      `terraform show -json`.
- [ ] Compact stderr on the CLI filter path.
- [ ] Convert markers to a compact structured notation (coordinate wording with t249
      recovery note).

## Notes

- % impact per item (rough, on top of today): o200k tokenizer ~+2-5% precision across
  ALL layers; doubled-newline fix ~+1-2% on changed-reads; token-aware filter caps
  ~+1-3% plus fewer dropped error/match lines; new Tier-1 parsers ~+2-5% lossless;
  stderr compaction ~+1-3%; structured marker notation cleaner/less prompt-contamination
  (no direct %). o200k and filter caps also reduce net-negative/drawdown cases. No hard
  dependencies.
- The marker notation sub-task should land with or after the t249 recovery note so the
  notation and the recovery contract agree.
- Doctrine: content-free, fail-open, scoped.

## Deviations

(none)
