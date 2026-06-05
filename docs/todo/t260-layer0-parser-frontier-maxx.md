# T260 - Layer 0 parser frontier and safe pre-entry max-out

## Why

Layer 0 is the safest high-ROI compression surface when it is structured:
build/test/git/search/log/json/container outputs can be reduced before they enter
the conversation. The risk is not wire safety, it is comprehension safety. A
valid reduced output can still drop the one line the model needed. This task
makes Layer 0 a parser-first, error-priority, repo-safe savings surface with
measured hit rate and no silent information loss in product defaults.

## Current reality check

- Layer 0 exists and is productive.
- File reads are not a Layer 0 lossy scan surface anymore. Product defaults must
  full-pass first reads.
- The reducer dispatch order now has a metadata registry with mechanism id,
  command family, safety class, default eligibility, and preserved-evidence
  contract. This is telemetry/control-plane groundwork only; it does not change
  model-facing output.
- Many reducers are good and deterministic, but "deterministic" is not enough.
  Reducers must preserve the actionable payload: failures, paths, line numbers,
  exit status, changed files, destructive actions, match context, and summary
  counts.
- Known default cap families now use priority/head-tail evidence retention
  instead of blind first-N positional truncation. This covers search, log/lint,
  Tier-1 test JSON, SARIF, ESLint JSON, cargo metadata, and Terraform JSON.
  Healthy non-empty inventory/list surfaces such as Docker/Kubernetes tables,
  GitHub/GitLab lists, and healthy kubectl JSON full-pass unless there is
  diagnostic attention evidence.
- Search keying hygiene is now hardened and both CLI/Desktop search proofs are
  positive; remaining "maxxed out" work is broader real-traffic coverage for
  rarer parser families and any parser shape not already represented by tests or
  live corpus.

## Product target

Layer 0 default-auto should reduce common tool outputs aggressively while never
making the model less capable. Unknown, ambiguous, malformed, or high-risk
outputs must full-pass. Every reducer must prove three things: shorter output,
actionable information retained, and failure-open behavior under shape drift.

## Technical work packages

1. Build a reducer registry that records, per reducer:
   - [x] mechanism id
   - [x] command family
   - [x] preserved evidence contract
   - [x] safety class: exact, structured evidence, diagnostic priority, count
     summary
   - [x] default eligibility
   - [x] required structured fields per parser
   - [x] known recovery path per parser where recoverable
2. Convert all remaining cap-first reducers to priority-first reducers:
   - preserve error/failure/warning/destructive lines before noise
   - preserve file, line, column, exit code, command, tool name
   - preserve at least one nearby context line where the tool semantics require
     it
   - include omitted-count markers with enough shape to know what was omitted
3. Add corpus fixtures for each major reducer family:
   - git status/log/diff/show
   - rg/grep/git grep
   - go test, cargo test, pytest, jest/vitest
   - eslint/tsc/ruff/mypy/pyright
   - terraform plan/show/apply
   - docker/podman/kubectl/helm
   - JSON/SARIF/eslint-json/go-test-json
4. Add per-reducer "must retain" tests with late-position failures:
   - target line after old cap
   - colon-less Codex envelope noise
   - mixed stdout/stderr
   - truncated terminal output
   - unicode and path-with-space cases
5. Add production telemetry:
   - attempts
   - hits
   - full-pass reasons
   - bytes/tokens saved
   - retained attention rows
   - parser failure count
   - route: HTTP, WSS, hook, explicit wrapper
6. Expose only product-level Layer 0 counters to TUI:
   - saved input tokens
   - hit rate by command family
   - fail-open count
   - no parser internals

## Zero product-drawdown gates

- A reducer cannot be default-auto unless malformed input returns original
  output byte-equivalent.
- A reducer cannot be default-auto if it can drop failure lines, changed files,
  match context, destructive actions, or exit-status evidence.
- File-read reducers cannot do first-read body elision in product default.
- For every parser family, at least one test must prove an important line past
  the historical positional cap is retained.
- The model-facing marker language must be neutral, product-name-free, and
  machine-readable where recovery exists.

## Savings targets

These are promotion targets, not claims:

