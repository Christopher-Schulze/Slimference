# Live corpus policy

This file governs what may and may not enter `tests/fixtures/live_corpus/`. The corpus is the only ground truth Slimference's savings claims rest on, so the privacy and provenance rules are strict.

## What the corpus is for

`tests/fixtures/live_corpus/` holds maintainer-captured-and-redacted coding sessions, one per category subdirectory. It backs the `benchmark-corpus` regression gate (`scripts/benchmarks benchmark_corpus.go`) which runs as part of `scripts/ci`. Without a real corpus, every "saves N percent" claim is unverifiable.

## What may be checked in

- The maintainer's **own** coding sessions, captured locally, scrubbed before commit.
- Synthetic / smoke fixtures, clearly marked with `"synthetic": true` in the category `metadata.json` and named under `synthetic_*` subdirectories.

## What may NOT be checked in

- Sessions captured against third-party customer code or third-party customer prompts.
- Sessions containing real secrets that the redaction pass missed: API keys, JWTs, OAuth tokens, AWS / GCP / Azure credentials, private SSH keys, passwords, internal hostnames, customer identifiers.
- Absolute filesystem paths that leak the maintainer's home directory or machine-identifying paths.
- Authorization headers or cookie values, even when the surrounding response shape is harmless.
- Anything covered by NDA or contributor confidentiality.

## Capture process (operator-driven)

The capture flow is intentionally manual. Slimference does not auto-capture sessions, ever.

0. Generate the exact category-specific runbook:

   ```
   go run ./scripts/verify -mode live-corpus-plan -category <category> -client codex_cli
   ```

   This prints the capture path, export command, metadata skeleton, and benchmark commands for the category.

   For a release/default-on promotion, generate the full ceremony first:

   ```
   go run ./scripts/verify -mode release-proof-plan
   ```

   The release runbook prints the clean CI baseline, workday window commands,
   scoped CLI and Desktop launch commands, required live-corpus category plans,
   additional maxx mechanism category plans, WSS proof-matrix command, and the
   `benchmark-corpus --promotion-check` / `--maxx-check` gates.
   It does not start capture or read private content; the operator still drives
   every live session and reviews every exported JSONL before commit.

   Existing content-free WSS proof-matrix rows can be converted into
   `benchmark-corpus` categories without copying raw frames, prompts, command
   output, auth, file paths, or decisions logs:

   ```
   go run ./scripts/utils wss-proof-inventory ~/.slimference/captures --json
   go run ./scripts/utils wss-proof-export-corpus ~/.slimference/captures tests/fixtures/live_corpus --json
   go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures <clean-release-matrix.jsonl> --json
   go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> --resource-profile-proof <codex-cli-resource-proof-bundle-dir> --resource-profile-proof <codex-desktop-resource-proof-bundle-dir> --json
   ```

   The exporter writes only scrubbed `RequestSummary` counters and
   `metadata.json` files. Existing category exports are loaded, deduplicated by
   request id, and rewritten with incoming rows appended so a weaker new proof
   cannot replace a stronger existing category gate. Rows with safety issues,
   unsupported workload classes, or no economic signal are skipped rather than
   being turned into fake proof. Exported proof rows gate on absolute live
   saved-token counters or mechanism-specific counters because proof-matrix rows
   do not preserve every original-token denominator needed for a real percentage
   claim.
   `wss-proof-clean-matrix` is the release-claim exporter. It reads proof rows
   only, normalizes stale expected-reducer labels only when the same row has
   current live reducer evidence, and writes only rows with host budget OK, zero
   safety counters, no expected-zero local-savings violation, and a positive
   economic signal unless the row is an expected-zero control.
   `release-proof-report` is the final content-free proof summary. Run it on
   that clean release matrix file or a focused release bundle, not on the whole
   historical capture archive. The archive intentionally contains old
   diagnostic and superseded rows, and those rows fail the strict gate by id.
   The report keeps
   local billable-input savings, output-wire savings, provider-cache economics,
   tool-prune schema-token savings, host-resource status, and safety counters
   separate, and it fails closed unless both CLI and Desktop resource/profile
   proof bundles are supplied. Each bundle must contain `admin-before.json`,
   `admin-after.json`, `ps-before.txt`, `ps-after.txt`,
   `workday-finish.json`, `slimference.sample.txt`, and `matrix.jsonl`; the
   JSON files must show host-budget OK plus zero WSS parse/degrade/compression
   deltas, and the local `matrix.jsonl` must contain a positive
   `host_resource_long_workday` row with `host_budget_ok` for that client. If a
   row records expected reducers, those expectations must be satisfied inside
   the same bundle; a positive savings row is not allowed to hide a missed
   mechanism-specific proof.
   CLI bundles should be generated with
   `codex-capture-run --resource-profile-proof <bundle-dir>` so the managed
   daemon writes all required files in one content-free run. Desktop bundles
   remain operator-driven because Codex.app prompts cannot be owned by the CLI
   runner.
   Host-budget rows are product-health evidence, not a substitute for the final
   CLI/Desktop resource/profile bundle proof. Historical host-budget attention
   rows or `expected_zero_savings` rows that still show local savings fail the
   release report by row id; use a clean matrix or focused release bundle for a
   release claim instead of relying on aggregate counts.

