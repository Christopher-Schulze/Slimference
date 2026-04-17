# Slimference - Context & Worklog

## Active Task - 2026-04-17 - Production readiness remediation and proof closure

Goal: close every blocker from `docs/audit-1.md`, complete the tracked
remediation plans, re-run the repository proof stack, and publish a fresh-eyes
follow-up audit without lowering any target documents.

Artifacts completed in this task:

- `docs/audit-2.md`
- `docs/changelog.md`
- `docs/documentation.md`
- `docs/map.md`
- `docs/todo.md`
- `docs/todo/t11-audit-remediation-program.md`
- `docs/todo/t12-hook-contract-hardening.md`
- `docs/todo/t13-zero-downside-and-cache-correctness.md`
- `docs/todo/t14-layer2-strictness-and-cancellation.md`
- `docs/todo/t15-daemon-service-productionization.md`
- `docs/todo/t16-proof-gates-and-release-readiness.md`

Execution rules for this pass:

- keep `docs/audit-1.md` as the frozen baseline
- raise code and tests to the documented target instead of lowering docs
- only mark the remediation program complete after live proof commands pass

## Status: v2.0.2 - Production readiness remediation complete

Core outcome:

- zero-downside is now mechanically enforced in the proxy hot path
- Layer 3 keys the full canonical forwarded request, not text-only slices
- Claude Code and Codex hook flows were rebuilt around the supported contracts
- Layer 2 now propagates caller cancellation and defaults to strict validation
- launchd uses a dedicated `0600` env file instead of embedding MiniMax secrets
- the coverage gate is real and the repository now proves 100% Go coverage
- version strings are now sourced centrally and the remaining documentation
  drift is being closed against the live codebase
- offline savings reporting now includes session, decision, filter, and
  combined reports with text, JSON, and CSV output
- file-dependent Layer 3 entries are now admitted only when invalidation is
  really armed; otherwise Slimference prefers a safe miss over a stale hit
- analytics shutdown now drains queued events before exit, and FileWatcher
  close is idempotent
- analytics collector now handles closed drain channels safely, keeps true
  per-provider latency averages, and reports the saved-token ratio correctly
- session log formatting is now deterministic across runs for cleaner exports
  and diffable operator artifacts
- hook status now reflects coherent installs more accurately instead of only
  loose file presence, reducing false "installed" states in the TUI
- TUI quit now uses a bounded shutdown context so a stuck shutdown path cannot
  hang the UI indefinitely

Repository proof as of this task:

- `go test ./...` green
- `go test -race ./...` green
- `go run ./scripts/ci` green
- `go test -count=1 -cover ./cmd/... ./internal/...` -> `100.0%`
- `bun test tests/ts` green

## Completed Workstreams

### T13 - Zero-downside and cache correctness

- moved the negative-savings revert so the forwarded body always matches the
  kept compression result
- added direct regression coverage for negative-savings forwarding
- replaced the old text-only cache key with provider + canonical full-request
  hashing
- replaced response-substring invalidation with dependency-path extraction from
  the request body and path-aware invalidation
- tightened Layer 3 admission so file-dependent responses are cached only when
  dependency watches are actually armed; unavailable or saturated watchers now
  force a safe cache skip instead of stale-hit risk
- added end-to-end regression coverage for watcher-unavailable, watch-failed,
  watch-not-armed, and live file-change invalidation paths

### T12 - Hook contract hardening

- Claude PreToolUse now emits structured `hookSpecificOutput` with
  `updatedInput` / `permissionDecision`
- Claude settings merge/remove became non-destructive for unrelated user hooks
- Codex now installs `hooks.json` PreToolUse and PostToolUse hooks plus a
  dedicated `slimference posttool` path for captured output compaction
- Codex verify now fails on missing scripts, missing config, or inconsistent
  installs instead of treating breakage as best-effort

### T14 - Layer 2 strictness and cancellation

- Layer 2 work now has explicit context-aware entry points and no remaining
  production summarization path ignores caller cancellation
- summary validation inspects structured message content, not just markdown
  fences
- strict mode is now a first-class summary config surface with regression tests

### T15 - Daemon service productionization

- launchd plist generation no longer embeds `MINIMAX_API_KEY`
- install/remove now exercise real `launchctl` lifecycle steps
- generated env files are written with `0600` permissions and are removed on
  uninstall

### T16 - Proof gates and release readiness

- `scripts/ci` now passes the intended coverage threshold directly
- package-level coverage gaps were closed to 100% across `cmd/` and `internal/`
- extra tests cover hook payloads, daemon lifecycle, response cache helpers,
  TUI seams, summarization edge paths, and proxy startup races

### Post-remediation hardening pass

- introduced `internal/buildinfo.Version` as the single source for CLI, proxy,
  and TUI version strings
- moved Layer 3 cache keys to the effective forwarded body plus normalized
  cache-relevant headers and disabled caching for explicitly stochastic requests
- expanded analytics/debug/reporting scanner buffers to 8 MiB for large JSONL
  records