- Search/log/test/build command families: positive savings in at least 80% of
  large-output corpus fixtures.
- Real CLI/Desktop WSS corpus: Layer 0 should contribute positive billable-input
  savings in every session with at least one large tool output.
- No reducer may ship if average host-side processing exceeds 5 ms p95 for
  100 KB outputs on Apple Silicon unless the output is large enough that the
  savings clearly dominate.

## Verification

- `go test ./internal/filter ./internal/proxy ./scripts/utils`
- `go run ./scripts/coverage -min=95.0`
- `go run ./scripts/utils wss-proof-matrix <matrix>`
- Real captured CLI/Desktop replay with:
  - lost=0
  - parse_failures=0
  - compression_errors=0
  - degraded_sessions=0
- no repair/re-read spike

## Progress

- 2026-05-31: Added `Layer0ReducerRegistry()` and dispatch metadata for every
  built-in Layer 0 reducer. The registry is copied on read, keeps the existing
  reducer order, and is covered by uniqueness/order/evidence-contract tests.
  This closes the first audit/control-plane slice without changing compression
  behavior.
- 2026-05-31: Extended the reducer registry with required-field and recovery-path
  contracts. This makes default reducer safety auditable beyond "has a name":
  every parser now declares the evidence it must retain and the fail-open
  recovery behavior expected when that evidence cannot be proven.
- 2026-05-31: Hardened format-output compaction so large formatter file lists
  keep sampled changed filenames plus omitted-count markers instead of collapsing
  to a count-only summary. This aligns the reducer with its preserved-evidence
  contract and removes a small silent context-loss surface without changing the
  fail-shorter gate.
- 2026-06-01: Converted the remaining known cap-first default reducers to
  priority/head-tail evidence retention. Tier-1 Vitest/Jest, pytest, and Cargo
  test JSON now keep late failures; SARIF and ESLint JSON keep late same-priority
  errors; kubectl JSON keeps late attention rows; cargo metadata and Terraform
  JSON keep late workspace/resource evidence while Terraform resource changes
  still avoid letting benign no-op tails crowd out destructive changes.
- 2026-06-02: Added automatic scoped CLI route proof for Layer-0 search and git
  families. The search breadth capture
  `/Users/christopher/.slimference/captures/auto-proof-search-clean-20260602T004340Z.jsonl`
  covered `rg`, changed `rg` result sets, `git grep`, and `grep -R`, replayed
  with `lost=0`, and saved 45273 live WSS input tokens with 19 captured-output
  blocks and 8 exact repeated-output blocks. The git-status capture
  `/Users/christopher/.slimference/captures/auto-proof-git-status-20260602T004545Z.jsonl`
  replayed with `lost=0` and saved 1518 live WSS input tokens through the
  Codex exec-envelope/git-status reducer. Both runs had zero tool/command
  resolution misses and zero parse/degraded/compression errors.
- 2026-06-02: Hardened the search reducer's plain-match-line gate. `rg`/`grep`
  modes that emit headings, context blocks, passthrough output, multiline
  matches, or custom field separators now full-pass instead of being grouped by
  the `file:line:match` parser. `git -C <repo> grep ...` remains eligible, but
  context flags after `git grep` are blocked. This prevents fake file names,
  dropped context lines, and unsafe search-delta identities while preserving
  exact repeated-output dedup for byte-identical repeats.
- 2026-06-03: Hardened git diff/show compaction to preserve structural diff
  metadata before stripping context lines. Mode changes, new/deleted files,
  rename/copy metadata, similarity/dissimilarity markers, and binary-file
  markers now survive compaction, including hunks with no added/removed lines.
  Focused tests prove rename-only, mode-only, and binary/new-file metadata are
  retained instead of being reduced to misleading `+0/-0` file entries.
- 2026-06-03: Removed the unsafe non-empty `ls` / `tree` count-only reducer
  behavior from the default Layer-0 path. Directory names and tree hierarchy are
  the evidence the model requested; without exact recovery, reducing them to
  counts is a product drawdown. Empty and total-only `ls` output still compacts
  to `[ls] empty`, empty `tree` still compacts to `[tree] empty`, and repeated
  non-empty listings remain available to exact repeated-output/read-cache
  savings later in the session.
