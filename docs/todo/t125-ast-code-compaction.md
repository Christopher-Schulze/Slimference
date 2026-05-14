# TASK 125: AST-based code compaction for file-read tool results

Status: CODE-COMPLETE FOR GO SAFE-GATED PATH + ARCHIVE-BACKED BODY RECOVERY / LIVE NET-SAVING PROOF PENDING (2026-05-14)
Priority: P1
Scope: `internal/codecompact/` (new package), `internal/filter/builtin_read.go`, `internal/filter/pipeline.go`, integration with cache_archive (T96 / T108). Must include edit-mode gating before default-on.
Driver: when an LLM coding agent reads a code file via `Read` / `cat` / similar tool, today it sees the entire file verbatim - all 2000 lines of a Go service file, every imported helper, every legacy function. The agent often only needs package structure, imports, symbol table, signatures, and a small number of bodies. T125 ships a gated code-read compactor that replaces full-file reads with a structured "skeleton + relevant bodies" view only when that is likely to help. In edit/debug mode it must pass through full content because extra re-reads can cost more than the input tokens saved.

This is one of the largest input-side levers after prompt/cache work, but it is not free. On scan/orientation turns, file-read tokens can drop 60-80%. On edit turns, compaction can become negative if the agent immediately asks for omitted bodies. The task is successful only if mode detection prevents negative net savings.

## Reality correction (2026-05-01 audit)

- Do not default this on for every read.
- Do not assume "agent can re-read" is free; each re-read is another tool loop and can increase expensive output tokens.
- Start with Go stdlib parsing and one or two high-value languages before adding a large tree-sitter dependency set.
- Body-on-demand must be a real, testable retrieval path. A footer that hopes the agent asks correctly is not enough.

---

## Languages covered (priority order)

2026-05-02 implementation reality:

- Go shipped first via stdlib `go/parser` / `go/ast` / `go/printer` in `internal/codecompact`.
- The filter integration is deliberately narrow: large `.go` full-file reads through `cat` only. `head` / `tail` are partial reads and bypass AST compaction.
- TypeScript/Python/Rust/tree-sitter expansion is deferred until the Go gate shows positive live net savings. This follows the reality correction: no 10-grammar dependency set before measured value.
- Existing regex structure extraction remains as fallback for non-Go languages already supported by `internal/compression/structure.go`.

Each language has a different parser path. We start with the four most-used in LLM coding agents and extend.

1. **Go** - `go/parser` from stdlib. First implementation path.
2. **TypeScript / JavaScript / TSX / JSX** - parser choice must be justified by fixture gain; tree-sitter is allowed only after binary/build impact is accepted.
3. **Python** - parser-backed only after Go proves the mode gate.
4. **Rust** - parser-backed only after Go proves the mode gate.
5. **C / C++ / Java / Swift / Ruby / PHP / Dart / Zig / Svelte** - later expansion only when T118b/T124 corpus shows real file-read volume.

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

## Mode gate + lossless recovery protocol

The compaction is recoverable, not magically free. It may run only when the mode gate decides the current action is scan/orientation. It must bypass and return full content when:

- The file was edited in the current turn/session.
- The tool intent is obviously edit/debug (`apply_patch`, `write`, failing test body lookup, "fix this function", etc.).
- The file is small enough that skeleton overhead is not worth it.
- Recent quality signals show re-read spikes for this session.

If the agent decides it needs a specific body that was omitted:

1. The compacted output includes a footer:
   ```
   /* AST-compacted by Slimference. To see a specific function's body, ask:
      "show me the body of <FuncName>"
      or re-read with read_full=true */
   ```
2. The agent's next tool call can request the full body with
   `slimference expand-body <archive-id> <symbol>`.
3. Slimference extracts the function/method body from the archived original
   tool output, not from the compacted preview. Supported symbols are plain Go
   functions (`Run`) and methods (`Service.Run`, `(*Service).Run`).

This makes T125 recoverable under the LLM-agent interaction loop. The acceptance gate still measures net savings, including re-read cost and output-token recovery cost.

## Implementation plan

### WP1 - codecompact package

Completed as `internal/codecompact/api.go`:

- `Compact(path string, content []byte, opts Options) ([]byte, Stats, bool, error)`.
- Go detection by `.go`.
- Go parsing via stdlib `go/parser` + `go/ast` + `go/printer`.
- Options include mode, min bytes, max included body lines, recently edited gate, force-full gate, and relevant symbols.
- Stats expose language, original/compacted bytes, function count, omitted bodies, included bodies, and mode.
- Unsupported languages return `ErrUnsupported` without mutating output.

### WP2 - tree-sitter integration

Deferred by design. No tree-sitter dependency was added. Reason: the current repo already has broad non-Go regex structure extraction and T124 diagnostic coverage; adding linked grammars before Go live metrics would be dependency weight without proof.

