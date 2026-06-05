# T289 RTK safe extra tool breadth

## Why

RTK is no longer broader than Slimference in the embedded TOML filter catalog after T288, but its command-discovery/wrapper registry still routes several safe tool families that Slimference can already compact only when the user reaches `slimference filter` manually. That leaves savings on the table without increasing model-facing risk.

This task closes that gap only where the product contract stays deterministic and drawdown-free: exact JSON compaction, empty-success evidence, structured diagnostic evidence, or fail-open passthrough. RTK-style lossy code-signature summaries, arbitrary output truncation, and unrecoverable command summaries stay rejected as defaults.

## Acceptance

- [x] `RewriteCommand` routes every existing safe built-in reducer family that can be reached without broad arbitrary-runtime interception.
- [x] `curl` and `wget` are safe by default: valid network JSON may be whitespace-compacted, but never schema-summarized or truncated.
- [x] RTK-only registry candidates are explicitly classified as adopted, already covered, or rejected with reason.
- [x] Tests prove new routing breadth, network JSON exactness, and risky arbitrary commands staying unrewritten.
- [x] `go test ./internal/filter`, `go test ./...`, and `go run ./scripts/ci` pass.
- [x] Documentation reflects the current RTK parity and the non-drawdown guard.

## Sub-Tasks

- [x] Verify RTK registry-only gaps against live Slimference built-ins.
- [x] Add safe rewrite breadth for existing deterministic reducers.
- [x] Add network JSON exact guard ahead of generic JSON schema extraction.
- [x] Add focused tests for routing, network JSON, and rejected risky commands.
- [x] Flush docs and run full gates.
- [x] Commit as `TASK 289: RTK safe extra tool breadth`.

## Notes

- Verified: embedded RTK TOML catalog parity was already achieved by T288.
- Still relevant RTK registry-only surfaces: `gt`, standalone `diff`, `curl`, `wget`, `prisma`, and some direct build/lint/format/package/search tool binaries whose Slimference reducer already exists.
- `curl/wget` are the only risky high-value addition because generic JSON schema extraction is lossy for arbitrary API responses. The fix is an argv-aware exact network JSON reducer before generic JSON minify.
- `pip/uv/pnpm list/outdated/show` output can be task-critical dependency evidence, so lossy list summaries are not default-safe.
- RTK `wget -O -` line-window summaries are rejected as default because they drop body lines without guaranteed model recovery.
- `go test ./internal/filter` passed.
- `go test ./...` passed.
- `go run ./scripts/ci` passed all 8 steps with aggregate coverage 96.5% and live-corpus gate PASS.

## Deviations

- None.
