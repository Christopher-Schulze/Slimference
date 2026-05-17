# TASK 172: Cross-tool dedup within conversation window

Status: TODO (planning 2026-05-16)
Priority: P2
Scope: `internal/crosstool/`, `internal/proxy/handler.go`, `internal/compression/`

## Why

When the agent calls `ls /foo` then `tree /foo`, both outputs enumerate the same directory tree with massive overlap. When `git log -10` is followed by `git log -20`, the first 10 entries are duplicated. The proxy already sees both tool_results in the input — we can dedupe **between** them.

**Why:** Cross-tool overlap is a common 5-15% input-token leak in explorer-heavy workflows. Eliminating it costs zero quality (the content is literally the same).
**How to apply:** Detect overlapping content between adjacent tool_result blocks. Mark the second occurrence with `[overlaps previous tool_result: see above]` for the overlapping portion.

## Target State

1. Extend `internal/crosstool/` to handle the dedup logic.
2. Algorithm: compute n-gram fingerprints of every tool_result, detect ≥80% overlap between adjacent results, splice marker into the duplicate region of the second.
3. Per-session state to track recent fingerprints across turns.
4. Telemetry: `crosstool_dedup_chars` saved.

## Acceptance

- `ls /foo` then `tree /foo` in the same conversation: tree output keeps directory structure but file-list entries that appeared in `ls` get marked.
- `git log -10` then `git log -20` in the same conversation: first 10 commits replaced with marker in the second result.
- Distinct cwds or non-overlapping commands → no false-positives.
- 100% coverage.

## Sub-Tasks

- [ ] Fingerprint scheme: minhash for line-level dedup.
- [ ] Adjacent-tool-result walker.
- [ ] Marker format that preserves enough context (line-range cited).
- [ ] Tests: ls→tree, git-log overlap, no-overlap negative cases.

## Notes

- Order matters: only earlier-occurring content is the canonical reference; later occurrence gets the marker.
- For very long results, partial overlap (50%) is also useful; threshold tunable.

## Deviations

(none yet)