- completed `scripts/utils` reporting with `combined-report` plus JSON/CSV
  formats and added unit coverage for the report helpers
- began a documentation drift cleanup across `documentation.md`, `map.md`,
  `changelog.md`, `scripts/README.md`, and legacy todo artifacts
- hardened analytics shutdown draining and made `FileWatcher.Close()` safe to
  call twice during cleanup-heavy paths
- fixed `analytics.drainInput()` so a closed channel cannot loop forever or
  record zero-value phantom events during shutdown
- corrected per-provider latency running averages so mixed Anthropic/OpenAI
  sessions no longer skew each other's latency display
- corrected `AnalyticsSnapshot.CompressionRatio` to the saved-token fraction
  and made `SessionLogger.Format()` key ordering deterministic
- fixed OpenAI request reconstruction so structured `content` arrays survive
  roundtrip as arrays instead of being rewritten into stringified JSON
- raised the SSE relay per-line cap to 8 MiB to better tolerate large
  tool-output frames without local scanner overflow
- tightened TUI debug-log exports to user-only permissions (`0700` export dir,
  `0600` files)
- tightened hook status detection: Claude now checks settings wiring when
  present, Codex prefers a coherent hooks.json+scripts+config install and only
  falls back to the legacy AGENTS marker when modern hooks are absent
- changed TUI `q` / `Ctrl+C` shutdown to a timed context to reduce UI hang risk
  under pathological shutdown behavior
- fixed Codex uninstall cleanup so `hook remove codex` only removes
  Slimference-managed config lines and preserves unrelated user-owned
  `codex_hooks` or other `[features]` entries
- hardened Codex install/verify so conflicting `openai_base_url` or
  `codex_hooks = false` now fail fast instead of silently leaving a broken
  install that still looks healthy
- hardened proxy body limits: client request bodies above 32 MiB now fail with
  HTTP 413 instead of being silently truncated in `ServeHTTP`
- hardened passthrough response handling: non-streaming upstream bodies above
  10 MiB now fail with a local 502 instead of partial replay, and local
  passthrough errors no longer leak copied upstream success headers
- fixed streaming relay newline writes so a failed second write stops the relay
  instead of continuing after a broken SSE frame boundary
- tightened decision-log readers (`ReplaySession`, `readLastDecisionSummaries`)
  to skip JSON objects without `req_id` and to avoid surfacing partial results
  after scanner failures on oversized/corrupt files
- `go vet ./...` re-run clean as part of the deeper post-remediation forensic pass
- hardened `slimference test intercept` so bind failures now fail fast instead
  of burning the full timeout window on a dead listener
- hardened `debug.Recorder.flushJSONL` so marshal/write failures log warnings
  and do not emit corrupt placeholder lines into decisions JSONL

## Verification Snapshot

Commands used for final proof:

- `go test ./...`
- `go test -race ./...`
- `go test -count=1 -cover ./cmd/... ./internal/...`
- `go run ./scripts/ci`
- `bun test tests/ts`

Fresh-eyes review artifact:

- `docs/audit-2.md`

## Session Log

2026-04-09 - Initial implementation complete. All packages written from spec.md v1.0.0-final.
2026-04-13 - Rotating debug logger (slogutil), strategic debug logging (hot path + Layer 0),
             full reliability audit, 7 bugs fixed, race detector clean, docs flush.
2026-04-13 - Spec parity: §17.8 enhanced /health endpoint (full status JSON: layers, providers,
             queue depth, cache entries, version, minimax_configured). §13.3 CLI flag overrides
             (--port, --sliding-window, --no-layer1/2/3, --log-level). ResponseCache.Len() added.
2026-04-17 - Audit baseline and remediation plans created (`docs/audit-1.md`, `docs/gap-analysis.md`,
             `docs/todo/t11`-`t16`).
2026-04-17 - Production-readiness remediation completed: zero-downside fix, canonical request cache
             keys, hook contract hardening, Layer 2 strictness + cancellation, launchd secret model,
             100% Go coverage, CI gate repair, fresh-eyes audit (`docs/audit-2.md`).
2026-04-17 - Post-remediation hardening: centralized build version, stricter cache key partitioning,
             8 MiB JSONL scan buffers, complete offline savings reports, and doc/task drift cleanup.
2026-04-17 - Deep forensic stability pass: fixed silent request/response truncation, prevented
             passthrough header leakage on local 502s, hardened SSE newline-write failure handling,
             and made decision-log readers reject pseudo-summaries and partial scan results.
2026-04-17 - Structured operability pass: `test intercept` now fails fast on bind errors and
             debug decision logs no longer corrupt JSONL on marshal/write failures.
2026-04-17 - Structured deep-screen pass: OpenAI structured content now roundtrips without
             stringification, SSE relay tolerates 8 MiB frames, and exported debug logs use
             user-only permissions.
2026-04-17 - Structured safety pass: recursive hook JSON extraction now traverses
             sibling keys deterministically, and Codex install preflights malformed
             hooks.json / config conflicts before any repo-managed files are written.