- 2026-06-03: Hardened the embedded default TOML reducer catalog so bundled
  `max_lines`, `head_lines`, and `tail_lines` caps preserve late fatal/error/
  warning/diagnostic evidence and emit omitted-line markers. This closes the
  remaining default cap-first surface below the handwritten Go reducers without
  changing user/project TOML semantics, which remain operator-owned literal
  configuration.
- 2026-06-04: Tightened the Layer-0 registry safety taxonomy. The old
  `count_summary` class is gone from the default registry because count-only
  listing reduction is not product-safe for model context. `ls` and `tree` now
  declare `empty_evidence`: they may only emit an empty marker and must declare
  that non-empty listings/hierarchies full-pass. Registry tests fail closed if a
  future reducer tries to reuse this class without the same full-pass contract.
- 2026-06-04: Removed the remaining Terraform list/value cap risk from the
  product default registry. `terraform state list` and human-readable
  `terraform output` now full-pass even when long, because resource addresses,
  output names, and output values are requested facts and the generic filter
  package cannot guarantee archive recovery. Plan/init/validate/show and
  structured JSON compaction stay active where they preserve diagnostic or
  structural evidence.
- 2026-06-04: Hardened the default cap evidence vocabulary for long logs and
  bundled TOML reducers. Late operational failures such as permission/auth
  denial, timeouts, refused connections, unhealthy/CrashLoop/OOM/segfault
  signals, and Terraform/OpenTofu-style destructive or drift state changes now
  survive evidence-first caps even when they sit behind large neutral output.
  Focused tests prove both log truncation and builtin TOML truncation preserve
  these late lines while staying within the configured line budget.
- 2026-06-05: Removed the remaining non-empty container table count-only
  shortcut from product defaults. `docker ps`, `docker images`, and
  `kubectl get` tables now full-pass when all rows are healthy because names,
  images, namespaces, and statuses are requested evidence. Large tables still
  compact only when diagnostic attention rows are present, and those rows
  remain verbatim with an omitted-count summary. Focused tests cover healthy
  full-pass and CrashLoopBackOff retention.
- 2026-06-05: Extended the same no-context-loss rule to non-empty GitHub/GitLab
  lists and kubectl JSON. Healthy `gh ... list` / `glab ... list` outputs now
  full-pass instead of becoming first-N previews, while large lists with
  diagnostic attention rows still compact to retained attention evidence.
  Healthy `kubectl -o json` lists also full-pass; only unhealthy attention
  items are summarized. Focused tests fail if these healthy non-empty surfaces
  become lossy again.
- 2026-06-05: Closed a Codex WSS Layer-0 miss for `cd <repo> && cargo check -vv`
  tool outputs. The shell-wrapper command stays intact for repo-safe cache and
  dependency keys, while the parser receives the extracted workdir plus inner
  Cargo command for compaction only. Cargo diagnostics now preserve the full
  error block, including source line, caret span, and "expected due to this",
  while dropping neutral verbose `Running CARGO=...` noise. The local wrapper
  path also no longer emits a fake stdout `[cargo check] ok` when stderr holds a
  non-zero failure. Focused tests cover non-zero stderr, Cargo check labels, and
  cd-wrapped Codex envelopes. Live proof:
  `/tmp/slimference-t260-cargo-vv-cd-testfailure-20260605T101244Z/matrix.jsonl`
  passed `wss-proof-matrix --require-live-token-delta` with one CLI row,
  `codex_exec_envelope=1`, `host_budget_ok`, `lost=0`, zero
  parse/degrade/compression errors, 934 billable input tokens saved, and 3183
  replay bytes saved. The content-free row was exported additively into
  `tests/fixtures/live_corpus/cli_test_failure/session_wss_proof_export_002.jsonl`;
  `benchmark-corpus --promotion-check` and `--maxx-check` pass with
  `cli_test_failure` now gating two real rows and 12081 saved tokens.

## Done

Layer 0 is done only when every default reducer has a preserved-evidence
contract, tests for late critical lines, real route attribution, and live
CLI/Desktop proof. Anything that is only "probably okay" stays non-default.
