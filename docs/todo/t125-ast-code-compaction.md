# TASK 125: AST-based code compaction for file-read tool results

Status: PENDING (planned 2026-05-01)
Priority: P1
Scope: `internal/codecompact/` (new package), `internal/filter/builtin_read.go`, `internal/filter/pipeline.go`, integration with cache_archive (T96 / T108).
Driver: when an LLM coding agent reads a code file via `Read` / `cat` / similar tool, today it sees the entire file verbatim - all 2000 lines of a Go service file, every imported helper, every legacy function. The agent rarely needs all of it; what it needs is the package structure, the imports, the symbol table (function / type / constant names + signatures), and the body of whatever function is relevant to the current task. T125 ships an AST-based compactor that replaces full-file reads with a structured "skeleton + relevant bodies" view, lossless because the agent can always re-read the file or `grep` for any body it discovers it needs.

This is the largest single Layer-0/1 saving lever after Layer 4 (output compression). On a typical 80k-token coding session, file-read tool results account for 30-50% of input tokens. Reducing that by 70-85% net saves 20-40% on total token spend with zero correctness loss.

---

## Languages covered (priority order)

Each language has a different parser path. We start with the four most-used in LLM coding agents and extend.

1. **Go** - `go/parser` from stdlib. Already in the binary.
2. **TypeScript / JavaScript** - `tree-sitter` via `github.com/smacker/go-tree-sitter` + `tree-sitter-typescript`.
3. **Python** - `tree-sitter-python` (parsing only; no execution).
4. **Rust** - `tree-sitter-rust`.
5. **Java** - `tree-sitter-java` (extends to Kotlin, Scala via separate grammars).
6. **C / C++** - `tree-sitter-c` / `tree-sitter-cpp`.
7. **Swift / Objective-C** - `tree-sitter-swift`.
8. **Ruby**, **PHP**, **Elixir**, **Haskell**, **Dart**, **Zig**, **Nim**, **Crystal** - tree-sitter grammars where stable.

Languages tree-sitter has no stable grammar for (e.g. very new langs) fall through to "no compaction; pass file unchanged".

## What "AST-based compaction" means here

For a Go file the compacted view is a deterministic projection:

```
package myservice

import (
    "context"
    "fmt"
    "github.com/x/y"
)

const (
    DefaultPort = 8990
    Timeout     = 30 * time.Second
)

type ServiceConfig struct {
    Listen string
    DB     *sql.DB
    // ... 8 more fields
}

func (s *ServiceConfig) Validate() error { /* body omitted: 18 lines */ }
func (s *ServiceConfig) Listen() net.Listener { /* body omitted: 12 lines */ }
func NewService(cfg *ServiceConfig) (*Service, error) { /* body omitted: 31 lines */ }

type Service struct {
    cfg     *ServiceConfig
    metrics *Metrics
    // ... 3 more fields
}

func (s *Service) Run(ctx context.Context) error {
    // body included: 4 lines, called HandleRequest
    if err := s.preFlight(ctx); err != nil {
        return err
    }
    return s.HandleRequest(ctx)
}

func (s *Service) HandleRequest(ctx context.Context) error { /* body omitted: 47 lines */ }
// ... 5 more methods omitted
```

Rules:

1. **Always-include**: package decl, imports, all type / interface / const / var declarations at file scope, all function / method **signatures**.
2. **Body decision per function**: include the body when the function is "central to the file" (heuristic below); otherwise emit `/* body omitted: N lines */`.
3. **Centrality heuristic**: a function body is included when (a) the function name appears in the agent's recent tool calls / queries, or (b) it is `main` / `init` / called from outside the file, or (c) it is short (<= 8 lines). Otherwise body omitted with line count.
4. **Imports are kept verbatim** (rarely large; pruning would lose info about what the file uses).
5. **Comments at file scope** (package doc, license header) are kept; in-function comments are dropped along with the body.
6. **Code in `_test.go` files**: same rules but the centrality heuristic favours functions matching the failing test name when the agent is debugging a test.

For TypeScript:

```ts
import { foo } from './foo';
import type { Bar } from './bar';

export interface ServiceConfig {
    listen: string;
    db: DB;
    // ... 5 more fields
}

export class Service {
    constructor(private cfg: ServiceConfig) {}
    run(ctx: Context): Promise<void> { /* body omitted: 47 lines */ }
    handleRequest(ctx: Context): Promise<void> { /* body omitted: 31 lines */ }
}

export function helperFn(x: string): number { /* body omitted: 8 lines */ }
```

Same rules; tree-sitter handles class / interface / function declarations.

For Python:

```python
"""Module docstring kept."""
from typing import Optional
from .db import DB

CONST_A = 42
CONST_B = "hello"

class ServiceConfig:
    """Class docstring kept."""
    def __init__(self, ...): ...  # body omitted: 18 lines
    def validate(self) -> bool: ...  # body omitted: 12 lines

class Service:
    def run(self, ctx: Context) -> None: ...  # body omitted: 47 lines
    # ... 4 more methods omitted

def helper_fn(x: str) -> int: ...  # body omitted: 8 lines
```

