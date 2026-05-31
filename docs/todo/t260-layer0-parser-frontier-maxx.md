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
- Many reducers are good and deterministic, but "deterministic" is not enough.
  Reducers must preserve the actionable payload: failures, paths, line numbers,
  exit status, changed files, destructive actions, match context, and summary
  counts.
- Search and log reducers still need broader real-traffic proof and keying
  hygiene before "maxxed out" is true.

## Product target

Layer 0 default-auto should reduce common tool outputs aggressively while never
making the model less capable. Unknown, ambiguous, malformed, or high-risk
outputs must full-pass. Every reducer must prove three things: shorter output,
actionable information retained, and failure-open behavior under shape drift.

## Technical work packages

1. Build a reducer registry that records, per reducer:
   - mechanism id
   - command family
   - required structured fields
   - preserved evidence contract
   - lossy class: exact, structured-lossless-for-task, summary-only
   - default eligibility
   - known recovery path
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

## Done

Layer 0 is done only when every default reducer has a preserved-evidence
contract, tests for late critical lines, real route attribution, and live
CLI/Desktop proof. Anything that is only "probably okay" stays non-default.
