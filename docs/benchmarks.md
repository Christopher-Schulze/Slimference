# Benchmarks

Date: 2026-06-09
Release target: v0.6.0

This document records benchmark evidence that should remain reproducible from
the checked-in repository. Legacy semantic Layer 2 summarization has been
removed from the product path; current Layer 2 means response/provider-cache
accounting only. Current reports and claims must attribute savings only to
active product paths.

## Session Reports

Run:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/sample_session.jsonl
go run ./scripts/benchmarks session-report --markdown tests/fixtures/sample_session.jsonl
```

Expected report dimensions:

| Dimension | Meaning |
| --- | --- |
| Original tokens | Estimated model-visible input before Slimference reducers |
| Final tokens | Estimated input after active reducers |
| Saved tokens | `original - final` |
| Layer 0 saved | Tool-output / Codex reducer savings |
| Layer 1 saved | Deterministic compression savings |
| Layer 2 saved | Response/provider-cache savings where applicable |
| Output reduce | Provider-output / directive accounting |
| Cache hits | Local response-cache hits |

Do not record or expect any semantic-summary Layer 2 savings column. If a
fixture still treats Layer 2 as model-facing summary replacement, the fixture is
stale and must be regenerated or scrubbed.

`slimference savings <period>` also reports decision-log conversation
breakdowns when the configured decisions log is present. Text output shows a
`Decision layer net` aggregate and each top session prints `layers=...` with
measured `L0`, `L1`, `L2`, `L3`, `out`, and `tools` net token fields. JSON
exposes the same fields on `decision_sessions`. Missing counters stay absent or
zero; the report does not invent estimates.

Decision logs and savings reports also include a deterministic evidence
manifest. It is content-free: content class, safety class, action, reason,
signals, recovery label, preserved-evidence label, and token accounting only.
The visible aggregate is `Evidence decisions` plus top content classes and
signals in CLI/JSON reports and the Savings TUI, so regressions in
error-priority, stacktrace preservation, changed hunks, cache hot zones, or
negative-net reducer behavior are visible without duplicating prompt/tool
payload.

## Codex Smoke Corpus

Run:

```bash
go run ./scripts/benchmarks session-report tests/fixtures/codex
go run ./scripts/benchmarks session-report --markdown tests/fixtures/codex
go run ./scripts/benchmarks codex-smoke-gate tests/fixtures/codex
```

The Codex smoke corpus proves the reporting and regression-gate path on
checked-in scrubbed data. It is not a production savings claim. The final
step of `go run ./scripts/ci` enforces the smoke gate so report schema drift is
caught locally.

Current checked-in smoke status:

| Gate | Result | Evidence |
| --- | --- | --- |
| Codex smoke gate | PASS | 2 requests, 5,600 original tokens, 2,400 final tokens, 3,200 saved tokens, 57.14% fixture ratio |

`tests/fixtures/codex/codex-metadata.json` declares provenance, scenarios, and
the regression baseline. It must list only active layers and active workload
classes.

## Live Corpus

For the per-category live corpus:

```bash
go run ./scripts/verify -mode live-corpus-plan -category codex_cli_tool_heavy -client codex_cli
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --check
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --promotion-check
go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --maxx-check
```

The gate reports and can enforce evidence level, input-layer savings,
output-wire savings, provider-cache read/create/cached tokens, output-reduce
hits, error count, latency p95, host-resource status, and planner replay
consistency. It also emits an observed layer-combination matrix such as
`L0+L1`, `L0+L1+L2`, `L3`, `WS`, and `none`.

Current checked-in live-corpus gate status from the 2026-06-09 v0.6.0 refresh:

| Gate | Result | Evidence |
| --- | --- | --- |
| Normal corpus | PASS | 55 requests across synthetic plus live categories |
| Promotion | PASS | 51 real sessions, `codex_cli=34`, `codex_desktop=17` |
| Maxx | PASS | Same 51 real sessions plus chunk/output/tool/provider-cache/resource workload breadth |

Observed current live-corpus highlights:

| Workload class | Evidence |
| --- | --- |
| `search_loop` | 15 real sessions across CLI/Desktop, 199,030 saved tokens |
| `repeat_read` | 7 real sessions across CLI/Desktop, 22,216 saved tokens |
| `ranged_read` | 7 real sessions across CLI/Desktop, 17,855 saved tokens |
| `test_failure` | 3 CLI sessions, 13,691 saved tokens, low-error gate |
| `git_status` | 7 real sessions across CLI/Desktop, 13,798 saved tokens |
| provider-cache-backed chunk/tool work | cache-read and cached-token rows present, reported separately from local input savings |

The current checked-in live corpus contains many absolute saved-token rows with
no original-token denominator, so the normal corpus prints an overall 42.07%
ratio only for rows with known denominators. Use the absolute token counts and
per-workload gates for release claims; do not turn the synthetic denominator
ratio into a universal product average.

The latest strict content-free release proof was built from the local capture
archive:

```bash
go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures /tmp/slimference-release-proof-t296-clean-matrix.jsonl --json
go run ./scripts/utils release-proof-report /tmp/slimference-release-proof-t296-clean-matrix.jsonl \
  --resource-profile-proof ~/.slimference/captures/host-resource-codex_cli-auto-20260604T212018Z \
  --resource-profile-proof ~/.slimference/captures/host-resource-codex_desktop-20260604T212111Z \
  --json
```

The 2026-06-06 refresh wrote 70 clean release rows from 89 local proof rows and
the final report returned `gate_passed=true`, `resource_profile_proof_ok=true`,
`local_billable_input_tokens_saved=330518`, `provider_cache_read_tokens=430720`,
`tool_prune_tokens_saved=26`, `host_budget_issue_rows=0`,
`proof_event_loss_rows=0`, and `safety_issue_rows=0`.

## Scope and Limits

- Checked-in smoke data keeps gates executable; it is not enough for a final
  production savings claim by itself.
- Current local release claims are backed by clean live Codex CLI/Desktop
  evidence plus resource/profile bundles. New or broader claims still require
  matching fresh proof rows.
- Savings must be reported by active layer and route. Provider-cache economics,
  local input savings, and output-wire savings stay separate.
- Semantic-summary Layer 2 must not appear in benchmark output, metadata,
  fixtures, or release proof. Layer 2 fields are valid only for current
  response/provider-cache accounting.