### WP3 - Centrality heuristic

`internal/codecompact/centrality.go` computes per-function score:

- +5 if function name matches a token from the agent's last 5 tool calls.
- +5 if function name == `main` / `init` / `setup` / `teardown`.
- +3 if function is exported (capitalised in Go, `pub` in Rust, `export` in TS).
- +2 if function calls `panic` / `assert` / `throw` (likely error-relevant).
- +1 if function body <= 12 lines (cheap to include, shows pattern).
- Decay: include the top-K-scored bodies until the compacted output reaches a target ratio (e.g. 30% of original).

Implemented subset:

- Include body when function body is <= 8 lines by default.
- Include `main` and `init`.
- Include body when the function name is in `Options.RelevantSymbols`.
- Omit other bodies with `/* body omitted: N lines */`.

### WP3b - Mode gate

`internal/codecompact/mode.go` decides whether compaction is allowed:

- `scan`: compact allowed.
- `edit`: compact denied, full content returned.
- `debug`: compact denied unless the failing symbol is known and included.
- `unknown`: compact denied until corpus evidence proves positive net savings.

Signals: recent tool names, current command, file path, previous edits, quality re-read counters from T77, and explicit operator config.

Implemented subset:

- `Mode` must be empty, `scan`, or `orientation`.
- `edit` / `debug` / unknown modes deny compaction.
- `RecentlyEdited` denies compaction.
- `ForceFull` denies compaction.
- `MinBytes` floor denies small files.
- Filter integration only calls the AST compactor for `cat <file.go>`, never for `head` / `tail`.
- 2026-05-13: `FileReadContext` is threaded into the captured-output filter path. For Codex PostToolUse, recently-edited files are detected through the hook turn-state adapter and return literal contents.

### WP4 - Pipeline integration

Completed by extending `internal/filter/builtin_read.go`:

- Single-file `cat` on large Go files attempts `codecompact.Compact` before existing regex structure extraction.
- `head` / `tail` bypass AST compaction and continue through the old path.
- Unknown languages pass through or use existing comment-strip/structure fallback where available.
- `CompactCapturedOutputWithContext` and `TryStripCommentsFileReadWithContext` preserve legacy behaviour for callers with no session context and use session-derived safety gates when available.

### WP5 - Re-request handling

Shipped for Go archive-backed recovery:

- `PostToolUse` already archives large raw tool outputs before returning the
  compacted preview.
- `toolarchive.RenderContext` now adds `Body expand: slimference expand-body
  <archive-id> <symbol>` whenever the preview is AST-compacted.
- `slimference expand-body <archive-id> <go-symbol>` expands the archived
  original and prints exactly one Go function/method declaration with body.
- Retrieval is intentionally archive-backed. It does not read a possibly
  changed workspace file and does not pretend Codex can mutate a previous tool
  call through unsupported hook fields.

### WP6 - Telemetry

Existing per-filter observability records in/out bytes for `strip_comments_file_read`. Dedicated per-language AST counters and re-read/net-savings accounting remain pending until live corpus data proves this path fires often enough to justify another report surface.

### WP7 - Tests

- `internal/codecompact`: Go skeleton generation, large body omission, short body inclusion, relevant-symbol body inclusion, mode gates, unsupported/invalid input, main/init body inclusion, integer formatting helper.
- `internal/filter`: AST compaction integration and partial-read bypass.
- Round-trip body-on-demand is covered for Go archived reads via `expand-body`.

## Acceptance criteria

- [x] Go compaction works on large Go files: skeleton + selected bodies.
- [x] Mode gate denies edit/debug/force-full/recently-edited/small-file paths and returns full content.
- [x] Broader tree-sitter expansion is deferred until Go gate metrics are green.
- [x] Re-read protocol round-trips for Go archived reads: agent asks for a body by archive id + symbol and gets the original function/method body.
- [x] Centrality heuristic produces stable output for the same input + relevant symbols.
- [x] Per-session hook-state is bounded and file-backed for edit/read gates; body-on-demand uses the existing tool archive rather than a new global cache.
- [x] Coverage 100%; race-clean; CI gate green after the full Phase R batch.
- [ ] On Slimference repo's own session corpus (T118b), scan/orientation file-read tokens drop by 60-80% and edit/debug paths show no negative net savings.

## Out of scope

- Compacting code WRITES (the agent emitting code into the file). Different problem; T130 covers output-token compression for code generation.
- Cross-file symbol-graph compaction (build a project-wide skeleton when the agent reads multiple files of the same project). Future T125b.
- Markdown / HTML / config-file compaction. Different shape; covered by existing format-specific filters.

## Validation

```
go test ./internal/codecompact ./internal/filter
go test -race ./internal/codecompact ./internal/filter
slimference gain --by-language    # post-corpus measurement
```
