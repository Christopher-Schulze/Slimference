# TASK 117: Generic log filtering with source auto-detection

Status: PENDING (audit-driven mitigation 2026-04-30)
Priority: P2
Scope: `internal/filter/builtin_log.go`, `internal/filter/dispatch.go`, `internal/filter/log_shape.go` (new), `tests/fixtures/log_corpus/`
Driver: `TryCompactLogDedup` only fires when argv matches `docker logs` or `kubectl logs`. Real coding sessions stream log output through `tail -f`, `journalctl`, `cat <file>.log`, `grep ERROR <file>.log`, custom log readers, and pipes (`tail -F app.log | grep WARN`). All of these bypass the existing log compaction and pass through the generic build/test/passthrough fallback - missing the line-dedup, timestamp normalisation, level-based filtering, and tail-cap that `filterLogCompact` already implements.

---

## Problem

`internal/filter/builtin_log.go:13`:
```go
func TryCompactLogDedup(argv []string, stdout []byte) ([]byte, bool) {
    if !isDockerLogsArgv(argv) && !isKubectlLogsArgv(argv) {
        return stdout, false
    }
    ...
}
```

The `filterLogCompact` body in `internal/compression/tool_compressor.go` is good (consecutive-dedup with `[xN]` markers, timestamp normalisation, DEBUG/TRACE drop on >4000 bytes, 80-line cap). It just isn't reachable from common log workflows.

## Target State

Two-stage detection:

**Stage 1 - Argv hints** (existing): docker / kubectl logs, journalctl, tail / tail -F, less / more on `*.log`, `cat *.log`, `grep ... *.log`, `awk ... *.log`, `sed ... *.log`, `head ... *.log`.

**Stage 2 - Shape detection**: when no argv hint matches, sample the first 50 lines and detect log shape:

- 60%+ lines with leading timestamp (`YYYY-MM-DD HH:MM:SS` / ISO8601 / Unix epoch / syslog) -> log shape.
- 30%+ lines with severity tokens (`INFO`, `WARN`, `ERROR`, `DEBUG`, `TRACE`, `FATAL`) -> log shape.
- 50%+ lines starting with `[<bracket>]` markers (custom logger format) -> log shape.

When shape detection matches, run `filterLogCompact` regardless of source command.

## Implementation Plan

### WP1 - Argv hint expansion
- New `internal/filter/log_argv.go`: `isLogReadingArgv(argv []string) bool` matches all the canonical log-source commands.
- Whitelisted base commands: `tail`, `head`, `less`, `more`, `cat`, `journalctl`, `kubectl logs`, `docker logs`, `podman logs`, `lnav`, `multitail`.
- Conditional matches: `grep|awk|sed|rg <pattern> <file>` where file ends in `.log`/`.txt`/`.out`/`.err`.

### WP2 - Shape detector
- New `internal/filter/log_shape.go::DetectLogShape(stdout []byte) (LogShape, float64)` returning shape + confidence.
- Shapes: `LogShapeISO8601Timestamp`, `LogShapeUnixTimestamp`, `LogShapeSyslog`, `LogShapeBracketedLevel`, `LogShapeJSONLines` (per-line valid JSON with timestamp/level fields).
- Confidence threshold: 0.7. Below -> not a log.

### WP3 - Per-shape compactor
- ISO/Unix timestamps: existing `filterLogCompact` works.
- Syslog: extract `<host>` + `<programname>`, dedup-by-message body.
- Bracketed-level: keep severity, dedup body.
- JSON Lines: parse, dedup by `(level, message)` field, drop verbose `metadata` blob, keep `error` and `stack_trace` fields.

### WP4 - Dispatch wire-in
- `TryCompactLogDedup` becomes `TryCompactLogOutput` and is moved earlier in the dispatch chain (before generic build/test fallbacks since logs frequently contain `error|fail` substrings that would FP into build extraction).
- Internal logic: argv hint OR shape detection -> compact.

### WP5 - JSON-Lines log parser
- `parser_jsonlogs.go` handles structured logs (Winston, Bunyan, structured Java/Go loggers).
- Dedup key = `sha256(level + msg)`, count repeats.
- Render: `[level] msg [xN]` per unique entry.

### WP6 - Corpus
- `tests/fixtures/log_corpus/<shape>/<scenario>.txt` for: nginx access log, application stdout (Go slog), Java logback, Python logging, journalctl output, syslog, JSON Lines, mixed content.
- Each with `expected.txt`.

### WP7 - Tests
- Per-shape detection unit tests.
- Per-shape compaction tests against corpus.
- Negative tests: random text, code listings, JSON document (not lines) -> shape NOT detected.
- Ensure FP on test output is impossible (test output has its own dedicated parsers from T115).

## Acceptance Criteria

- [ ] All listed argv shapes route into the log compactor.
- [ ] Shape detection finds logs in stdout from non-listed commands with ≥70% confidence threshold.
- [ ] Each of the 6 shape parsers achieves ≥40% reduction on its representative corpus fixture.
- [ ] Zero FP on test/build output corpora (T115).
- [ ] Coverage 100%; race tests green.

## Out of Scope

- Streaming log compaction (T94/T108 own that surface).
- Log-pattern learning across sessions (T93 cross-session pattern mining is the right home for this if needed).
- ANSI colour-code log output stays handled by the existing `StripANSICodes` pre-pass.

## Validation

```
go test -race ./internal/filter/...
go run ./scripts/ci
```
