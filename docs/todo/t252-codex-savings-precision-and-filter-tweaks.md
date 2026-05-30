# TASK 252: Codex savings precision + filter/marker tweaks (quick wins)

Status: [x] SOLVED - precision, filter caps, parser expansion, stderr, markers, and accounting hardened
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
  the old positional cap and prove it survives. PASS means every remaining built-in
  parser that truncates output has either an attention-priority test or an explicit
  "safe positional only" rationale in this file.
- New Tier-1 parsers land for eslint-json, tsc, `kubectl -o json`, `cargo metadata`,
  `terraform show -json`, each keeping failures/values faithfully.
- stderr is compacted on the CLI path.
- Markers use a compact, voice-neutral, reinject-compatible structured notation.
- Coverage gate green; doctrine clean.

## Sub-Tasks

- [x] Use `o200k_base` for Codex token counting in the WSS/Layer-0 guards.
- [x] Fix `delta.go` doubled-newline (one newline per diff line); add a test asserting
      no `\n\n` inside a single-line-change delta.
- [x] Make remaining parser caps token-budget-aware + error/match-priority.
      Required audit surface: every `Max*`, `Limit*`, `first N`, `head`, `tail`,
      `truncate`, and slice-bound cap in `internal/filter`. Each cap gets one of:
      (a) priority-preserving implementation + failable late-error test, or
      (b) documented safe-positional rationale with a test proving no diagnostic
      content is dropped.
- [x] Add Tier-1 parsers: eslint-json, tsc, `kubectl -o json`, `cargo metadata`,
      `terraform show -json`.
- [x] Compact stderr on the CLI filter path.
- [x] Convert markers to a compact structured notation (coordinate wording with t249
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
- 2026-05-30: Codex WSS/Layer-0 guards use `o200k_base`; observability now names the
  o200k tokenizer distinctly.
- 2026-05-30: delta output emits one newline per diff line.
- 2026-05-30: log and lint truncation now preserve late error/failure lines ahead of
  benign positional caps. Search grouping now preserves both head and tail matches/files
  under cap pressure, and terraform output compaction keeps late diagnostic/error
  outputs before benign positional entries. Broader parser-specific caps remain open.
- 2026-05-30: `gh ... list` and `glab ... list` previews now preserve late
  attention rows (failed/cancelled/error/security/etc.) ahead of benign positional
  rows. Regression tests put failed CI/pipeline rows past the old 15-row cap and
  prove they survive.
- 2026-05-30: SARIF summaries now sort error-level findings ahead of warning/note
  rows before the 10-result cap; a regression test puts an error past the old cap.
  Terraform `show -json` resource changes now sort destructive/create/update actions
  ahead of no-op rows before the 30-row cap; a regression test puts a replacement
  past the old cap.
- Remaining cap audit classification:
  - `builtin_testrun_tier1.go` caps only already-failed test cases and short
    per-failure snippets; this preserves failure identity and count, not benign rows.
  - `builtin_eslint.go` caps after severity ordering; errors cannot be crowded out
    by warnings.
  - `builtin_log.go`, `builtin_compact_helpers.go`, `builtin_search.go`,
    `builtin_terraform.go`, `builtin_container.go`, `builtin_gh.go`,
    `builtin_glab.go`, `builtin_structured_json.go`, and `builtin_sarif.go` have
    late-attention regression coverage.
  - `builtin_format.go` is safe positional-by-design: long formatter output is
    replaced only with a count of formatted files, and file identity is not treated
    as diagnostic content.
  - `filters_toml.go` and `passthrough.go` caps are operator-configured explicit
    limits and emit direct truncation/selection semantics.
  - `log_shape.go` caps only detection sampling, not model-facing output.
- Final PASS gates:
  - `rg -n "truncate|limit|Limit|Max|first|head|tail|\\[:|cap" internal/filter`
    reviewed with no unexplained diagnostic-dropping caps.
  - Targeted tests cover late diagnostics for each priority cap family.
  - `go test ./internal/filter ./cmd/slimference ./scripts/utils` green.
  - Full `go build ./... && go vet ./... && go test ./... && go run ./scripts/ci`
    green before closing the task.
- 2026-05-30: eslint `--format json`, TypeScript diagnostics, `kubectl -o json`,
  `cargo metadata`, and `terraform show -json` are now Tier-1 parsers. They keep
  diagnostic/attention rows before benign summaries and fail open on unknown shapes.
- 2026-05-30: `slimference filter <cmd>` now strips ANSI and applies Layer-0
  compaction to stderr as well as stdout, while preserving raw stderr for audit and
  exit-code fidelity.
- 2026-05-30: Read-delta, unchanged-read, unchanged-output, stale-read, obsolete-read,
  and recovery-note text now use compact neutral `[context-* ...]` marker notation.
  Product identity was removed from model-facing marker text; `local-archive://` URIs
  remain reinject-compatible.
- 2026-05-30: WSS/admin/report accounting now separates billable input-token savings
  from output-wire bytes and request-side reduced bytes. Output-wire savings are no
  longer folded into the billable headline.
- Doctrine: content-free, fail-open, scoped.

## Deviations

(none)