1. Run a real coding session through Slimference with the debug decision log enabled:

   ```
   SLIMFERENCE_DEBUG_DECISIONS_LOG=~/.slimference/captures/<session_name>.jsonl slimference start
   ```

   The proxy writes one redacted `RequestSummary` per request line to that path. The T109 redactor (default-on) strips secret patterns, normalises absolute paths, and drops auth headers before anything is written.

2. After the session ends, **read** the captured file end-to-end. If anything looks wrong:
   - run a regex sweep against the maintained secret-pattern list (`internal/security/secrets.go`);
   - eyeball every `tool_input` and `tool_result` excerpt;
   - delete the file and re-capture rather than commit something doubtful.

3. Decide which category the session belongs to. Suggested categories (extend as real captures arrive):
   - `bug_fix_short`
   - `feature_long`
   - `refactor_multifile`
   - `debug_session`
   - `code_review`
   - `large_test_run`
   - `desktop_tool_heavy`
   - `mixed_lang`
   - `cjk_session`
   - `large_response_streaming`

4. Create `tests/fixtures/live_corpus/<category>/<session_name>.jsonl` and a sibling `metadata.json` with at least:

   ```json
   {
     "category": "<category>",
     "description": "<what kind of work this session represents>",
     "synthetic": false,
     "evidence_level": "live_operator",
     "client_family": "<codex_cli|codex_desktop>",
     "workload_class": "<repeat_read|ranged_read|search_loop|git_status|test_failure|apply_patch_edit_read|large_tool_output|long_workday>",
     "language": "<primary language>",
     "tool_mix": "<short summary>",
     "expected_savings_min": 0.30,
     "expected_savings_max": 0.80,
     "expected_saved_tokens_min": 0,
     "expected_request_count": <int>,
     "expected_max_errors": 0,
     "expected_latency_p95_max_ms": 1000,
     "expected_provider_cache_read_min": 0,
     "expected_output_reduce_applied_min": 0,
     "expected_reread_count_max": 0,
     "expected_planner_missed_max": 0,
     "expected_planner_bypass_applied_max": 0,
     "scenario_validators": ["tool_heavy", "low_error"],
     "notes": "<provenance, scrubbing notes, anything reviewers should know>"
   }
   ```

5. Run the gate locally before commit:

   ```
   go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus/ --check
   go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus/ --promotion-check
   go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus/ --maxx-check
   ```

   The gate fails if any category's measured ratio falls below its `expected_savings_min`, exceeds its `expected_savings_max`, has fewer requests than `expected_request_count`, falls below `expected_saved_tokens_min`, exceeds `expected_max_errors`, exceeds `expected_latency_p95_max_ms`, misses an explicitly configured provider-cache/output-reduce threshold, exceeds explicitly configured planner replay thresholds (`expected_planner_missed_max`, `expected_planner_bypass_applied_max`), or fails any declared `scenario_validators`. Supported validators are `tool_heavy`, `cache_reuse`, `output_reduce`, `planner_alignment`, `websocket`, `low_error`, `layer_combo_diversity`, and `l2_summary`; unknown names fail the gate so typos cannot silently weaken evidence. The report also prints a factual layer-combination matrix (`L0+L1`, `L0+L1+L3`, `L4`, `WS`, `none`) so reviewers can see which combinations actually produced savings before adding stricter gates.

   `--promotion-check` is stricter and is only for release/default-on decisions.
   It ignores synthetic categories and requires at least five `codex_cli`
   sessions, five `codex_desktop` sessions, and live coverage for
   `repeat_read`, `ranged_read`, `search_loop`, `git_status`, `test_failure`,
   `apply_patch_edit_read`, `large_tool_output`, and `long_workday`. Each real
   category must declare `client_family`, `workload_class`, explicit zero error
   budget, explicit re-read canary budget, explicit latency budget, and either a
   ratio floor, an absolute saved-token floor, or a workload-specific economic
   signal such as provider-cache read tokens, tool-prune schema-token savings,
   or a positive output-reduce A/B pair. Missing metadata fails closed so no
   mechanism can be promoted from vague evidence.

   `--maxx-check` includes the promotion gate and then requires the
   mechanism-specific live workload classes that close the max-out program:
   `chunk_dedup_similar_outputs`, `chunk_dedup_log_output`,
   `chunk_dedup_test_output`, `output_reduce_aggressive`, `output_reduce_ab`,
   `tool_heavy`, `provider_cache_long_session`, and
   `host_resource_long_workday`. `output_reduce_aggressive` proves guarded
   injection and observed provider-output accounting; `output_reduce_ab` proves
   a counterfactual baseline/directive pair with positive net tokens after
   directive overhead. It is the gate for "all currently planned savings
   mechanisms are broadly proven", not just "the base release matrix is
   healthy".

6. Commit. The fixture is now part of the CI regression contract.

## Tightening the gate as the corpus grows (T118b)

The shipping seed (`synthetic_smoke/`) accepts a wide ratio band (0.30 to 0.85) by design - it is not real-session data. Real captures with known denominators should set tighter `expected_savings_min` / `expected_savings_max` brackets so a regression is caught quickly. Proof-matrix exports without known denominators should use `expected_saved_tokens_min` plus mechanism-specific validators instead. T118b in `docs/todo.md` tracks the operator-driven expansion to >=10 real-session categories.

## Removal

If a corpus entry is later found to contain content that should not have shipped, delete the file in a normal commit, mention it in the commit message, and consider rewriting history (`git filter-repo`) before the next push if the leak is sensitive enough to warrant it. Slimference is a local-first tool and the repository is small enough that a history rewrite is reasonable.
