# TASK 97: Hybrid regex + tree-sitter structure extraction

Status: deferred - see docs/todo.md for closure rationale
Priority: P2
Scope: `internal/compression/structure.go`, `internal/compression/structure_more.go`, dependency on tree-sitter Go bindings
Driver: Structure extraction is regex-only. Templates, embedded DSLs, and unusual formatters (jsx-in-markdown, scripts inside HTML, embedded SQL) cause regex misses or false positives. Tree-sitter would handle all of these but at a much higher cost. A hybrid path keeps the speed of regex while raising the accuracy ceiling on confidence-fail.

---

## Problem

The 10-language regex set in `structure.go` is pragmatic but brittle. Unusual files produce wrong structure summaries, which feed into Layer 1's preview and later into MiniMax. The current `structure_min_tokens` threshold is the only safety valve.

## Target State

Hybrid extraction:

1. Regex pass first (current behaviour, fast).
2. Confidence score on the regex result (heuristic: ratio of recognised tokens, presence of expected shapes).
3. On low confidence, fall through to a tree-sitter pass for the language. Tree-sitter parser bundle is loaded lazily and cached.
4. Use the better-confident result.

If neither pass reaches the threshold, the file is left as raw content.

## Implementation Plan

### WP1 - Confidence score
- Score function per language; rolled up into a single 0-1 confidence number.

### WP2 - Tree-sitter wrapper
- `internal/compression/treesitter_extract.go` wraps a small set of grammars (Go, TS/JS, Python, Rust to start).
- Parser load is lazy and cached.

### WP3 - Hybrid orchestrator
- Existing entry points unchanged; orchestrator picks regex vs tree-sitter based on confidence.

### WP4 - Build constraints
- Tree-sitter under `// +build with_treesitter` so default builds remain Cgo-free; release pipeline enables it.

### WP5 - Tests
- Fixtures: regex wins on plain Go file; tree-sitter wins on jsx-in-md; both fail on bizarre input.

## Acceptance Criteria

- [ ] Regex remains the default and unchanged path.
- [ ] When confidence is below threshold and tree-sitter is available, the better result is used.
- [ ] Coverage stays at 100% for the non-tree-sitter build (Cgo-free).
- [ ] Tree-sitter build is exercised in CI on at least one platform.
- [ ] Race tests green.

## Out of Scope

- Adding tree-sitter for all 10 languages on day one (start with the four most common).
- Replacing regex; the hybrid is additive.

## Validation

```
go test ./internal/compression/...
go test -tags=with_treesitter ./internal/compression/...
```
