# Audit 2 - Post-Remediation Fresh-Eyes Review

Date: 2026-04-17
Scope: entire Slimference repository except `rtk-master/`
Comparison baseline: `docs/audit-1.md`

---

## Verdict

The blockers from `docs/audit-1.md` are closed. Slimference is now
production-ready on its supported surface: Claude Code, Codex, the local
proxy/TUI runtime, and the macOS daemon/install path.

No critical or high findings remain from the original audit set.

---

## Live Verification Snapshot

The following checks were re-run after the remediation work:

- `go test ./...`
- `go test -race ./...`
- `go test -count=1 -cover ./cmd/... ./internal/...`
- `go run ./scripts/ci`
- `bun test tests/ts`

Observed result:

- Go test suite: green
- Race detector: green
- Go coverage on `cmd/...` + `internal/...`: `100.0%`
- `scripts/ci`: green and now fails correctly when coverage drops below target
- TypeScript test suite: green

---

## Comparison Against Audit 1

### A1 - Zero-downside hot-path breakage

Status: closed

- The forwarded request body is now built from the final kept message slice.
- Negative-savings regressions are covered by dedicated proxy tests.
- Metrics and forwarded-body behavior now agree.

### A2 - Response cache key correctness

Status: closed

- Layer 3 now hashes the canonical full JSON request body plus provider.
- Request identity covers non-text blocks and generation parameters because the
  full body is canonicalized instead of reduced to text-only content.
- File-change invalidation now uses tracked dependency paths extracted from the
  request body rather than response-substring guesses.

### A3 - Claude/Codex hook contract correctness

Status: closed

- Claude Code hooks now emit structured `hookSpecificOutput` responses for
  `updatedInput`, `deny`, and `ask`.
- Claude settings merging/removal is non-destructive for unrelated user hooks.
- Codex now uses `hooks.json` PreToolUse/PostToolUse hooks and a dedicated
  `slimference posttool` path for finished tool output compaction.
- `hook verify` now treats broken Codex installs as hard failures.

### A4 - Coverage proof and CI gating

Status: closed

- `scripts/ci` now passes the intended coverage threshold directly.
- The repository reaches `100.0%` Go coverage across `cmd/...` and `internal/...`.
- New tests were added for previously weak packages including daemon, hooks,
  summarization, TUI seams, proxy startup, response-cache helpers, and
  analytics persistence.

### A5 - Layer 2 strictness and cancellation

Status: closed

- Production Layer 2 call paths now propagate caller cancellation.
- Strict summary behavior is exposed through `[compression.summary].strict`.
- Validator checks are grounded in structured message content, not only fenced
  markdown accidents.

### A6 - Daemon/service production safety

Status: closed

- launchd no longer persists `MINIMAX_API_KEY` in the plist.
- A dedicated env file is written with `0600` permissions and removed on uninstall.
- install/remove now exercise real `launchctl` lifecycle steps with tests for
  success and failure paths.

### A7 - Validator weakness

Status: closed

- preservation checks now inspect structured message content, tool details,
  paths, errors, and identifiers more directly
- strict-mode tests cover acceptance and rejection paths

### A8 - Docs overclaiming proof

Status: closed

- docs now point to the proof-bearing commands and the post-remediation audit
- the remediation/todo program is marked complete instead of left open

---

## What Is Strong Now

- Layer 0 and Layer 1 form a coherent deterministic core with hard tests.
- Hook adoption for Claude Code and Codex is now strict, scoped, and
  non-destructive.
- Layer 2 is materially safer: cancellation is propagated, validation is
  stricter, and strict mode is explicit in config.
- Layer 3 correctness is now based on canonical request identity rather than
  heuristics that could alias distinct requests.
- The proof stack is real: coverage, race detection, repo CI, and TS tests are
  all reproducible from repository-native commands.

---

## Residual Non-Blockers

- External CLI hook contracts can drift over time. This is an evergreen
  compatibility risk, not a current release blocker. Future CLI releases should
  continue to be checked against the hook fixtures.
- Real-world performance under very large live sessions should keep being
  benchmarked, but the current repository no longer has a correctness or proof
  deficit that blocks production use.

---

## Final Judgment

`docs/audit-1.md` identified real blockers. They were not papered over; they
were fixed in code, covered by tests, and backed by green repository proof.

Slimference is ready for production use on the currently documented scope.
