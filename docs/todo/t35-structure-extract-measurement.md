# T35 - Structure Extraction: Measure Regex Error Rate, Decide on Tree-sitter

Status: open
Priority: medium
Scope: internal/compression/structure.go, scripts/utils, docs

---

## Problem

`structure.go` uses hand-written regex patterns to extract function, type,
and import signatures across 10 languages. Tree-sitter was dropped to avoid a
CGO dependency. The trade-off is pragmatic, but the **actual error rate**
(missed or malformed extractions) has never been measured. The failure mode
is silent: a false negative shows up as missed savings, a false positive as
corrupted compressed output (mitigated by the zero-downside guard, but still
wasteful).

Without numbers, we cannot decide if the regex approach is fine, or if
Tree-sitter-via-WASM or a similar CGO-free alternative is worth the effort.

---

## Desired End State

A measured baseline and a documented decision:

- Corpus of 1000+ real code snippets across the supported languages.
- Measurement of precision and recall for each language's extraction.
- Report: `docs/structure-extract-accuracy.md` with per-language numbers.
- Decision: keep regex / upgrade to Tree-sitter-WASM / hybrid. Recorded in
  `spec+.md`.

---

## Work Packages

### WP1 - Corpus

- Source: real-world files from popular open-source repos, multiple sizes.
- Organized under `scripts/utils/structure-accuracy/corpus/<lang>/*.{go,ts,py,rs,...}`.
- Include adversarial cases: nested generics, string-literal-with-braces,
  conditionally compiled blocks, macro-heavy code.

### WP2 - Ground truth

- For each file, generate a ground-truth signature list using a real parser
  per language (Go: `go/ast`; Python: `ast`; TS: `@typescript-eslint/parser`
  via a small Bun helper; Rust: `syn` via a one-shot binary; etc.).
- Store as `<file>.truth.json`.

### WP3 - Measurement harness

- `scripts/utils/structure-accuracy/main.go`: run our regex extractor over
  the corpus, diff against truth, emit precision/recall per language.
- Summary Markdown report.

### WP4 - Decision

- If precision >= 0.95 and recall >= 0.90 on all languages: keep regex,
  document known misses.
- If any language falls short: evaluate Tree-sitter-WASM via
  `github.com/smacker/go-tree-sitter` or a WASM runtime; measure impact on
  build size and compression speed.
- Write the decision and rationale into `spec+.md` §5.4.

---

## Subtasks

- [ ] Assemble per-language corpus.
- [ ] Build ground-truth generator per language.
- [ ] Implement measurement harness.
- [ ] Publish `docs/structure-extract-accuracy.md`.
- [ ] Record decision in `spec+.md`.

## Acceptance Criteria

- Each supported language has measured precision and recall numbers.
- The decision (keep regex / switch / hybrid) is explicit and dated.
- If switching, a follow-up task is created.
