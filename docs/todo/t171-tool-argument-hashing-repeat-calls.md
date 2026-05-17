# TASK 171: Tool-argument hashing for repeat-call short-circuit

Status: TODO (planning 2026-05-16)
Priority: P1
Scope: `internal/filter/`, `internal/hooks/codex.go`, `internal/crosstool/`, `cmd/slimference/main.go`

## Why

In a Codex session it is common to see `git status` called 3-5 times within a few seconds, or `ls /foo` followed by `ls /foo -la`, etc. The tool's output is essentially deterministic over the call interval. Returning the same compacted output from a small in-process cache instead of re-running the command saves:
- the tool's actual exec time (10-200 ms)
- the duplicate tokens in the next request (≈100-500 input tokens per repeated call)

**Why:** Tool reruns of deterministic commands are a constant token-leak and latency-leak. A 15-second TTL cache keyed on (command, cwd, argv-hash) closes it for negligible state.
**How to apply:** At the Codex `PreToolUse` hook (or via L0 filter), check the cache. If hit, emit a synthetic result with the same exit code and a small `[cached: <age>s]` annotation. The actual tool is not invoked.

## Target State

1. New `internal/crosstool/cache.go` with a short-TTL keyed cache `(cmd, cwd, argv-hash) -> compacted-output`.
2. TTL configurable: `[filter] tool_repeat_ttl_seconds = 15` (default).
3. Apply only to **deterministic-prone** commands: `git status`, `git diff` (unchanged tree only), `ls`, `pwd`, `which`, `env`, `git log -n1 HEAD`. Whitelist; no opt-out blacklist.
4. Hook integration: PreToolUse checks cache; if hit, **block** the tool call and emit cached output via PostToolUse-style additionalContext.
5. Telemetry: `repeat_call_hits` counter.

## Acceptance

- Running `git status` twice within 15s in the same cwd: second call serves from cache without exec.
- Different cwds → different cache keys → no false serving.
- After 15s: cache expires, next call re-runs.
- Code-changing commands (`git checkout`, etc.) bust the cache automatically for that cwd.
- 100% coverage.

## Sub-Tasks

- [ ] Whitelist of deterministic commands + their argv-canonicalisation rules.
- [ ] Cache: in-process LRU keyed by (cmd, cwd, argv-hash, TTL).
- [ ] Cache-bust triggers: any `git commit`, `git checkout`, `git merge` in the same cwd invalidates the cache.
- [ ] Hook contract: how to short-circuit the tool via PreToolUse decision (research Codex 0.130 contract).
- [ ] Tests: hit, miss, expiry, bust, whitelist-only.

## Notes

- Risk: caching `ls` may miss filesystem changes within TTL. Mitigation: 15s default is short enough that user-visible drift is rare; configurable down to 0.
- Hook-driven short-circuit is the cleanest; if Codex contract doesn't allow it, fall back to "rewrite the command to echo the cached output" via slimference rewrite.

## Deviations

(none yet)
