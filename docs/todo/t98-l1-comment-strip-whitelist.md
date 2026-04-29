# TASK 98: Comment-strip whitelist for semantic comments

Status: completed (config knob deferred)
Priority: P2
Scope: `internal/compression/comment_strip.go`
Driver: Comment-strip removes ALL comments. License headers, `// SAFETY:` invariants, `// TODO(critical):` blockers, structured doc-comments are valuable signal that the agent needs. Stripping them is correct on aggregate but produces avoidable failures on the long tail.

---

## Problem

Layer 1 comment-strip is destructive across all 10 supported languages. It does not distinguish between filler (`// next line`) and load-bearing notes (`// SAFETY: must hold the mutex`). License headers also vanish, which can cause downstream prompts to lose required attribution context.

## Target State

A configurable whitelist preserves semantic comment patterns:

- Default whitelist: `SAFETY:`, `INVARIANT:`, `TODO(critical):`, `FIXME(critical):`, `// Copyright`, `// SPDX-License-Identifier`.
- Configurable via `[compression.comment_strip] keep_patterns = [...]` (regex per pattern).
- Tested per language so language-specific comment markers (`#`, `//`, `/* */`) all interact with the whitelist.

T76 archive captures the stripped non-whitelisted comments for reverse path.

## Implementation Plan

### WP1 - Whitelist matcher
- Compiled regex set, cached.

### WP2 - Per-language integration
- Each language pass consults the whitelist before discarding a comment.

### WP3 - Defaults + config
- Document defaults; config knob for additions.

### WP4 - Tests
- Per-language fixture with a license header and a `SAFETY:` line; assert preservation.

## Acceptance Criteria

- [ ] Default whitelist preserves the listed patterns across all 10 languages.
- [ ] Config can extend the whitelist.
- [ ] T76 archive holds the stripped non-whitelisted comments.
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Auto-detecting "important" comments via NLP (research track).

## Validation

```
go test ./internal/compression/...
```