## Lossless guarantee + agent re-read protocol

The compaction is **lossless** in the sense that no information the agent needs to act correctly is dropped *and unrecoverable*. If the agent decides it needs a specific body it omitted:

1. The compacted output includes a footer:
   ```
   /* AST-compacted by Slimference. To see a specific function's body, ask:
      "show me the body of <FuncName>"
      or re-read with read_full=true */
   ```
2. The agent's next tool call can request the full body; Slimference handles the re-request by replaying the original file content for that function.
3. Slimference caches the original file content per session so a re-read is free (no second filesystem hit).

This makes T125 effectively lossless under the LLM-agent interaction loop: the cost of "I needed that body, fetch please" is one extra tool call, recouped many times over by every other body that did not need fetching.

## Implementation plan

### WP1 - codecompact package

- `internal/codecompact/api.go`: `Compact(path string, content []byte, opts Options) ([]byte, Stats, error)`. Options include centrality-bias (function names from recent agent context, defaulted from sliding-window analyser), max-skeleton-size, language-detect override.
- `internal/codecompact/lang_detect.go`: per-extension dispatch (`.go` -> Go, `.ts`/`.tsx` -> TS, `.py` -> Py, etc.). Shebang detection for extensionless scripts.
- `internal/codecompact/lang_go.go`: uses `go/parser` + `go/ast` + `go/printer`. No external dependency (already in stdlib).
- `internal/codecompact/lang_ts.go`, `lang_py.go`, `lang_rust.go`, `lang_java.go`, `lang_c.go`: each wraps a tree-sitter grammar with a small AST visitor that extracts decl types + signatures.

### WP2 - tree-sitter integration

- New dependency: `github.com/smacker/go-tree-sitter` + per-grammar packages.
- Grammars are statically linked (~1MB per language). Total binary growth ~10MB across 9 grammars. Acceptable.
- Build tag `notreesitter` excludes tree-sitter for operators who care about binary size (Go-only compaction still works).

### WP3 - Centrality heuristic

`internal/codecompact/centrality.go` computes per-function score:

- +5 if function name matches a token from the agent's last 5 tool calls.
- +5 if function name == `main` / `init` / `setup` / `teardown`.
- +3 if function is exported (capitalised in Go, `pub` in Rust, `export` in TS).
- +2 if function calls `panic` / `assert` / `throw` (likely error-relevant).
- +1 if function body <= 12 lines (cheap to include, shows pattern).
- Decay: include the top-K-scored bodies until the compacted output reaches a target ratio (e.g. 30% of original).

### WP4 - Pipeline integration

- New dispatch entry in `internal/filter/pipeline.go`: `code_compact` runs after `read_summary` (T96) but before generic passthrough.
- Detection: argv-based (`Read`, `cat`, `bat`) + path-extension match.
- Bypass: file size < 200 lines or < 4KB - too small for compaction to help; full pass-through.
- Bypass: agent tool call has `read_full=true` flag.

### WP5 - Re-request handling

- Slimference caches the original file content per session in an in-memory map keyed by (sessionID, absolutePath, mtime).
- New tool-result-pre-processor detects the "agent asked for a function body explicitly" pattern (heuristic: tool call with `query=` + function name from a previous compacted output).
- On match, Slimference rewrites the tool result to include the body of the requested function from the cached original.

### WP6 - Telemetry

- Per-language hit counter + bytes-saved counter.
- Re-read counter: how often the agent had to ask for a body explicitly (high re-read count means the centrality heuristic is too aggressive; tune downward).

### WP7 - Tests

- Per-language `lang_<lang>_test.go`: 15+ tests per language. Skeleton matches expected; bodies-included match centrality heuristic.
- Lossless round-trip test: random file -> compact -> ask for every omitted body -> reconstruct -> byte-equal to original (modulo whitespace inside bodies).
- Performance test: 10k-line Go file compacts in <50ms.

## Acceptance criteria

- [ ] Go compaction works on the live `internal/proxy/proxy.go` (1k+ lines): skeleton + 2-3 method bodies.
- [ ] TypeScript / Python / Rust / Java parsers ship with corpus tests.
- [ ] Re-read protocol round-trips: agent asks for a body, gets it, reconstructed file is byte-equal to original.
- [ ] Centrality heuristic produces stable output for the same input + recent context.
- [ ] Per-session cache LRU-bounded; no leak on long sessions.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] On Slimference repo's own session corpus (T118b), file-read tokens drop by 60-80% with measurable agent-functioning gain (no degraded task completion).

## Out of scope

- Compacting code WRITES (the agent emitting code into the file). Different problem; T130 covers output-token compression for code generation.
- Cross-file symbol-graph compaction (build a project-wide skeleton when the agent reads multiple files of the same project). Future T125b.
- Markdown / HTML / config-file compaction. Different shape; covered by existing format-specific filters.

## Validation

```
go test -race ./internal/codecompact/...
slimference gain --by-language    # post-corpus measurement
```
